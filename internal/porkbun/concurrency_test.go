package porkbun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"
)

func TestClientSerializesConcurrentRequests(t *testing.T) {
	t.Parallel()

	var active, overlapping atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if active.Add(1) != 1 {
			overlapping.Add(1)
			active.Add(-1)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"ERROR","message":"concurrent request"}`))
			return
		}
		defer active.Add(-1)
		// Keep the request active long enough for concurrent callers to overlap.
		time.Sleep(5 * time.Millisecond)
		_, _ = w.Write([]byte(`{"status":"SUCCESS","ns":["a.example.com","b.example.com"]}`))
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const calls = 24
	results := make(chan error, calls)
	start := make(chan struct{})
	for i := range calls {
		go func() {
			<-start
			if i%2 == 0 {
				_, err := c.GetNameservers(ctx, "example.com")
				results <- err
			} else {
				results <- c.UpdateNameservers(ctx, "example.com", []string{"a.example.com", "b.example.com"})
			}
		}()
	}
	close(start)
	for range calls {
		if err := <-results; err != nil {
			t.Errorf("concurrent caller failed: %v", err)
		}
	}
	if got := overlapping.Load(); got != 0 {
		t.Errorf("overlapping requests = %d, want 0", got)
	}
}

func TestClientQueuedRequestHonorsContext(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	unblock := make(chan struct{})
	release := sync.OnceFunc(func() { close(unblock) })
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-unblock
		}
		_, _ = w.Write([]byte(`{"status":"SUCCESS","ns":[]}`))
	}))
	defer srv.Close()
	defer release()

	c := testClient(t, srv.URL)
	first := make(chan error, 1)
	go func() {
		_, err := c.GetNameservers(context.Background(), "first.example.com")
		first <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not reach server")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.GetNameservers(ctx, "queued.example.com")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("queued request error = %v, want context deadline exceeded", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls = %d, want 1; canceled queued request must not be sent", got)
	}
	release()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetNameservers(context.Background(), "after.example.com"); err != nil {
		t.Fatalf("request after cancellation failed: %v", err)
	}
}

func TestClientHoldsRequestSlotAcrossRetries(t *testing.T) {
	t.Parallel()

	backoffStarted := make(chan struct{})
	unblock := make(chan struct{})
	release := sync.OnceFunc(func() { close(unblock) })
	var attempts, otherCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/domain/getNs/retry.example.com" {
			if attempts.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"status":"ERROR"}`))
				return
			}
		} else {
			otherCalls.Add(1)
		}
		_, _ = w.Write([]byte(`{"status":"SUCCESS","ns":[]}`))
	}))
	defer srv.Close()
	defer release()

	c := retryingClient(t, srv.URL)
	c.http.Backoff = func(_, _ time.Duration, _ int, _ *http.Response) time.Duration {
		close(backoffStarted)
		<-unblock
		return 0
	}
	first := make(chan error, 1)
	go func() {
		_, err := c.GetNameservers(context.Background(), "retry.example.com")
		first <- err
	}()
	select {
	case <-backoffStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not enter retry backoff")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.GetNameservers(ctx, "queued.example.com"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("queued request error = %v, want context deadline exceeded", err)
	}
	if got := otherCalls.Load(); got != 0 {
		t.Errorf("other calls during retry backoff = %d, want 0", got)
	}
	release()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("retry attempts = %d, want 2", got)
	}
}

