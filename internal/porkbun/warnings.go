package porkbun

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
)

// Warning is a non-fatal advisory Porkbun attaches to an otherwise
// successful response.
//
// The v3 OpenAPI spec does not declare this field on any response schema,
// but the prose reference promises it in cases the provider cannot afford to
// swallow. The load-bearing one: once a domain has been moved to a
// customer's own Cloudflare account, Porkbun's nameservers stop answering
// for it, yet "/dns/* writes still succeed against the Porkbun zone ... but
// they do not change what resolves — every /dns/* response for such a domain
// carries a warnings entry saying so". Without decoding it, a
// porkbun_dns_record apply against a moved domain reports success for a
// write that changes nothing resolvable.
//
// Because the shape is undeclared, both spellings the docs suggest are
// accepted: a bare string, and an object carrying a code and a message.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (w Warning) String() string {
	switch {
	case w.Code != "" && w.Message != "":
		return "[" + w.Code + "] " + w.Message
	case w.Code != "":
		return w.Code
	default:
		return w.Message
	}
}

// warningList decodes the undeclared `warnings` field, tolerating a list of
// strings, a list of objects, or a single one of either. An unrecognised
// shape decodes to nothing rather than failing the call: a warning is
// advisory, and rejecting a successful response over it would be worse than
// missing it.
type warningList []Warning

func (wl *warningList) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" || trimmed == "null" {
		return nil
	}

	var items []json.RawMessage
	if trimmed[0] == '[' {
		if err := json.Unmarshal(b, &items); err != nil {
			return nil //nolint:nilerr // advisory field; see type doc
		}
	} else {
		items = []json.RawMessage{b}
	}

	for _, raw := range items {
		r := strings.TrimSpace(string(raw))
		switch {
		case r == "" || r == "null":
		case r[0] == '"':
			var s string
			if json.Unmarshal(raw, &s) == nil && strings.TrimSpace(s) != "" {
				*wl = append(*wl, Warning{Message: s})
			}
		case r[0] == '{':
			var w Warning
			if json.Unmarshal(raw, &w) == nil && w.String() != "" {
				*wl = append(*wl, w)
			}
		}
	}
	return nil
}

// WarningCollector gathers the warnings Porkbun returned during a sequence
// of calls.
//
// It hangs off the context rather than the Client because a Client is shared
// by every resource in a run: a field on the Client would interleave
// warnings from concurrent applies and attribute them to the wrong resource.
type WarningCollector struct {
	mu    sync.Mutex
	items []Warning
}

type warningCollectorKey struct{}

// WithWarningCollector returns a context that accumulates the API warnings
// raised by calls made with it, and the collector holding them. Callers pass
// the context to client methods, then read Warnings to surface them.
func WithWarningCollector(ctx context.Context) (context.Context, *WarningCollector) {
	wc := &WarningCollector{}
	return context.WithValue(ctx, warningCollectorKey{}, wc), wc
}

// Warnings returns the warnings collected so far, de-duplicated and in the
// order first seen. A nil collector returns nothing, so callers need not
// branch.
func (wc *WarningCollector) Warnings() []Warning {
	if wc == nil {
		return nil
	}
	wc.mu.Lock()
	defer wc.mu.Unlock()

	seen := make(map[string]struct{}, len(wc.items))
	out := make([]Warning, 0, len(wc.items))
	for _, w := range wc.items {
		key := w.String()
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, w)
	}
	return out
}

func (wc *WarningCollector) add(ws []Warning) {
	if wc == nil || len(ws) == 0 {
		return
	}
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.items = append(wc.items, ws...)
}

func collectorFrom(ctx context.Context) *WarningCollector {
	wc, _ := ctx.Value(warningCollectorKey{}).(*WarningCollector)
	return wc
}
