package provider

import (
	"context"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ datasource.DataSource              = (*pricingDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*pricingDataSource)(nil)
)

func init() { registerDataSource(NewPricingDataSource) }

// NewPricingDataSource reads Porkbun's public per-TLD price sheet.
func NewPricingDataSource() datasource.DataSource { return &pricingDataSource{} }

type pricingDataSource struct {
	client *porkbun.Client
}

type pricingModel struct {
	TLDs    types.Set `tfsdk:"tlds"`
	Pricing types.Map `tfsdk:"pricing"`
}

type tldPricingModel struct {
	Registration types.String `tfsdk:"registration"`
	Renewal      types.String `tfsdk:"renewal"`
	Transfer     types.String `tfsdk:"transfer"`
	SpecialType  types.String `tfsdk:"special_type"`
	Coupons      types.Map    `tfsdk:"coupons"`
}

type couponModel struct {
	Code          types.String `tfsdk:"code"`
	Type          types.String `tfsdk:"type"`
	Amount        types.String `tfsdk:"amount"`
	MaxPerUser    types.Int64  `tfsdk:"max_per_user"`
	FirstYearOnly types.Bool   `tfsdk:"first_year_only"`
}

func couponAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"code":            types.StringType,
		"type":            types.StringType,
		"amount":          types.StringType,
		"max_per_user":    types.Int64Type,
		"first_year_only": types.BoolType,
	}
}

func tldPricingAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"registration": types.StringType,
		"renewal":      types.StringType,
		"transfer":     types.StringType,
		"special_type": types.StringType,
		"coupons":      types.MapType{ElemType: types.ObjectType{AttrTypes: couponAttributeTypes()}},
	}
}

func (d *pricingDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pricing"
}

// priceDescription is repeated on all three amounts: the thousands separator
// is the thing that breaks configurations, and it has to be on the attribute
// to reach the generated docs page.
const priceDescription = " Porkbun formats money for display, so an expensive TLD arrives with a " +
	"thousands separator (`.security` is `\"2,060.25\"`). It is kept verbatim as a string; run it through " +
	"`tonumber(replace(price, \",\", \"\"))` before comparing it to a number."

