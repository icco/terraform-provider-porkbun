package porkbun

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

// TLDPricing is Porkbun's default price sheet for one TLD, from
// /pricing/get.
//
// Every amount stays a string. Porkbun formats money for display, with a
// thousands separator: .security arrives as "2,060.25". Parsing that to a
// float here would either fail or, worse, round the money silently, and the
// caller cannot tell which prices are affected until one of them is.
type TLDPricing struct {
	Registration flexString `json:"registration"`
	Renewal      flexString `json:"renewal"`
	Transfer     flexString `json:"transfer"`
	// SpecialType is set only for TLDs that are not conventional
	// registrations; "handshake" is the only value the catalog uses today.
	SpecialType flexString `json:"specialType"`
	// Coupons is keyed by the product the promotion applies to, e.g.
	// "registration".
	Coupons CouponSet `json:"coupons"`
}

// Coupon is one active promotion on a TLD.
type Coupon struct {
	Code       flexString `json:"code"`
	MaxPerUser flexInt    `json:"max_per_user"`
	// FirstYearOnly is Porkbun's "yes"/"no" string, not a JSON boolean.
	FirstYearOnly flexString `json:"first_year_only"`
	Type          flexString `json:"type"`
	// Amount is the discount. A string for the same reason the prices are.
	Amount flexString `json:"amount"`
}

// CouponSet is a TLD's coupons keyed by product type.
//
// It needs its own decoder because Porkbun sends the field as a JSON array
// when a TLD has no promotion: the value is a PHP associative array, and
// json_encode renders an empty one as [] rather than {}. That is the common
// case — the whole live catalog is currently "coupons": [] — so a plain
// map[string]Coupon fails to decode every response.
type CouponSet map[string]Coupon

func (cs *CouponSet) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		*cs = nil
		return nil
	}
	if strings.HasPrefix(s, "[") {
		// Only the empty array has been observed. Key a populated one by
		// index rather than dropping it: losing a live discount quietly is
		// worse than an odd key.
		var list []Coupon
		if err := json.Unmarshal(b, &list); err != nil {
			return err
		}
		if len(list) == 0 {
			*cs = nil
			return nil
		}
		m := make(CouponSet, len(list))
		for i, c := range list {
			m[strconv.Itoa(i)] = c
		}
		*cs = m
		return nil
	}
	var m map[string]Coupon
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*cs = m
	return nil
}

// NormalizeTLD folds a TLD to the spelling Porkbun keys its price sheet by:
// trimmed, lowercase, no leading dot.
func NormalizeTLD(s string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), ".")
}

// GetPricing returns Porkbun's default pricing keyed by TLD. The endpoint is
// public: it needs no credentials, so this works with an unauthenticated
// client.
//
// tlds selects a subset, but the selection happens here rather than through
// the API's own `tlds` body parameter. /pricing/get answers a TLD it does
// not sell with an invented entry priced "0.00" instead of an error, and
// "0.00" is a genuine price (.fly costs nothing, .uk transfers are free), so
// nothing in a server-filtered response distinguishes a typo from a real
// quote. Filtering the full catalog locally leaves an unsold TLD absent,
// which the caller can act on.
func (c *Client) GetPricing(ctx context.Context, tlds []string) (map[string]TLDPricing, error) {
	var out struct {
		Pricing map[string]TLDPricing `json:"pricing"`
	}
	if err := c.get(ctx, "pricing/get", nil, &out); err != nil {
		return nil, err
	}
	if out.Pricing == nil {
		out.Pricing = map[string]TLDPricing{}
	}
	if len(tlds) == 0 {
		return out.Pricing, nil
	}

	filtered := make(map[string]TLDPricing, len(tlds))
	for _, t := range tlds {
		t = NormalizeTLD(t)
		if p, ok := out.Pricing[t]; ok {
			filtered[t] = p
		}
	}
	return filtered, nil
}