func TestClientReleasesRequestSlotAfterError(t *testing.T) {
	t.Parallel()

	for name, response := range map[string]string{
		"API error":      `{"status":"ERROR","code":"INTERNAL"}`,
		"decode error":   `not JSON`,
		"response error": `{"status":"SUCCESS","ns":42}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					_, _ = w.Write([]byte(response))
					return
				}
				_, _ = w.Write([]byte(`{"status":"SUCCESS","ns":[]}`))
			}))
			defer srv.Close()
			c := testClient(t, srv.URL)
			if _, err := c.GetNameservers(context.Background(), "first.example.com"); err == nil {
				t.Fatal("expected first request to fail")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := c.GetNameservers(ctx, "after.example.com"); err != nil {
				t.Fatalf("request slot was not released: %v", err)
			}
		})
	}
}

func TestClientSerializationIsNotGlobal(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	unblock := make(chan struct{})
	release := sync.OnceFunc(func() { close(unblock) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/domain/getNs/blocked.example.com" {
			close(started)
			<-unblock
		}
		_, _ = w.Write([]byte(`{"status":"SUCCESS","ns":[]}`))
	}))
	defer srv.Close()
	defer release()

	firstClient := testClient(t, srv.URL)
	secondClient := testClient(t, srv.URL)
	first := make(chan error, 1)
	go func() {
		_, err := firstClient.GetNameservers(context.Background(), "blocked.example.com")
		first <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not reach server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := secondClient.GetNameservers(ctx, "independent.example.com"); err != nil {
		t.Errorf("independent client was blocked: %v", err)
	}
	release()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func successfulResponse(body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}
}

func TestClientReleasesRequestSlotAfterActiveCancellation(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	c := testClient(t, "https://example.com")
	c.http.HTTPClient.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/domain/getNs/blocked.example.com" {
			close(started)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return successfulResponse(io.NopCloser(strings.NewReader(`{"status":"SUCCESS","ns":[]}`))), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() {
		_, err := c.GetNameservers(ctx, "blocked.example.com")
		first <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not start")
	}
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("active request error = %v, want context canceled", err)
	}
	after, cancelAfter := context.WithTimeout(context.Background(), time.Second)
	defer cancelAfter()
	if _, err := c.GetNameservers(after, "after.example.com"); err != nil {
		t.Fatalf("request slot was not released after active cancellation: %v", err)
	}
}

func TestClientReleasesRequestSlotAfterIOError(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"transport", "body read"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := testClient(t, "https://example.com")
			var calls atomic.Int32
			c.http.HTTPClient.Transport = transportFunc(func(_ *http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 {
					if name == "transport" {
						return nil, errors.New("transport failed")
					}
					return successfulResponse(io.NopCloser(iotest.ErrReader(errors.New("read failed")))), nil
				}
				return successfulResponse(io.NopCloser(strings.NewReader(`{"status":"SUCCESS","ns":[]}`))), nil
			})
			if _, err := c.GetNameservers(context.Background(), "first.example.com"); err == nil {
				t.Fatal("expected first request to fail")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := c.GetNameservers(ctx, "after.example.com"); err != nil {
				t.Fatalf("request slot was not released: %v", err)
			}
		})
	}
}

type blockingCloseBody struct {
	io.Reader
	started chan struct{}
	unblock <-chan struct{}
}

func (b *blockingCloseBody) Close() error {
	close(b.started)
	<-b.unblock
	return nil
}

func TestClientHoldsRequestSlotUntilBodyClosed(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	unblock := make(chan struct{})
	release := sync.OnceFunc(func() { close(unblock) })
	defer release()
	c := testClient(t, "https://example.com")
	var calls atomic.Int32
	c.http.HTTPClient.Transport = transportFunc(func(_ *http.Request) (*http.Response, error) {
		body := io.NopCloser(strings.NewReader(`{"status":"SUCCESS","ns":[]}`))
		if calls.Add(1) == 1 {
			body = &blockingCloseBody{Reader: body, started: started, unblock: unblock}
		}
		return successfulResponse(body), nil
	})
	first := make(chan error, 1)
	go func() {
		_, err := c.GetNameservers(context.Background(), "first.example.com")
		first <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("response body did not enter Close")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.GetNameservers(ctx, "queued.example.com"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("queued request error = %v, want context deadline exceeded", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("transport calls = %d, want 1 until the first body is closed", got)
	}
	release()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetNameservers(context.Background(), "after.example.com"); err != nil {
		t.Fatalf("request after body close failed: %v", err)
	}
}
