package porkbun

import (
	"context"
	"sort"
	"strings"
)

// webhookEventTypesResponse is the /webhook/eventTypes body. A nil EventTypes
// means the field was absent or null, which is not the same as a catalog
// that came back empty; ListWebhookEventTypes preserves the difference so
// callers can report "Porkbun told us nothing" separately from "Porkbun told
// us nothing is subscribable".
type webhookEventTypesResponse struct {
	EventTypes []string `json:"eventTypes"`
}

// ListWebhookEventTypes returns the catalog of event types a webhook
// endpoint can subscribe to.
//
// The catalog is not the full set of legal subscription values: Porkbun also
// accepts a prefix wildcard (`dns.*`) and `*`, and neither is ever listed
// here. Validating a subscription list against this slice alone would reject
// the wildcards Porkbun itself recommends.
func (c *Client) ListWebhookEventTypes(ctx context.Context) ([]string, error) {
	var out webhookEventTypesResponse
	if err := c.get(ctx, "webhook/eventTypes", nil, &out); err != nil {
		return nil, err
	}
	return NormalizeEventTypes(out.EventTypes), nil
}

// NormalizeEventTypes trims, drops empties, de-duplicates and sorts. The
// catalog is a set, and Porkbun does not promise an order; sorting keeps
// what lands in state independent of how the API happens to group the
// entries today.
//
// Case is left alone. Event types are documented lowercase, so folding it
// here would hide the API starting to send something else.
func NormalizeEventTypes(in []string) []string {
	if in == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, e := range in {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}
