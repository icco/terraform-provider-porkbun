package porkbun

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

// retryingClient has the default retry budget with millisecond waits.
func retryingClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := New(Config{APIKey: "pk1_test", SecretKey: "sk1_test", BaseURL: baseURL, MaxRetries: -1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.http.RetryWaitMin = time.Millisecond
	c.http.RetryWaitMax = 5 * time.Millisecond
	return c
}

func TestRetriesServerErrorThenSucceeds(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	keys := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys <- r.Header.Get("Idempotency-Key")
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"ERROR","message":"try later"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"SUCCESS","yourIp":"203.0.113.9"}`))
	}))
	defer srv.Close()

	ip, err := retryingClient(t, srv.URL).Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if ip != "203.0.113.9" {
		t.Errorf("ip = %q, want the successful third response", ip)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
	close(keys)
	first := <-keys
	if first == "" {
		t.Fatal("POST carried no Idempotency-Key")
	}
	for k := range keys {
		if k != first {
			t.Errorf("Idempotency-Key changed across retries: %q then %q", first, k)
		}
	}
}

func TestGivesUpAfterFiveTries(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"status":"ERROR","message":"still broken","code":"INTERNAL"}`))
	}))
	defer srv.Close()

	_, err := retryingClient(t, srv.URL).GetNameservers(context.Background(), "example.com")
	if err == nil {
		t.Fatal("expected an error once the retry budget is spent")
	}
	if got := attempts.Load(); got != 5 {
		t.Errorf("attempts = %d, want 5 (1 + DefaultMaxRetries)", got)
	}
	var apiErr *Error
	if !as(err, &apiErr) || apiErr.HTTPStatus != http.StatusInternalServerError || apiErr.Code != "INTERNAL" {
		t.Errorf("err = %v, want the last 500 response's body as an *Error", err)
	}
}

func TestDoesNotRetryClientError(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"ERROR","message":"Invalid domain.","code":"INVALID_DOMAIN",` +
			`"next_action":{"type":"retry","hint":"lying"}}`))
	}))
	defer srv.Close()

	if _, err := retryingClient(t, srv.URL).GetNameservers(context.Background(), "example.com"); err == nil {
		t.Fatal("expected an error")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1: 4xx is not retried, even when next_action says retry", got)
	}
}

func TestZeroRetriesMeansOneTry(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	if _, err := testClient(t, srv.URL).GetNameservers(context.Background(), "example.com"); err == nil {
		t.Fatal("expected an error")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 with MaxRetries 0", got)
	}
}

func TestRetryPolicy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		status int
		want   bool
	}{
		{200, false}, {400, false}, {404, false}, {429, true},
		{500, true}, {501, true}, {502, true}, {503, true}, {504, true}, {599, true},
	} {
		got, err := retryPolicy(context.Background(), &http.Response{StatusCode: tc.status}, nil)
		if err != nil || got != tc.want {
			t.Errorf("retryPolicy(%d) = %v, %v; want %v, nil", tc.status, got, err, tc.want)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := retryPolicy(ctx, &http.Response{StatusCode: http.StatusInternalServerError}, nil); got || err == nil {
		t.Errorf("cancelled context must stop retrying, got %v, %v", got, err)
	}
}

// TestBackoffIsExponential pins the curve: 0.5s, 1s, 2s, 4s, capped at 30s.
func TestBackoffIsExponential(t *testing.T) {
	t.Parallel()

	c, err := New(Config{BaseURL: "https://example.com", MaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	if c.http.RetryMax != DefaultMaxRetries || DefaultMaxRetries != 4 {
		t.Fatalf("RetryMax = %d, want 4 so that a call is tried five times", c.http.RetryMax)
	}
	resp := &http.Response{StatusCode: http.StatusInternalServerError}
	want := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second}
	for attempt, w := range want {
		if got := c.http.Backoff(c.http.RetryWaitMin, c.http.RetryWaitMax, attempt, resp); got != w {
			t.Errorf("backoff after attempt %d = %v, want %v", attempt+1, got, w)
		}
	}
	if got := c.http.Backoff(c.http.RetryWaitMin, c.http.RetryWaitMax, 10, resp); got != c.http.RetryWaitMax {
		t.Errorf("backoff must cap at RetryWaitMax, got %v", got)
	}

	// A Retry-After on 503 wins over the curve.
	ra := &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": {"7"}}}
	if got := retryablehttp.DefaultBackoff(c.http.RetryWaitMin, c.http.RetryWaitMax, 0, ra); got != 7*time.Second {
		t.Errorf("Retry-After ignored, got %v", got)
	}
}
