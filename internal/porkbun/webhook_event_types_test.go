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
//
// Both assertions share one pair of calls on purpose: the mock rate-limits,
// and three CI matrix jobs hit it at once.
func TestMockWebhookEventTypes(t *testing.T) {
	c := mockClient(t)
	ctx := context.Background()

	// Read the raw body first, so the element type can be pinned. If Porkbun
	// ever wraps the entries in objects, the client silently stops decoding
	// them and only this check notices.
	var body map[string]json.RawMessage
	rawErr := c.get(ctx, "webhook/eventTypes", nil, &body)
	skipIfUnavailable(t, rawErr)
	if rawErr != nil {
		t.Fatalf("GET webhook/eventTypes: %v", rawErr)
	}
	arr, ok := body["eventTypes"]
	if !ok {
		t.Fatal("response has no eventTypes field; did it get renamed?")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(arr, &items); err != nil {
		t.Fatalf("eventTypes is not a JSON array: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("eventTypes array is empty")
	}
	rawTypes := make([]string, 0, len(items))
	for i, it := range items {
		var s string
		if err := json.Unmarshal(it, &s); err != nil {
			t.Errorf("eventTypes[%d] = %s is not a JSON string: %v", i, it, err)
			continue
		}
		rawTypes = append(rawTypes, strings.TrimSpace(s))
	}

	got, err := c.ListWebhookEventTypes(ctx)
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("ListWebhookEventTypes: %v", err)
	}
	// A rename of the field decodes to nil rather than failing, so an empty
	// result is the signal that the response shape moved.
	if len(got) == 0 {
		t.Fatal("expected at least one event type from the mock")
	}
	for _, e := range got {
		if strings.TrimSpace(e) == "" {
			t.Errorf("event type %q is blank after normalization", e)
		}
	}

	// Cross-check the decode against whatever the raw body actually said,
	// rather than against a hard-coded list.
	inDecoded := make(map[string]struct{}, len(got))
	for _, e := range got {
		inDecoded[e] = struct{}{}
	}
	for _, e := range rawTypes {
		if e == "" {
			continue
		}
		if _, ok := inDecoded[e]; !ok {
			t.Errorf("%q was in the raw response but not in the decoded catalog", e)
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
