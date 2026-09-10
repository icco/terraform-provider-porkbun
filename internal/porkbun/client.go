// Package porkbun is a hand-written client for the subset of the Porkbun v3
// JSON API this provider needs.
//
// Two API behaviours shape it: errors arrive in the JSON body rather than the
// HTTP status (200 and 400 both carry {"status":"ERROR"}), so status is what
// is checked; and credentials go in headers, not the body, to keep them out
// of anything that logs payloads.
package porkbun

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// DefaultBaseURL is the production API. api-ipv4.porkbun.com is the same API
// on an A-records-only host, for callers with a broken IPv6 path.
const DefaultBaseURL = "https://api.porkbun.com/api/json/v3"

// MockBaseURL serves schema-shaped placeholder responses without
// authentication. It is stateless: writes are not reflected in later reads.
const MockBaseURL = DefaultBaseURL + "/mock"

const defaultMaxRetries = 3

// Config configures a Client.
type Config struct {
	APIKey    string
	SecretKey string
	BaseURL   string
	// MaxRetries is the retry budget for failed calls. Zero means no retries;
	// a negative value selects the package default.
	MaxRetries int
	UserAgent  string
	// HTTPClient, when set, replaces the underlying transport. Used by tests.
	HTTPClient *http.Client
}

// Client talks to the Porkbun v3 JSON API.
type Client struct {
	apiKey    string
	secretKey string
	baseURL   *url.URL
	userAgent string
	http      *retryablehttp.Client
}

// New builds a Client. It returns an error only if BaseURL is not an
// absolute URL.
func New(cfg Config) (*Client, error) {
	raw := cfg.BaseURL
	if raw == "" {
		raw = DefaultBaseURL
	}
	base, err := url.Parse(strings.TrimSuffix(raw, "/"))
	if err != nil {
		return nil, fmt.Errorf("parsing base_url %q: %w", raw, err)
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("base_url %q must be an absolute URL", raw)
	}

	retries := cfg.MaxRetries
	if retries < 0 {
		retries = defaultMaxRetries
	}

	rc := retryablehttp.NewClient()
	rc.RetryMax = retries
	rc.RetryWaitMin = 500 * time.Millisecond
	rc.RetryWaitMax = 30 * time.Second
	// DefaultBackoff honours Retry-After on 429 and 503.
	rc.Backoff = retryablehttp.DefaultBackoff
	// This package logs through tflog.
	rc.Logger = nil
	// Hand back the last response instead of a "giving up after N attempts"
	// error: Porkbun puts the useful diagnosis in the body, including on the
	// 4xx and 5xx responses that exhaust the retry budget.
	rc.ErrorHandler = retryablehttp.PassthroughErrorHandler
	if cfg.HTTPClient != nil {
		rc.HTTPClient = cfg.HTTPClient
	}

	ua := cfg.UserAgent
	if ua == "" {
		ua = "terraform-provider-porkbun"
	}

	return &Client{
		apiKey:    cfg.APIKey,
		secretKey: cfg.SecretKey,
		baseURL:   base,
		userAgent: ua,
		http:      rc,
	}, nil
}

// BaseURL returns the configured API root.
func (c *Client) BaseURL() string { return c.baseURL.String() }

// statusEnvelope is the success/failure part of every Porkbun response,
// decoded before the caller's own struct.
type statusEnvelope struct {
	Status     string      `json:"status"`
	Message    string      `json:"message"`
	Code       string      `json:"code"`
	NextAction *NextAction `json:"next_action"`
	// Warnings is undeclared in the OpenAPI spec but promised by the prose
	// reference, most importantly on /dns/* writes to a domain whose DNS
	// Cloudflare now serves. See warnings.go.
	Warnings warningList `json:"warnings"`
}

// NextAction is Porkbun's machine-readable remediation hint.
type NextAction struct {
	Type string `json:"type"`
	Hint string `json:"hint"`
	URL  string `json:"url"`
}

// Error is a Porkbun API error. It is returned whenever the response body's
// "status" field is not SUCCESS, whatever the HTTP status code was, and also
// for HTTP-level failures that carry no usable JSON body.
type Error struct {
	// HTTPStatus is the HTTP status code the API answered with. It is
	// frequently 200 even for errors.
	HTTPStatus int
	// Status is the JSON "status" field, normally "ERROR".
	Status string
	// Message is the human-readable description, or the truncated response
	// body when the API answered an HTTP error with no usable JSON.
	Message string
	// Code is the machine-readable error code, e.g. DOMAIN_NOT_FOUND.
	Code string
	// NextAction is Porkbun's remediation hint, when present.
	NextAction *NextAction
	// Path is the API path that failed, for context in diagnostics.
	Path string
	// Raw is the response body, kept so callers can pull endpoint-specific
	// fields such as existingId out of an error.
	Raw json.RawMessage
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("porkbun api error")
	if e.Path != "" {
		fmt.Fprintf(&b, " on %s", e.Path)
	}
	if e.Code != "" {
		fmt.Fprintf(&b, " [%s]", e.Code)
	}
	fmt.Fprintf(&b, " (http %d)", e.HTTPStatus)
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	if e.NextAction != nil && e.NextAction.Hint != "" {
		fmt.Fprintf(&b, " — %s", e.NextAction.Hint)
	}
	return b.String()
}

