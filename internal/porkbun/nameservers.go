package porkbun

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrNoNameservers guards against sending an empty nameserver array to
// /domain/updateNs. The schema permits it (`ns` is required but has no
// minItems) and the behaviour is documented nowhere: it might be rejected,
// no-op, restore Porkbun defaults, or de-delegate a live domain at the
// registry. None of those is a thing a Terraform apply should discover in
// production, so the client refuses.
var ErrNoNameservers = errors.New("refusing to send an empty nameserver list to porkbun: /domain/updateNs behaviour for ns:[] is undocumented and could de-delegate the domain")

// MinNameservers and MaxNameservers bound a workable delegation. Every
// registry requires at least two authoritative nameservers, and Porkbun's
// own UI will not save fewer.
const (
	MinNameservers = 2
	MaxNameservers = 13
)

// ErrTooFewNameservers guards the floor after normalization rather than
// before it.
//
// The resource schema enforces a minimum of two elements, but it counts the
// strings as configured, and NormalizeNameservers folds case, strips
// trailing dots and de-duplicates. ["ns1.example.com", "ns1.example.com."]
// is two distinct strings and one nameserver, so a config that satisfies the
// schema can still put a single-nameserver delegation on the wire. That
// leaves a domain one outage away from dark, and — because both sides
// normalize before comparing — it never shows up as drift. The floor
// therefore lives where the payload is built.
var ErrTooFewNameservers = errors.New("refusing to send fewer than two distinct nameservers to porkbun: a single-nameserver delegation has no redundancy")

// NormalizeNameserver lowercases a nameserver hostname and strips the root
// label's trailing dot.
//
// This exists because the same nameserver arrives spelled three ways:
// Cloud DNS hands out "ns-cloud-a1.googledomains.com." with a trailing dot,
// people type them without, and registries are free to change the case. If
// the provider does not fold all three to one spelling on both the read and
// the write side, every plan shows drift that no apply can fix.
func NormalizeNameserver(ns string) string {
	ns = strings.TrimSpace(ns)
	ns = strings.TrimSuffix(ns, ".")
	return strings.ToLower(ns)
}

// NormalizeNameservers normalizes every element, drops empties, and
// de-duplicates. The result is sorted so that two equal sets always render
// identically; order is not meaningful in an NS RRset (RFC 1034/2181).
func NormalizeNameservers(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, ns := range in {
		n := NormalizeNameserver(ns)
		if n == "" {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// SameNameserverSet reports whether two nameserver lists denote the same set
// after normalization, ignoring order, case, trailing dots and duplicates.
func SameNameserverSet(a, b []string) bool {
	na, nb := NormalizeNameservers(a), NormalizeNameservers(b)
	if len(na) != len(nb) {
		return false
	}
	for i := range na {
		if na[i] != nb[i] {
			return false
		}
	}
	return true
}

type getNsResponse struct {
	NS []string `json:"ns"`
}

// GetNameservers reads the authoritative nameservers the registry currently
// lists for the domain. The returned slice is normalized and sorted.
func (c *Client) GetNameservers(ctx context.Context, domain string) ([]string, error) {
	var out getNsResponse
	if err := c.get(ctx, "domain/getNs/"+escapePath(domain), nil, &out); err != nil {
		return nil, err
	}
	return NormalizeNameservers(out.NS), nil
}

// UpdateNameservers sets the registry nameservers for the domain. It refuses
// an empty list. There is no create and no delete: a registered domain always
// has nameservers, so this is the only write operation for delegation.
func (c *Client) UpdateNameservers(ctx context.Context, domain string, ns []string) error {
	normalized := NormalizeNameservers(ns)
	if len(normalized) == 0 {
		return fmt.Errorf("updating nameservers for %s: %w", domain, ErrNoNameservers)
	}
	if len(normalized) < MinNameservers {
		return fmt.Errorf("updating nameservers for %s: %d input(s) collapsed to %v: %w",
			domain, len(ns), normalized, ErrTooFewNameservers)
	}
	body := map[string]any{"ns": normalized}
	return c.post(ctx, "domain/updateNs/"+escapePath(domain), body, nil)
}
