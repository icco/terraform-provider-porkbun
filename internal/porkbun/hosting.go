package porkbun

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
)

// HostingPlan is one row of /hosting/plans: a plan that can be provisioned
// through the API. Both hosting products are listed together — Secure Static
// Hosting and Cloud for WordPress — and Product is what separates them.
type HostingPlan struct {
	// Product is the hosting product, e.g. secureStaticHosting.
	Product string
	// Plan is Porkbun's billing label for the term, e.g. monthly.
	Plan string
	// SKU is the identifier /hosting/create takes. Since v3.13 create is
	// driven by the SKU alone; product + plan is the pre-v3.13 spelling and
	// parts of the spec prose still describe it.
	SKU string
	// Interval is the billing interval the price covers.
	Interval string
	// Price is the plan price in cents, echoed back to /hosting/create as
	// acknowledgedCost. Nil when the API sent null or omitted the field: 0
	// cents is a real price (a free trial term), so it cannot double as
	// "unknown".
	Price *int64
	// PriceFormatted is Porkbun's display rendering of Price.
	PriceFormatted string
	// TrialDays is the free-trial length in days. Nil when absent; 0 means
	// the plan has no trial.
	TrialDays *int64
	// Name is the human-readable plan name.
	Name string
	// Features is the plan's feature bag. The spec types it as a bare object
	// with no declared properties, so values are kept as text: a JSON string
	// unquoted, anything else (number, boolean, nested object, array) as
	// compact JSON. Nil when the API omitted the field, which is distinct
	// from an empty bag.
	Features map[string]string
}

// ListHostingPlansOptions filters the plan list. /hosting/plans takes no
// request parameters, so both filters are applied client-side.
type ListHostingPlansOptions struct {
	// Product keeps only plans for this product, compared case-insensitively.
	// Empty means no filter.
	Product string
	// SKUPrefix keeps only plans whose SKU starts with it, compared
	// case-insensitively. Empty means no filter.
	SKUPrefix string
}

type hostingPlansResponse struct {
	Plans []hostingPlanJSON `json:"plans"`
}

type hostingPlanJSON struct {
	Product  flexString `json:"product"`
	Plan     flexString `json:"plan"`
	SKU      flexString `json:"sku"`
	Interval flexString `json:"interval"`
	// A *flexInt, not a flexInt: encoding/json nils a settable pointer on
	// null without calling the element's UnmarshalJSON, which is what keeps
	// "no price" apart from a price of 0 cents. The one gap is a quoted
	// empty string, which still routes through flexInt and lands as 0.
	Price          *flexInt                   `json:"price"`
	PriceFormatted flexString                 `json:"priceFormatted"`
	TrialDays      *flexInt                   `json:"trialDays"`
	Name           flexString                 `json:"name"`
	Features       map[string]json.RawMessage `json:"features"`
}

// ListHostingPlans reads the provisionable hosting plans. Results are sorted
// by SKU so the order is stable; the API does not promise one.
func (c *Client) ListHostingPlans(ctx context.Context, opts ListHostingPlansOptions) ([]HostingPlan, error) {
	var out hostingPlansResponse
	if err := c.get(ctx, "hosting/plans", nil, &out); err != nil {
		return nil, err
	}
	return hostingPlansFrom(out, opts), nil
}

// hostingPlansFrom converts and filters a decoded response. Split out of
// ListHostingPlans so the mock-shape test can drive it with a body the
// client itself refuses: Porkbun's /mock renders the hosting envelope's
// `status` from an untyped example, so it answers "status":"string" and the
// client — rightly — reads that as an error.
func hostingPlansFrom(out hostingPlansResponse, opts ListHostingPlansOptions) []HostingPlan {
	product := strings.ToLower(strings.TrimSpace(opts.Product))
	prefix := strings.ToUpper(strings.TrimSpace(opts.SKUPrefix))

	plans := make([]HostingPlan, 0, len(out.Plans))
	for _, raw := range out.Plans {
		p := HostingPlan{
			Product:        string(raw.Product),
			Plan:           string(raw.Plan),
			SKU:            string(raw.SKU),
			Interval:       string(raw.Interval),
			Price:          hostingAmount(raw.Price),
			PriceFormatted: string(raw.PriceFormatted),
			TrialDays:      hostingAmount(raw.TrialDays),
			Name:           string(raw.Name),
			Features:       hostingFeatures(raw.Features),
		}
		if product != "" && !strings.EqualFold(strings.TrimSpace(p.Product), product) {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToUpper(p.SKU), prefix) {
			continue
		}
		plans = append(plans, p)
	}

	sort.Slice(plans, func(i, j int) bool {
		if plans[i].SKU != plans[j].SKU {
			return plans[i].SKU < plans[j].SKU
		}
		return plans[i].Plan < plans[j].Plan
	})
	return plans
}

func hostingAmount(v *flexInt) *int64 {
	if v == nil {
		return nil
	}
	n := v.Int64()
	return &n
}

// hostingFeatures flattens the untyped feature bag to text. A nil map in
// means the field was absent or null, and stays nil so callers can report it
// as unset rather than empty.
func hostingFeatures(in map[string]json.RawMessage) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = hostingFeatureValue(v)
	}
	return out
}

func hostingFeatureValue(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return trimmed
	}
	// Numbers, booleans and nested structures keep their JSON spelling, with
	// the server's whitespace squeezed out so the value is diff-stable.
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return trimmed
	}
	return buf.String()
}