// Retryable reports whether Porkbun said the call is worth retrying.
func (e *Error) Retryable() bool {
	return e.NextAction != nil && (e.NextAction.Type == "retry" || e.NextAction.Type == "wait_and_retry")
}

// ErrorCode returns the Porkbun error code of err, or "" if err is not an
// *Error.
func ErrorCode(err error) string {
	var apiErr *Error
	if as(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

// IsNotFound reports whether err means the domain or record is gone. Read
// uses it to drop the resource from state, so it is deliberately narrow:
// matching bare HTTP 404, or INVALID_DOMAIN ("invalid or not in your
// account"), would RemoveResource every domain on one bad base_url.
func IsNotFound(err error) bool {
	switch ErrorCode(err) {
	case "DOMAIN_NOT_FOUND", "RECORD_NOT_FOUND", "INVALID_RECORD_ID":
		return true
	}
	return false
}

// as is errors.As specialised to *Error.
func as(err error, target **Error) bool {
	for err != nil {
		if e, ok := err.(*Error); ok { //nolint:errorlint // unwrap loop below
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

// post performs a write. Every POST carries an Idempotency-Key, generated
// once per call so that it stays constant across this client's own retries.
func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	return c.do(ctx, http.MethodPost, path, nil, body, out)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	endpoint := c.baseURL.String() + "/" + strings.TrimPrefix(path, "/")
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request for %s: %w", path, err)
		}
	}

	req, err := retryablehttp.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building request for %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
		req.Header.Set("X-Secret-API-Key", c.secretKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost {
		key, err := idempotencyKey()
		if err != nil {
			return fmt.Errorf("generating idempotency key: %w", err)
		}
		req.Header.Set("Idempotency-Key", key)
	}

	tflog.Debug(ctx, "porkbun api request", map[string]any{"method": method, "path": path})

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling %s %s: %w", method, path, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("reading response from %s: %w", path, err)
	}

	logFields := map[string]any{"method": method, "path": path, "http_status": resp.StatusCode}
	if id := resp.Header.Get("X-Request-Id"); id != "" {
		logFields["x_request_id"] = id
	}
	if rem := resp.Header.Get("X-RateLimit-Remaining"); rem != "" {
		logFields["x_ratelimit_remaining"] = rem
	}
	tflog.Debug(ctx, "porkbun api response", logFields)

	var env statusEnvelope
	if jsonErr := json.Unmarshal(raw, &env); jsonErr != nil {
		// No usable JSON. Only an HTTP-level failure is actionable here.
		if resp.StatusCode >= 400 {
			return &Error{
				HTTPStatus: resp.StatusCode,
				Status:     "ERROR",
				Message:    strings.TrimSpace(truncate(string(raw), 512)),
				Path:       path,
				Raw:        raw,
			}
		}
		return fmt.Errorf("decoding response from %s (http %d): %w", path, resp.StatusCode, jsonErr)
	}

	// Branch on the body's status, never on the HTTP code: /domain/updateNs
	// answers 200 with {"status":"ERROR"} on some failures, and a client that
	// trusts the HTTP code would record nameservers that were never applied.
	if !strings.EqualFold(env.Status, "SUCCESS") {
		return &Error{
			HTTPStatus: resp.StatusCode,
			Status:     env.Status,
			Message:    env.Message,
			Code:       env.Code,
			NextAction: env.NextAction,
			Path:       path,
			Raw:        raw,
		}
	}
	if resp.StatusCode >= 400 {
		return &Error{
			HTTPStatus: resp.StatusCode,
			Status:     env.Status,
			Message:    env.Message,
			Code:       env.Code,
			NextAction: env.NextAction,
			Path:       path,
			Raw:        raw,
		}
	}

	// Log every warning, and hand them to a collector when the caller
	// installed one. This runs on the success path only: an error already
	// carries the whole body in Raw.
	if len(env.Warnings) > 0 {
		msgs := make([]string, 0, len(env.Warnings))
		for _, w := range env.Warnings {
			msgs = append(msgs, w.String())
		}
		tflog.Warn(ctx, "porkbun api warning", map[string]any{"path": path, "warnings": msgs})
		collectorFrom(ctx).add(env.Warnings)
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decoding response body from %s: %w", path, err)
	}
	return nil
}

func idempotencyKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Ping verifies credentials and returns the caller's public IP as Porkbun
// sees it. Handy for diagnosing IP_NOT_ALLOWED.
func (c *Client) Ping(ctx context.Context) (string, error) {
	var out struct {
		YourIP string `json:"yourIp"`
	}
	if err := c.post(ctx, "ping", map[string]any{}, &out); err != nil {
		return "", err
	}
	return out.YourIP, nil
}
