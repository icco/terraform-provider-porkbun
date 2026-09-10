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
// DNS names are case-insensitive, but Terraform is not. `domain` forces
// replacement and core compares it literally, so respelling "example.com" as
// "Example.com" plans a destroy-and-create for a cosmetic edit, and
// `terraform import … Example.com` against a lowercase config yields an
// immediate forced replacement. Worse, nothing otherwise stops two resources
// spelled differently from managing the same registry delegation and
// fighting over it — the provider cannot detect that, because it never sees
// the two as related.
//
// The nameserver set solves the same problem by folding spellings, but that
// works only because a plan modifier can fall back to the prior state value.
// On create there is no prior value, and Terraform rejects a planned value
// for a non-computed attribute that differs from the configuration. So the
// canonical spelling has to be required rather than applied.
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

	var problem, want string
	switch {
	case strings.TrimSpace(in) != in:
		problem, want = "has leading or trailing whitespace", strings.TrimSpace(in)
	case strings.HasSuffix(in, "."):
		problem, want = "is fully qualified with a trailing dot", strings.TrimSuffix(in, ".")
	case strings.ToLower(in) != in:
		problem, want = "is not lowercase", strings.ToLower(in)
	default:
		return
	}

	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Domain is not in canonical form",
		"The domain \""+in+"\" "+problem+". Write it as \""+strings.ToLower(strings.TrimSuffix(strings.TrimSpace(in), "."))+"\".\n\n"+
			"DNS names are case-insensitive and the trailing dot is implied, but Terraform compares this attribute "+
			"literally: a respelling would plan a replacement, and two resources spelled differently would silently "+
			"manage the same delegation. Suggested value: \""+want+"\".",
	)
}
