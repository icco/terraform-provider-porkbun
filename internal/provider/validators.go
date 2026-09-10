package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

var _ validator.String = canonicalDomain{}

// canonicalDomain requires a domain to be written the one way that round
// trips: trimmed, lowercase, and without the root label's trailing dot.
//
// DNS names are case-insensitive, Terraform is not. On the resources
// `domain` forces replacement and core compares it literally, so respelling
// "example.com" as "Example.com" plans a destroy-and-create for a cosmetic
// edit. Worse, nothing otherwise stops two differently spelled resources
// managing the same registry delegation and fighting over it; the provider
// never sees the two as related.
//
// It is on the data source inputs too, where nothing is replaced, so that
// one domain has one spelling everywhere in the provider. Do not remove it
// there on the grounds that a data source has no state to churn.
//
// The nameserver set folds spellings instead, but only because a plan
// modifier can fall back to the prior state value. On create there is no
// prior value, and core rejects a planned value for a non-computed attribute
// that differs from the configuration — so here the spelling must be
// required rather than applied.
type canonicalDomain struct{}

func (canonicalDomain) Description(_ context.Context) string {
	return "must be a lowercase domain name with no surrounding whitespace and no trailing dot"
}

func (v canonicalDomain) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (canonicalDomain) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	in := req.ConfigValue.ValueString()

	var problem string
	switch {
	case strings.TrimSpace(in) != in:
		problem = "has leading or trailing whitespace"
	case strings.HasSuffix(in, "."):
		problem = "is fully qualified with a trailing dot"
	case strings.ToLower(in) != in:
		problem = "is not lowercase"
	default:
		return
	}

	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Domain is not in canonical form",
		"The domain \""+in+"\" "+problem+". Write it as \""+strings.ToLower(strings.TrimSuffix(strings.TrimSpace(in), "."))+"\".\n\n"+
			"DNS names are case-insensitive and the trailing dot is implied, but Terraform compares this attribute "+
			"literally: a respelling would plan a replacement, and two resources spelled differently would silently "+
			"manage the same delegation.",
	)
}
