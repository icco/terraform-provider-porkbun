package porkbun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWarningListUnmarshal(t *testing.T) {
	t.Parallel()

	// The field is undeclared in the OpenAPI spec, so every shape the prose
	// docs could plausibly mean has to decode, and an unrecognised one must
	// degrade to "no warnings" rather than failing an otherwise good call.
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"null", `{"warnings":null}`, nil},
		{"absent", `{}`, nil},
		{"empty array", `{"warnings":[]}`, nil},
		{"string array", `{"warnings":["dns is served by cloudflare"]}`, []string{"dns is served by cloudflare"}},
		{"bare string", `{"warnings":"heads up"}`, []string{"heads up"}},
		{
			"object array",
			`{"warnings":[{"code":"CLOUDFLARE_SERVES_DNS","message":"not resolvable"}]}`,
			[]string{"[CLOUDFLARE_SERVES_DNS] not resolvable"},
		},
		{"bare object", `{"warnings":{"message":"just a message"}}`, []string{"just a message"}},
		{"code only", `{"warnings":[{"code":"ONLY_CODE"}]}`, []string{"ONLY_CODE"}},
		{"mixed", `{"warnings":["a",{"code":"B"}]}`, []string{"a", "B"}},
		{"blank entries dropped", `{"warnings":["","  ",{},null]}`, nil},
		// Degradation, not failure.
		{"unexpected number", `{"warnings":[42]}`, nil},
		{"unexpected shape", `{"warnings":123}`, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var env statusEnvelope
			if err := json.Unmarshal([]byte(tc.in), &env); err != nil {
				t.Fatalf("unmarshal %s: %v", tc.in, err)
			}
			got := make([]string, 0, len(env.Warnings))
			for _, w := range env.Warnings {
				got = append(got, w.String())
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("warning %d: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// A successful response carrying warnings must still decode into out, and
// the warnings must reach a collector installed on the context. This is the
// Cloudflare-moved-domain case: the write succeeds, so returning an error
// would be wrong, but reporting plain success would hide that nothing
// resolvable changed.
func TestWarningsCollectedOnSuccess(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"SUCCESS",
			"id":"12345",
			"warnings":[{"code":"CLOUDFLARE_SERVES_DNS","message":"this zone is served by cloudflare"}]
		}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, MaxRetries: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, wc := WithWarningCollector(context.Background())
	id, _, err := c.CreateRecord(ctx, "example.com", RecordInput{Name: "www", Type: "A", Content: "1.2.3.4"})
	if err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	if id != "12345" {
		t.Errorf("id: got %q, want %q", id, "12345")
	}

	got := wc.Warnings()
	if len(got) != 1 {
		t.Fatalf("got %d warnings, want 1: %v", len(got), got)
	}
	if want := "[CLOUDFLARE_SERVES_DNS] this zone is served by cloudflare"; got[0].String() != want {
		t.Errorf("got %q, want %q", got[0].String(), want)
	}
}

// Calls made without a collector must not panic, and duplicates from
// repeated calls collapse: one apply can touch an endpoint several times and
// three identical "cloudflare serves this zone" lines is noise.
func TestWarningCollectorAbsentAndDeduplicated(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"SUCCESS","warnings":["same warning"]}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, MaxRetries: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// No collector on the context: must be a no-op, not a nil dereference.
	if err := c.DeleteRecord(context.Background(), "example.com", "1"); err != nil {
		t.Fatalf("DeleteRecord without collector: %v", err)
	}

	ctx, wc := WithWarningCollector(context.Background())
	for range 3 {
		if err := c.DeleteRecord(ctx, "example.com", "1"); err != nil {
			t.Fatalf("DeleteRecord: %v", err)
		}
	}
	if got := wc.Warnings(); len(got) != 1 {
		t.Fatalf("got %d warnings after 3 identical calls, want 1: %v", len(got), got)
	}

	// A nil collector answers empty rather than panicking.
	var nilWC *WarningCollector
	if got := nilWC.Warnings(); got != nil {
		t.Errorf("nil collector: got %v, want nil", got)
	}
}
