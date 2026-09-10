package porkbun

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMockAccountBalance is the tier-1 shape check against the live /mock
// endpoint. The mock serves schema-shaped placeholders — balance is 0 and
// display is the literal "string" — so nothing about the values is
// assertable; what it catches is the endpoint disappearing, answering
// something other than SUCCESS, or moving `balance` to a type flexInt
// refuses.
func TestMockAccountBalance(t *testing.T) {
	c := mockClient(t)

	bal, err := c.GetBalance(context.Background())
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if bal.Display == "" {
		t.Errorf("display did not decode: %+v", bal)
	}
}

func TestGetBalanceUsesGET(t *testing.T) {
	t.Parallel()

	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"status":"SUCCESS","balance":1234,"display":"$12.34"}`))
	}))
	defer srv.Close()

	bal, err := testClient(t, srv.URL).GetBalance(context.Background())
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if method != http.MethodGet {
		t.Errorf("reads should be GET, got %s", method)
	}
	if path != "/account/balance" {
		t.Errorf("path = %s", path)
	}
	if bal.Cents != 1234 {
		t.Errorf("Cents = %d, want 1234", bal.Cents)
	}
	if bal.Display != "$12.34" {
		t.Errorf("Display = %q", bal.Display)
	}
}

// The mock can never exercise this: the spec says `balance` is an integer,
// but Porkbun serves integers as decimal strings on several other
// endpoints, and a balance that stops decoding would surface as a bare
// "decoding response body" error on every plan.
func TestGetBalanceDecodesStringAmount(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"SUCCESS","balance":"1234","display":"$12.34"}`))
	}))
	defer srv.Close()

	bal, err := testClient(t, srv.URL).GetBalance(context.Background())
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if bal.Cents != 1234 {
		t.Errorf("Cents = %d, want 1234", bal.Cents)
	}
}

// A decimal must fail rather than decode. If Porkbun ever switched the field
// to dollars, silently truncating "12.34" to 12 would report $0.12 of credit
// as if the API had said so, and a spend guard built on it would pass.
func TestGetBalanceRefusesDecimalAmount(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"SUCCESS","balance":"12.34","display":"$12.34"}`))
	}))
	defer srv.Close()

	_, err := testClient(t, srv.URL).GetBalance(context.Background())
	if err == nil {
		t.Fatal("a decimal balance must not decode as cents")
	}
	if !strings.Contains(err.Error(), "12.34") {
		t.Errorf("error should name the undecodable value, got %v", err)
	}
}

// A null balance is zero credit, not a decode failure: an account that has
// never been topped up is a normal state, and flexInt maps null to 0.
func TestGetBalanceToleratesNull(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"SUCCESS","balance":null,"display":null}`))
	}))
	defer srv.Close()

	bal, err := testClient(t, srv.URL).GetBalance(context.Background())
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if bal.Cents != 0 || bal.Display != "" {
		t.Errorf("null fields should decode to zero values, got %+v", bal)
	}
}
