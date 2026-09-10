package porkbun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := New(Config{APIKey: "pk1_test", SecretKey: "sk1_test", BaseURL: baseURL, MaxRetries: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func decodeJSON(t *testing.T, r *http.Request, out any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		t.Fatalf("decoding request body: %v", err)
	}
}

// TestErrorOnHTTP200 is the load-bearing error case: Porkbun signals failure
// in the body, and some endpoints do it while answering HTTP 200. A client
// that branches on the HTTP code records nameservers that were never applied.
func TestErrorOnHTTP200(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ERROR","message":"Domain is not opted in to API access.","code":"DOMAIN_NOT_ALLOWED"}`))
	}))
	defer srv.Close()

	err := testClient(t, srv.URL).UpdateNameservers(context.Background(), "example.com", []string{"a.example.com", "b.example.com"})
	if err == nil {
		t.Fatal("HTTP 200 with status ERROR must be an error")
	}
	if code := ErrorCode(err); code != "DOMAIN_NOT_ALLOWED" {
		t.Errorf("ErrorCode = %q, want DOMAIN_NOT_ALLOWED", code)
	}
}

func TestErrorOnHTTP400(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"ERROR","message":"Invalid domain.","code":"INVALID_DOMAIN",` +
			`"next_action":{"type":"fix_request","hint":"Check the domain name.","url":"https://porkbun.com/account/api"}}`))
	}))
	defer srv.Close()

	_, err := testClient(t, srv.URL).GetNameservers(context.Background(), "nope")
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *Error
	if !as(err, &apiErr) {
		t.Fatalf("error is %T, want *Error", err)
	}
	if apiErr.HTTPStatus != http.StatusBadRequest {
		t.Errorf("HTTPStatus = %d", apiErr.HTTPStatus)
	}
	if apiErr.Code != "INVALID_DOMAIN" {
		t.Errorf("Code = %q", apiErr.Code)
	}
	if apiErr.NextAction == nil || apiErr.NextAction.Type != "fix_request" {
		t.Errorf("NextAction = %+v", apiErr.NextAction)
	}
	if !IsNotFound(err) {
		t.Error("INVALID_DOMAIN should read as not-found so Read can drop the resource")
	}
	if apiErr.Retryable() {
		t.Error("fix_request is not retryable")
	}
}

func TestErrorMessageNamesCodeAndHint(t *testing.T) {
	t.Parallel()

	err := &Error{HTTPStatus: 400, Status: "ERROR", Message: "nope", Code: "IP_NOT_ALLOWED", Path: "domain/updateNs/x",
		NextAction: &NextAction{Type: "fix_request", Hint: "Allowlist the caller IP."}}
	got := err.Error()
	for _, want := range []string{"IP_NOT_ALLOWED", "domain/updateNs/x", "nope", "Allowlist the caller IP."} {
		if !contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
}

func TestNonJSONServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>gateway</html>"))
	}))
	defer srv.Close()

	_, err := testClient(t, srv.URL).GetNameservers(context.Background(), "example.com")
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *Error
	if !as(err, &apiErr) || apiErr.HTTPStatus != http.StatusBadGateway {
		t.Errorf("err = %v, want an *Error carrying http 502", err)
	}
}

func TestNewRejectsBadBaseURL(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"not-a-url", "/relative/only", "://"} {
		if _, err := New(Config{BaseURL: bad}); err == nil {
			t.Errorf("New(BaseURL=%q) should fail", bad)
		}
	}
	c, err := New(Config{})
	if err != nil {
		t.Fatalf("New with defaults: %v", err)
	}
	if c.BaseURL() != DefaultBaseURL {
		t.Errorf("default BaseURL = %q", c.BaseURL())
	}
	c, err = New(Config{BaseURL: DefaultBaseURL + "/"})
	if err != nil {
		t.Fatalf("New with trailing slash: %v", err)
	}
	if c.BaseURL() != DefaultBaseURL {
		t.Errorf("trailing slash should be trimmed, got %q", c.BaseURL())
	}
}

func TestListDomainsFiltersAndPages(t *testing.T) {
	t.Parallel()

	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"status":"SUCCESS","count":2,"domains":[` +
			`{"domain":"b.example","tld":"example","apiAccess":1,"notLocal":1,"autoRenew":0},` +
			`{"domain":"a.example","tld":"example","apiAccess":0,"notLocal":0,"autoRenew":1}]}`))
	}))
	defer srv.Close()

	yes := true
	got, err := testClient(t, srv.URL).ListDomains(context.Background(), ListDomainsOptions{
		APIAccess: &yes,
		TLDs:      []string{".COM", "io"},
	})
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d domains", len(got))
	}
	if !got[0].APIAccess.Bool() || !got[0].NotLocal.Bool() || got[0].AutoRenew.Bool() {
		t.Errorf("0/1 integer flags decoded wrong: %+v", got[0])
	}
	if len(queries) != 1 {
		t.Fatalf("a short page must not trigger another request, got %d requests", len(queries))
	}
	for _, want := range []string{"apiAccess=yes", "tlds%5B%5D=com", "tlds%5B%5D=io", "start=0"} {
		if !contains(queries[0], want) {
			t.Errorf("query %q missing %q", queries[0], want)
		}
	}
}

func TestRetrieveRecordEmptyListIsNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"SUCCESS","records":[]}`))
	}))
	defer srv.Close()

	rec, found, err := testClient(t, srv.URL).RetrieveRecord(context.Background(), "example.com", "1")
	if err != nil {
		t.Fatalf("RetrieveRecord: %v", err)
	}
	if found || rec != nil {
		t.Error("an empty records array means the record is gone, not an error")
	}
}

func TestRetrieveRecordUnwrapsSingletonList(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"SUCCESS","records":[{"id":"123","name":"www.example.com","type":"A",` +
			`"content":"1.2.3.4","ttl":"600","prio":null,"notes":null}]}`))
	}))
	defer srv.Close()

	rec, found, err := testClient(t, srv.URL).RetrieveRecord(context.Background(), "example.com", "123")
	if err != nil || !found {
		t.Fatalf("RetrieveRecord: rec=%v found=%v err=%v", rec, found, err)
	}
	if rec.TTL.Int64() != 600 {
		t.Errorf("ttl string did not decode to 600: %v", rec.TTL)
	}
	if rec.Prio.Int64() != 0 {
		t.Errorf("null prio must normalize to 0, got %v", rec.Prio)
	}
	if got := SubdomainOf(rec.Name, "example.com"); got != "www" {
		t.Errorf("SubdomainOf = %q, want www", got)
	}
}

func TestCreateRecordSurfacesExistingID(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"ERROR","message":"Duplicate record.","code":"DUPLICATE_RECORD","existingId":"987654"}`))
	}))
	defer srv.Close()

	_, existing, err := testClient(t, srv.URL).CreateRecord(context.Background(), "example.com", RecordInput{Type: "A", Content: "1.2.3.4"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if existing != "987654" {
		t.Errorf("existingID = %q, want 987654", existing)
	}
}

func TestCreateRecordOmitsZeroTTL(t *testing.T) {
	t.Parallel()

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeJSON(t, r, &body)
		_, _ = w.Write([]byte(`{"status":"SUCCESS","id":"42"}`))
	}))
	defer srv.Close()

	id, _, err := testClient(t, srv.URL).CreateRecord(context.Background(), "example.com", RecordInput{Type: "A", Content: "1.2.3.4"})
	if err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	if id != "42" {
		t.Errorf("id = %q", id)
	}
	if _, present := body["ttl"]; present {
		t.Error("a zero ttl should be omitted so Porkbun applies the account minimum")
	}
}

func TestSubdomainOf(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ fqdn, domain, want string }{
		"subdomain":     {"www.example.com", "example.com", "www"},
		"apex":          {"example.com", "example.com", ""},
		"deep":          {"a.b.example.com", "example.com", "a.b"},
		"wildcard":      {"*.example.com", "example.com", "*"},
		"trailing dots": {"www.example.com.", "example.com.", "www"},
		"uppercase":     {"WWW.Example.COM", "example.com", "www"},
		"empty":         {"", "example.com", ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := SubdomainOf(tc.fqdn, tc.domain); got != tc.want {
				t.Errorf("SubdomainOf(%q, %q) = %q, want %q", tc.fqdn, tc.domain, got, tc.want)
			}
		})
	}
}

func TestFlexIntDecoding(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   string
		want int64
	}{
		{`{"v":600}`, 600},
		{`{"v":"600"}`, 600},
		{`{"v":null}`, 0},
		{`{"v":""}`, 0},
		{`{"v":1}`, 1},
	} {
		var out struct {
			V flexInt `json:"v"`
		}
		if err := json.Unmarshal([]byte(tc.in), &out); err != nil {
			t.Errorf("Unmarshal(%s): %v", tc.in, err)
			continue
		}
		if out.V.Int64() != tc.want {
			t.Errorf("Unmarshal(%s) = %d, want %d", tc.in, out.V.Int64(), tc.want)
		}
	}

	var bad struct {
		V flexInt `json:"v"`
	}
	if err := json.Unmarshal([]byte(`{"v":"six hundred"}`), &bad); err == nil {
		t.Error("a non-numeric string should not silently decode to 0")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
