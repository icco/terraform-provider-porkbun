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
// minItems) and the behaviour is documented nowhere, so it could de-delegate
// a live domain. The client refuses rather than find out in production.
var ErrNoNameservers = errors.New("refusing to send an empty nameserver list to porkbun: /domain/updateNs behaviour for ns:[] is undocumented and could de-delegate the domain")

// MinNameservers and MaxNameservers bound the delegation the provider
// accepts. UpdateNameservers enforces the floor; the ceiling is enforced
// only by the resource schema validator.
const (
	MinNameservers = 2
	MaxNameservers = 13
)

// ErrTooFewNameservers guards the floor after normalization rather than
// before it. The resource schema validator counts the strings as configured,
// but NormalizeNameservers folds case, strips trailing dots and
// de-duplicates: ["ns1.example.com", "ns1.example.com."] satisfies the
// validator and still puts a single-nameserver delegation on the wire. Both
// sides normalize before comparing, so it never surfaces as drift either.
var ErrTooFewNameservers = errors.New("refusing to send fewer than two distinct nameservers to porkbun: a single-nameserver delegation has no redundancy")

// NormalizeNameserver lowercases a nameserver hostname and strips the root
// label's trailing dot. The same nameserver arrives spelled three ways —
// Cloud DNS emits "ns-cloud-a1.googledomains.com." with the dot, people type
// it without, and registries may change the case — and unless the read and
// write sides fold all three to one spelling, every plan shows drift that no
// apply can fix.
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

// UpdateNameservers sets the registry nameservers for the domain. There is
// no create and no delete: a registered domain always has nameservers, so
// this is the only write operation for delegation.
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
