package porkbun

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// Tier 1: decode the shapes the real API actually serves.
//
// Porkbun's /mock prefix answers every documented endpoint with
// schema-shaped placeholder data and needs no credentials, so these run on
// every pull request including from forks. They are the only tests that
// catch the API changing its response shape under us.
//
// The mock is stateless: it will not echo back anything a write "stored", so
// resource lifecycle behaviour cannot be tested here. That lives in the
// in-process fake in internal/provider.
var (
	mockOnce     sync.Once
	mockShared   *Client
	mockProbeErr error
)

func mockClient(t *testing.T) *Client {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping network-dependent mock API test in -short mode")
	}

	// Probe once for the whole package. The mock rate-limits, so repeating
	// the reachability check per test is itself a way to fail it.
	mockOnce.Do(func() {
		c, err := New(Config{
			BaseURL:    MockBaseURL,
			MaxRetries: 3,
			HTTPClient: &http.Client{Timeout: 20 * time.Second},
		})
		if err != nil {
			mockProbeErr = err
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if _, err := c.Ping(ctx); err != nil {
			mockProbeErr = err
			return
		}
		mockShared = c
	})

	if mockProbeErr != nil {
		t.Skipf("porkbun mock API unreachable, skipping: %v", mockProbeErr)
	}
	return mockShared
}

// skipIfUnavailable turns a transient mock-side failure into a skip. The
// mock rate-limits and occasionally 503s, and three CI matrix jobs hit it at
// once; a shared public endpoint must never be able to fail the build.
func skipIfUnavailable(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var apiErr *Error
	if as(err, &apiErr) {
		switch apiErr.HTTPStatus {
		case http.StatusTooManyRequests, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			t.Skipf("porkbun mock API returned %d, skipping: %v", apiErr.HTTPStatus, err)
		}
		return
	}
	// A transport-level failure is never this package's bug either.
	t.Skipf("porkbun mock API call failed at the transport level, skipping: %v", err)
}

func TestMockGetNameservers(t *testing.T) {
	c := mockClient(t)
	ns, err := c.GetNameservers(context.Background(), "example.com")
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("GetNameservers: %v", err)
	}
	if len(ns) == 0 {
		t.Fatal("expected the mock to return some nameservers")
	}
	for _, n := range ns {
		if n != NormalizeNameserver(n) {
			t.Errorf("%q came back un-normalized", n)
		}
	}
}

func TestMockUpdateNameservers(t *testing.T) {
	c := mockClient(t)
	err := c.UpdateNameservers(context.Background(), "example.com", []string{"ns1.example.com", "ns2.example.com"})
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("UpdateNameservers: %v", err)
	}
}

// TestMockErrorShape pins the error contract this client depends on: the
// documented ?status=error switch answers HTTP 400 with a JSON body carrying
// status, message, code and next_action.
func TestMockErrorShape(t *testing.T) {
	c := mockClient(t)

	var out getNsResponse
	err := c.get(context.Background(), "domain/getNs/example.com", map[string][]string{"status": {"error"}}, &out)
	if err == nil {
		t.Fatal("?status=error must produce an error")
	}
	var apiErr *Error
	if !as(err, &apiErr) {
		t.Skipf("porkbun mock API failed at the transport level, skipping: %v", err)
	}
	if apiErr.HTTPStatus >= 500 || apiErr.HTTPStatus == http.StatusTooManyRequests {
		t.Skipf("porkbun mock API returned %d, skipping: %v", apiErr.HTTPStatus, err)
	}
	if !strings.EqualFold(apiErr.Status, "ERROR") {
		t.Errorf("Status = %q", apiErr.Status)
	}
	if apiErr.Code == "" {
		t.Error("error responses should carry a machine-readable code")
	}
	if apiErr.Message == "" {
		t.Error("error responses should carry a message")
	}
}

func TestMockDomainMetadata(t *testing.T) {
	c := mockClient(t)

	dom, err := c.GetDomain(context.Background(), "example.com")
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("GetDomain: %v", err)
	}
	if dom.Domain == "" || dom.TLD == "" {
		t.Errorf("domain metadata did not decode: %+v", dom)
	}

	domains, err := c.ListDomains(context.Background(), ListDomainsOptions{})
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(domains) == 0 {
		t.Fatal("expected at least one domain from the mock")
	}
}

func TestMockDNSRecords(t *testing.T) {
	c := mockClient(t)

	records, err := c.RetrieveRecords(context.Background(), "example.com")
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("RetrieveRecords: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one record from the mock")
	}
	// The spec types ttl as a string on read and an integer on write; if that
	// stops decoding, every record grows a permanent diff.
	if records[0].TTL.Int64() == 0 {
		t.Errorf("ttl did not decode from the mock's string form: %+v", records[0])
	}

	_, _, err = c.CreateRecord(context.Background(), "example.com", RecordInput{
		Name: "www", Type: "A", Content: "1.2.3.4", TTL: 600,
	})
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}

	err = c.EditRecord(context.Background(), "example.com", "123456789", RecordInput{
		Name: "www", Type: "A", Content: "1.2.3.5", TTL: 600,
	})
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("EditRecord: %v", err)
	}

	err = c.DeleteRecord(context.Background(), "example.com", "123456789")
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
}
