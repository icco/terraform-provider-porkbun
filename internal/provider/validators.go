package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

var _ validator.String = canonicalDomain{}

// canonicalDomain requires the one spelling that round trips: trimmed,
// lowercase, no trailing dot. DNS is case-insensitive, Terraform is not, so
// two spellings would be two resources managing one delegation. Applied to
// data source inputs too, for one spelling provider-wide.
//
// The nameserver set folds spellings instead; that needs a prior state value,
// which create does not have.
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
