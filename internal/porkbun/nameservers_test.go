package porkbun

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestNormalizeNameserver(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ in, want string }{
		"already normal":     {"ns1.example.com", "ns1.example.com"},
		"trailing dot":       {"ns1.example.com.", "ns1.example.com"},
		"uppercase":          {"NS1.EXAMPLE.COM", "ns1.example.com"},
		"mixed case and dot": {"Ns-Cloud-A1.GoogleDomains.com.", "ns-cloud-a1.googledomains.com"},
		"surrounding space":  {"  ns1.example.com  ", "ns1.example.com"},
		"space and dot":      {" ns1.example.com. ", "ns1.example.com"},
		"empty":              {"", ""},
		"whitespace only":    {"   ", ""},
		"root dot only":      {".", ""},
		"internal dots kept": {"a.b.c.example.com", "a.b.c.example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeNameserver(tc.in); got != tc.want {
				t.Errorf("NormalizeNameserver(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeNameservers(t *testing.T) {
	t.Parallel()

	got := NormalizeNameservers([]string{
		"NS-CLOUD-A2.googledomains.com.",
		"ns-cloud-a1.googledomains.com",
		"",
		"ns-cloud-a1.googledomains.com.",
		"  ns-cloud-a4.googledomains.com  ",
		"ns-cloud-a3.googledomains.com",
	})
	want := []string{
		"ns-cloud-a1.googledomains.com",
		"ns-cloud-a2.googledomains.com",
		"ns-cloud-a3.googledomains.com",
		"ns-cloud-a4.googledomains.com",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NormalizeNameservers() = %v, want %v", got, want)
	}
}

// TestSameNameserverSet covers the exact failure this provider exists to
// avoid: Cloud DNS hands out trailing dots, the registry hands back an
// arbitrary order, and if those two are not the same value to Terraform then
// every plan across every domain shows drift no apply can settle.
func TestSameNameserverSet(t *testing.T) {
	t.Parallel()

	cloudDNS := []string{
		"ns-cloud-a1.googledomains.com.",
		"ns-cloud-a2.googledomains.com.",
		"ns-cloud-a3.googledomains.com.",
		"ns-cloud-a4.googledomains.com.",
	}
	registry := []string{
		"ns-cloud-a3.googledomains.com",
		"NS-CLOUD-A1.googledomains.com",
		"ns-cloud-a4.googledomains.com",
		"ns-cloud-a2.GOOGLEDOMAINS.com",
	}

	if !SameNameserverSet(cloudDNS, registry) {
		t.Error("shuffled, case-folded, dot-stripped registry answer should equal the configured set")
	}
	if SameNameserverSet(cloudDNS, registry[:3]) {
		t.Error("a genuinely missing nameserver must not compare equal")
	}
	if SameNameserverSet(cloudDNS, append(append([]string{}, registry...), "ns5.example.com")) {
		t.Error("a genuinely extra nameserver must not compare equal")
	}
	if !SameNameserverSet(nil, nil) {
		t.Error("two empty sets are equal")
	}
	if !SameNameserverSet([]string{"a.example.com", "a.example.com."}, []string{"A.EXAMPLE.COM"}) {
		t.Error("duplicates should collapse before comparison")
	}
}

func TestUpdateNameserversRefusesEmpty(t *testing.T) {
	t.Parallel()

	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"status":"SUCCESS"}`))
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	for _, in := range [][]string{nil, {}, {""}, {"  ", "."}} {
		err := c.UpdateNameservers(context.Background(), "example.com", in)
		if !errors.Is(err, ErrNoNameservers) {
			t.Errorf("UpdateNameservers(%v) error = %v, want ErrNoNameservers", in, err)
		}
	}
	if called {
		t.Error("client must not reach the API with an empty nameserver list")
	}
}

func TestUpdateNameserversSendsNormalizedSet(t *testing.T) {
	t.Parallel()

	var body map[string]any
	var method, path, idem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, idem = r.Method, r.URL.Path, r.Header.Get("Idempotency-Key")
		decodeJSON(t, r, &body)
		if got := r.Header.Get("X-API-Key"); got != "pk1_test" {
			t.Errorf("X-API-Key = %q", got)
		}
		if got := r.Header.Get("X-Secret-API-Key"); got != "sk1_test" {
			t.Errorf("X-Secret-API-Key = %q", got)
		}
		_, _ = w.Write([]byte(`{"status":"SUCCESS"}`))
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	if err := c.UpdateNameservers(context.Background(), "example.com", []string{"NS2.example.com.", "ns1.example.com"}); err != nil {
		t.Fatalf("UpdateNameservers: %v", err)
	}

	if method != http.MethodPost {
		t.Errorf("method = %s, want POST", method)
	}
	if path != "/domain/updateNs/example.com" {
		t.Errorf("path = %s", path)
	}
	if idem == "" {
		t.Error("POST must carry an Idempotency-Key so a retried apply cannot apply twice")
	}
	ns, _ := body["ns"].([]any)
	if len(ns) != 2 || ns[0] != "ns1.example.com" || ns[1] != "ns2.example.com" {
		t.Errorf("ns = %v, want normalized and sorted", body["ns"])
	}
	if _, leaked := body["apikey"]; leaked {
		t.Error("credentials must go in headers, not the request body")
	}
}

func TestGetNameserversUsesGETAndNormalizes(t *testing.T) {
	t.Parallel()

	var method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		_, _ = w.Write([]byte(`{"status":"SUCCESS","ns":["NS2.example.com.","ns1.EXAMPLE.com"]}`))
	}))
	defer srv.Close()

	got, err := testClient(t, srv.URL).GetNameservers(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("GetNameservers: %v", err)
	}
	if method != http.MethodGet {
		t.Errorf("reads should be GET, got %s", method)
	}
	want := []string{"ns1.example.com", "ns2.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetNameservers() = %v, want %v", got, want)
	}
}

// TestUpdateNameserversRefusesCollapsedDuplicates covers the gap between the
// schema's element count and the number of nameservers that reach the wire.
//
// The resource validator counts configured strings; the payload is built
// from NormalizeNameservers, which folds case, strips trailing dots and
// de-duplicates. Two spellings of one hostname satisfy the validator and
// would delegate the domain to a single nameserver.
func TestUpdateNameserversRefusesCollapsedDuplicates(t *testing.T) {
	t.Parallel()

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"status":"SUCCESS"}`))
	}))
	defer srv.Close()

	for _, in := range [][]string{
		{"ns1.example.com", "ns1.example.com."},
		{"NS1.EXAMPLE.COM.", " ns1.example.com "},
		{"ns1.example.com", ""},
	} {
		err := testClient(t, srv.URL).UpdateNameservers(context.Background(), "example.com", in)
		if !errors.Is(err, ErrTooFewNameservers) {
			t.Errorf("UpdateNameservers(%v) error = %v, want ErrTooFewNameservers", in, err)
		}
	}
	if called {
		t.Error("a collapsed nameserver list reached the API")
	}
}
