package porkbun

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrNoNameservers: ns:[] is schema-legal but undocumented, and could
// de-delegate a live domain.
var ErrNoNameservers = errors.New("refusing to send an empty nameserver list to porkbun: /domain/updateNs behaviour for ns:[] is undocumented and could de-delegate the domain")

// UpdateNameservers enforces the floor; the schema validator enforces the
// ceiling.
const (
	MinNameservers = 2
	MaxNameservers = 13
)

// ErrTooFewNameservers is checked after normalization: the schema validator
// counts configured strings, and duplicates that differ only in case or a
// trailing dot collapse into one.
var ErrTooFewNameservers = errors.New("refusing to send fewer than two distinct nameservers to porkbun: a single-nameserver delegation has no redundancy")

// NormalizeNameserver folds the spellings the same host arrives in --
// trailing dot from Cloud DNS, no dot from humans, any case from registries.
func NormalizeNameserver(ns string) string {
	ns = strings.TrimSpace(ns)
	ns = strings.TrimSuffix(ns, ".")
	return strings.ToLower(ns)
}

// NormalizeNameservers normalizes, drops empties, de-duplicates and sorts.
// NS RRset order is not meaningful (RFC 1034/2181).
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