func (d *pricingDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads Porkbun's default domain pricing, in US dollars, for every TLD it sells " +
			"(`/pricing/get`).\n\nPorkbun serves this endpoint without authentication — the prices are the same " +
			"list-price sheet anyone sees — but the provider still needs `api_key` and `secret_key` configured, so " +
			"nothing else changes about using it.\n\nPrices are quotes, not commitments: an account-specific " +
			"discount, a coupon, or a premium name can all make the real charge differ.",
		Attributes: map[string]schema.Attribute{
			"tlds": schema.SetAttribute{
				MarkdownDescription: "Limit `pricing` to these top-level domains, e.g. `com`. Case and a leading " +
					"dot are ignored. Omit for the whole catalog, which is around 900 entries.\n\nA TLD Porkbun " +
					"does not sell is simply absent from `pricing`, and the data source warns about it. The " +
					"filtering happens in the provider on purpose: the API answers an unknown TLD with an invented " +
					"entry priced `0.00` rather than an error, and `0.00` is a genuine price for `.fly` and for " +
					"`.uk` transfers, so a typo would otherwise be indistinguishable from a real quote.",
				Optional:    true,
				ElementType: types.StringType,
				Validators: []validator.Set{
					setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
			"pricing": schema.MapNestedAttribute{
				MarkdownDescription: "Prices keyed by TLD, without a leading dot. Multi-label suffixes are keyed " +
					"whole, e.g. `co.uk`.",
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"registration": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "First-term registration price in USD." + priceDescription,
						},
						"renewal": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Renewal price in USD." + priceDescription,
						},
						"transfer": schema.StringAttribute{
							Computed: true,
							MarkdownDescription: "Inbound transfer price in USD, `0.00` where Porkbun transfers " +
								"the TLD for free." + priceDescription,
						},
						"special_type": schema.StringAttribute{
							Computed: true,
							MarkdownDescription: "Set only for TLDs that are not conventional registrations — " +
								"`handshake` for the Handshake namespace, which is not resolvable by ordinary DNS. " +
								"Empty for everything else.",
						},
						"coupons": schema.MapNestedAttribute{
							MarkdownDescription: "Active coupons, keyed by the product they apply to, e.g. " +
								"`registration`. Empty for most TLDs.",
							Computed: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"code": schema.StringAttribute{
										Computed:            true,
										MarkdownDescription: "The coupon code, applied at checkout.",
									},
									"type": schema.StringAttribute{
										Computed:            true,
										MarkdownDescription: "How the discount is calculated, as Porkbun labels it.",
									},
									"amount": schema.StringAttribute{
										Computed: true,
										MarkdownDescription: "The discount, kept as a string for the same reason " +
											"the prices are.",
									},
									"max_per_user": schema.Int64Attribute{
										Computed:            true,
										MarkdownDescription: "How many times one account may use the coupon.",
									},
									"first_year_only": schema.BoolAttribute{
										Computed: true,
										MarkdownDescription: "Whether the discount applies to the first term only. " +
											"Porkbun sends this as `\"yes\"`/`\"no\"`; it is exposed as a boolean.",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *pricingDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *pricingDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config pricingModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var want []string
	if !config.TLDs.IsNull() {
		resp.Diagnostics.Append(config.TLDs.ElementsAs(ctx, &want, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	pricing, err := d.client.GetPricing(ctx, want)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Unable to read Porkbun pricing", err))
		return
	}

	// Warn rather than error: a TLD missing from the catalog is worth
	// surfacing, but the request itself succeeded, and a `for_each` over a
	// silently short map is exactly the failure this catches.
	if missing := missingTLDs(want, pricing); len(missing) > 0 {
		resp.Diagnostics.AddAttributeWarning(
			path.Root("tlds"),
			"Porkbun sells no such TLD",
			"Porkbun's price sheet has no entry for "+strings.Join(missing, ", ")+", so `pricing` omits "+
				"those keys. Check the spelling: the catalog keys multi-label suffixes whole, e.g. `co.uk` "+
				"rather than `uk`.",
		)
	}

	values := make(map[string]tldPricingModel, len(pricing))
	for tld, p := range pricing {
		coupons := make(map[string]couponModel, len(p.Coupons))
		for product, c := range p.Coupons {
			coupons[product] = couponModel{
				Code:       types.StringValue(string(c.Code)),
				Type:       types.StringValue(string(c.Type)),
				Amount:     types.StringValue(string(c.Amount)),
				MaxPerUser: types.Int64Value(c.MaxPerUser.Int64()),
				// "yes"/"no", so anything that is not "yes" is false.
				FirstYearOnly: types.BoolValue(strings.EqualFold(string(c.FirstYearOnly), "yes")),
			}
		}
		couponMap, diags := types.MapValueFrom(ctx, types.ObjectType{AttrTypes: couponAttributeTypes()}, coupons)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		values[tld] = tldPricingModel{
			Registration: types.StringValue(string(p.Registration)),
			Renewal:      types.StringValue(string(p.Renewal)),
			Transfer:     types.StringValue(string(p.Transfer)),
			SpecialType:  types.StringValue(string(p.SpecialType)),
			Coupons:      couponMap,
		}
	}

	pricingMap, diags := types.MapValueFrom(ctx, types.ObjectType{AttrTypes: tldPricingAttributeTypes()}, values)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	config.Pricing = pricingMap
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// missingTLDs reports which requested TLDs the catalog had no entry for,
// deduplicated and sorted so the warning text is stable across runs.
func missingTLDs(want []string, got map[string]porkbun.TLDPricing) []string {
	seen := map[string]struct{}{}
	var missing []string
	for _, t := range want {
		n := porkbun.NormalizeTLD(t)
		if _, ok := got[n]; ok {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		missing = append(missing, n)
	}
	sort.Strings(missing)
	return missing
}
