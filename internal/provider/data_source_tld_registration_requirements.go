package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ datasource.DataSource              = (*tldRegistrationRequirementsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*tldRegistrationRequirementsDataSource)(nil)
	_ validator.String                   = tldNoLeadingDot{}
)

func init() { registerDataSource(NewTLDRegistrationRequirementsDataSource) }

// NewTLDRegistrationRequirementsDataSource reads what a TLD demands of a
// registrant.
func NewTLDRegistrationRequirementsDataSource() datasource.DataSource {
	return &tldRegistrationRequirementsDataSource{}
}

type tldRegistrationRequirementsDataSource struct {
	client *porkbun.Client
}

type tldRegistrationRequirementsModel struct {
	TLD                       types.String `tfsdk:"tld"`
	APIRegisterable           types.Bool   `tfsdk:"api_registerable"`
	NotAPIRegisterableReason  types.String `tfsdk:"not_api_registerable_reason"`
	RegistrationDurationYears types.Int64  `tfsdk:"registration_duration_years"`
	MaxRegistrationYears      types.Int64  `tfsdk:"max_registration_years"`
	WhoisPrivacySupported     types.Bool   `tfsdk:"whois_privacy_supported"`
	RequiresValidatedAddress  types.Bool   `tfsdk:"requires_validated_address"`
	RegistrantOnly            types.Bool   `tfsdk:"registrant_only"`
	RequestSchema             types.String `tfsdk:"request_schema"`
	RegistryRequirements      types.String `tfsdk:"registry_requirements"`
}

// tldNoLeadingDot rejects a leading dot. Porkbun's path segment is the TLD alone, so
// `.us` and `us` are one lookup written two ways. Interior dots are left
// alone: `co.uk` is a TLD Porkbun sells.
type tldNoLeadingDot struct{}

func (tldNoLeadingDot) Description(_ context.Context) string {
	return "must be a TLD with no leading dot, e.g. \"us\""
}

func (v tldNoLeadingDot) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (tldNoLeadingDot) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	in := req.ConfigValue.ValueString()
	if !strings.HasPrefix(in, ".") {
		return
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"TLD must not start with a dot",
		fmt.Sprintf("Write %q as %q. Porkbun's API takes the bare TLD as a path segment.",
			in, strings.TrimPrefix(in, ".")),
	)
}

func (d *tldRegistrationRequirementsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tld_registration_requirements"
}

func (d *tldRegistrationRequirementsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads Porkbun's machine-readable registration requirements for one TLD: whether it can " +
			"be registered through the API at all, the term and contact rules it imposes, and the JSON Schema of the " +
			"registrant data it demands.\n\n" +
			"This is account-independent — it describes a TLD, not a domain you own — so it can be read before " +
			"anything is registered.\n\n" +
			"`request_schema` and `registry_requirements` are whole JSON Schema documents, which have no fixed shape, " +
			"so they are returned as JSON **strings**. Read them with `jsondecode()`.",
		Attributes: map[string]schema.Attribute{
			"tld": schema.StringAttribute{
				MarkdownDescription: "The top-level domain to describe, without a leading dot, e.g. `com`, `us`, " +
					"`co.uk`. Lowercase, with no surrounding whitespace.",
				Required:   true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(2), canonicalDomain{}, tldNoLeadingDot{}},
			},
			"api_registerable": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether this TLD can be registered through the API. False for TLDs whose " +
					"registry eligibility data the API cannot submit; those must be registered on porkbun.com.",
			},
			"not_api_registerable_reason": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Why the TLD is not API-registerable. Null when `api_registerable` is true, " +
					"and when Porkbun gave no reason.",
			},
			"registration_duration_years": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "The fixed term, in years, that the API registers this TLD for.",
			},
			"max_registration_years": schema.Int64Attribute{
				Computed: true,
				MarkdownDescription: "The longest term the registry allows, in years. **Null means Porkbun stated " +
					"no maximum**, which is not the same as a stated `0`.",
			},
			"whois_privacy_supported": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether WHOIS privacy can be enabled for this TLD.",
			},
			"requires_validated_address": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether the registry requires a validated registrant address. Registrations " +
					"for these TLDs can come back asking for an address correction to be confirmed.",
			},
			"registrant_only": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether the TLD uses only the registrant contact, with no separate admin, " +
					"tech or billing contacts.",
			},
			"request_schema": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The `/domain/create` request body this TLD accepts, as a JSON Schema " +
					"(Draft 2020-12) document in a string. Pass it to `jsondecode()`. Null if Porkbun sent no schema.",
			},
			"registry_requirements": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "A second JSON Schema document, in a string, enumerating the extra registry " +
					"eligibility fields the TLD requires — `.us` purpose and category, `.ca` legal type — with their " +
					"allowed values, human labels and an `x-policyNote`. **Null when the TLD has no structured extra " +
					"requirements**, so `registry_requirements != null` is the test for \"this TLD asks for more\". " +
					"These fields document eligibility; `/domain/create` does not accept them today.",
			},
		},
	}
}

func (d *tldRegistrationRequirementsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *tldRegistrationRequirementsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state tldRegistrationRequirementsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tld := state.TLD.ValueString()
	out, err := d.client.GetTLDRegistrationRequirements(ctx, tld)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to read registration requirements for .%s", tld), err))
		return
	}

	// state.TLD deliberately keeps the configured spelling rather than the
	// response's echo: Terraform rejects a data source that hands back a
	// different value for a required config attribute, and the echo is not
	// reliably the TLD that was asked for.
	state.APIRegisterable = types.BoolValue(out.APIRegisterable.Bool())
	state.NotAPIRegisterableReason = tldNullIfEmpty(string(out.NotAPIRegisterableReason))
	state.RegistrationDurationYears = types.Int64Value(out.RegistrationDurationYears.Int64())
	state.WhoisPrivacySupported = types.BoolValue(out.WhoisPrivacySupported.Bool())
	state.RequiresValidatedAddress = types.BoolValue(out.RequiresValidatedAddress.Bool())
	state.RegistrantOnly = types.BoolValue(out.RegistrantOnly.Bool())
	state.RequestSchema = tldNullIfEmpty(out.RequestSchemaJSON())
	state.RegistryRequirements = tldNullIfEmpty(out.RegistryRequirementsJSON())

	// An unstated maximum is null, never 0: 0 would read as "no years
	// permitted", the opposite of what an absent limit means.
	state.MaxRegistrationYears = types.Int64Null()
	if out.MaxRegistrationYears != nil {
		state.MaxRegistrationYears = types.Int64Value(out.MaxRegistrationYears.Int64())
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// tldNullIfEmpty keeps a field Porkbun did not send out of state as "", which
// is a value it never sent.
func tldNullIfEmpty(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
