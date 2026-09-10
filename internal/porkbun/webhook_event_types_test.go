package porkbun

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Tier 1: prove /webhook/eventTypes still answers with an `eventTypes` array
// of strings.
//
// Nothing here asserts which event types come back. The mock and the prose
// reference already disagree — the prose documents nine, the mock serves
// seven, omitting the two cloudflare.connect.* types — and the whole point
// of the endpoint is that the catalog grows. Pinning membership or a count
// would break this build the next time Porkbun adds an event.
func TestMockWebhookEventTypes(t *testing.T) {
	c := mockClient(t)

	got, err := c.ListWebhookEventTypes(context.Background())
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("ListWebhookEventTypes: %v", err)
	}
	// A rename of the `eventTypes` field decodes to nil rather than failing,
	// so an empty result is the signal that the response shape moved.
	if len(got) == 0 {
		t.Fatal("expected the mock to return at least one event type; did the eventTypes field get renamed?")
	}
	for _, e := range got {
		if strings.TrimSpace(e) == "" {
			t.Errorf("event type %q is blank after normalization", e)
		}
	}

	// Cross-check the decode against the raw body rather than against a
	// hard-coded list: whatever the mock said, that is what must have
	// decoded.
	var raw struct {
		EventTypes []string `json:"eventTypes"`
	}
	if err := c.get(context.Background(), "webhook/eventTypes", nil, &raw); err != nil {
		skipIfUnavailable(t, err)
		t.Fatalf("raw GET webhook/eventTypes: %v", err)
	}
	if len(raw.EventTypes) == 0 {
		t.Fatal("raw response carried no eventTypes array")
	}
	inDecoded := make(map[string]struct{}, len(got))
	for _, e := range got {
		inDecoded[e] = struct{}{}
	}
	for _, e := range raw.EventTypes {
		if _, ok := inDecoded[strings.TrimSpace(e)]; !ok {
			t.Errorf("%q was in the raw response but not in the decoded catalog", e)
		}
	}
}

// The mock serves the eventTypes entries as plain JSON strings. If Porkbun
// ever wraps them in objects the client stops decoding, so pin the element
// type against the live body.
func TestMockWebhookEventTypesAreStrings(t *testing.T) {
	c := mockClient(t)

	var body map[string]json.RawMessage
	if err := c.get(context.Background(), "webhook/eventTypes", nil, &body); err != nil {
		skipIfUnavailable(t, err)
		t.Fatalf("GET webhook/eventTypes: %v", err)
	}
	arr, ok := body["eventTypes"]
	if !ok {
		t.Fatal("response has no eventTypes field")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(arr, &items); err != nil {
		t.Fatalf("eventTypes is not a JSON array: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("eventTypes array is empty")
	}
	for i, it := range items {
		var s string
		if err := json.Unmarshal(it, &s); err != nil {
			t.Errorf("eventTypes[%d] = %s is not a JSON string: %v", i, it, err)
		}
	}
}

func TestNormalizeEventTypes(t *testing.T) {
	t.Parallel()

	// nil must survive as nil: it is how "the API sent no eventTypes field"
	// reaches the data source, which renders it as a null rather than as an
	// empty catalog Porkbun never claimed.
	if got := NormalizeEventTypes(nil); got != nil {
		t.Errorf("NormalizeEventTypes(nil) = %v, want nil", got)
	}
	if got := NormalizeEventTypes([]string{}); got == nil || len(got) != 0 {
		t.Errorf("NormalizeEventTypes([]) = %v, want a non-nil empty slice", got)
	}
	if got := NormalizeEventTypes([]string{"  ", ""}); got == nil || len(got) != 0 {
		t.Errorf("an all-blank catalog should stay empty and non-nil, got %v", got)
	}

	in := []string{
		"dns.record.updated",
		"  domain.registered  ",
		"dns.record.updated",
		"",
		"cloudflare.connect.failed",
		"domain.registered",
	}
	want := []string{"cloudflare.connect.failed", "dns.record.updated", "domain.registered"}
	got := NormalizeEventTypes(in)
	if len(got) != len(want) {
		t.Fatalf("NormalizeEventTypes(%v) = %v, want %v", in, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormalizeEventTypes(%v) = %v, want %v", in, got, want)
		}
	}

	// Case is deliberately not folded.
	if got := NormalizeEventTypes([]string{"DNS.Record.Created"}); len(got) != 1 || got[0] != "DNS.Record.Created" {
		t.Errorf("case should be preserved, got %v", got)
	}
}
