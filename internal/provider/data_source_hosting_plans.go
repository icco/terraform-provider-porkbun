package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ datasource.DataSource              = (*hostingPlansDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*hostingPlansDataSource)(nil)
)

func init() { registerDataSource(NewHostingPlansDataSource) }

// NewHostingPlansDataSource lists provisionable hosting plans.
func NewHostingPlansDataSource() datasource.DataSource { return &hostingPlansDataSource{} }

type hostingPlansDataSource struct {
	client *porkbun.Client
}

type hostingPlansModel struct {
	Product   types.String `tfsdk:"product"`
	SKUPrefix types.String `tfsdk:"sku_prefix"`

	Plans types.List `tfsdk:"plans"`
	SKUs  types.Set  `tfsdk:"skus"`
}

type hostingPlanModel struct {
	Product        types.String `tfsdk:"product"`
	Plan           types.String `tfsdk:"plan"`
	SKU            types.String `tfsdk:"sku"`
	Interval       types.String `tfsdk:"interval"`
	Price          types.Int64  `tfsdk:"price"`
	PriceFormatted types.String `tfsdk:"price_formatted"`
	TrialDays      types.Int64  `tfsdk:"trial_days"`
	Name           types.String `tfsdk:"name"`
	Features       types.Map    `tfsdk:"features"`
}

func hostingPlanAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"product":         types.StringType,
		"plan":            types.StringType,
		"sku":             types.StringType,
		"interval":        types.StringType,
		"price":           types.Int64Type,
		"price_formatted": types.StringType,
		"trial_days":      types.Int64Type,
		"name":            types.StringType,
		"features":        types.MapType{ElemType: types.StringType},
	}
}

func (d *hostingPlansDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosting_plans"
}

func (d *hostingPlansDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the hosting plans Porkbun will provision through the API (`/hosting/plans`), " +
			"across both products: Secure Static Hosting (`PIXIESECURESTATIC…` SKUs) and Cloud for WordPress " +
			"(`CLOUDWORDPRESS…` SKUs). The `product` field on each row is what tells the two apart.\n\n" +
			"A plan is provisioned by its `sku`: since API v3.13 `/hosting/create` takes one `sku` rather than a " +
			"product plus a plan. The same row's `price` is the value that call wants echoed back as " +
			"`acknowledgedCost`, so read the pair from here rather than hard-coding cents.\n\n" +
			"Porkbun accepts no request parameters on this endpoint, so `product` and `sku_prefix` filter the " +
			"response inside the provider. That is why a value matching nothing yields an empty `plans` list and a " +
			"warning instead of an API error.\n\n" +
			"There is no hosting resource in this provider yet; this data source is for discovering what exists and " +
			"what it costs.",
		Attributes: map[string]schema.Attribute{
			"product": schema.StringAttribute{
				MarkdownDescription: "Keep only plans for this product, e.g. `secureStaticHosting`. Compared " +
					"case-insensitively. Omit for both products.",
				Optional:   true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"sku_prefix": schema.StringAttribute{
				MarkdownDescription: "Keep only plans whose SKU starts with this prefix, e.g. " +
					"`PIXIESECURESTATIC` or `CLOUDWORDPRESS`. Compared case-insensitively. Omit for every SKU.",
				Optional:   true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"skus": schema.SetAttribute{
				MarkdownDescription: "The matching SKUs on their own, for set arithmetic — checking that the SKU " +
					"a configuration provisions is still offered, for instance.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"plans": schema.ListNestedAttribute{
				MarkdownDescription: "The matching plans, sorted by SKU.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"product": schema.StringAttribute{
							Computed: true,
							MarkdownDescription: "The hosting product this plan provisions, e.g. " +
								"`secureStaticHosting`. Cloud for WordPress plans are managed WordPress: the " +
								"file endpoints do not apply to them.",
						},
						"plan": schema.StringAttribute{
							Computed: true,
							MarkdownDescription: "Porkbun's billing label for the term, e.g. `monthly`. Kept for " +
								"display: `/hosting/create` is driven by `sku`, not by this.",
						},
						"sku": schema.StringAttribute{
							Computed: true,
							MarkdownDescription: "The plan identifier `/hosting/create` provisions from, e.g. " +
								"`PIXIESECURESTATICM2`.",
						},
						"interval": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "The billing interval `price` covers.",
						},
						"price": schema.Int64Attribute{
							Computed: true,
							MarkdownDescription: "Plan price **in cents** — `300` is $3.00. This is the number " +
								"`/hosting/create` expects as `acknowledgedCost`; a mismatch there returns " +
								"`COST_ACKNOWLEDGMENT_REQUIRED`. Null when Porkbun sent no price, which is not " +
								"the same as `0`.",
						},
						"price_formatted": schema.StringAttribute{
							Computed: true,
							MarkdownDescription: "Porkbun's display rendering of the price. For display only — " +
								"compute against `price`.",
						},
						"trial_days": schema.Int64Attribute{
							Computed: true,
							MarkdownDescription: "Length of the free trial in days. `0` means the plan has no " +
								"trial; null means Porkbun sent no value. A domain gets one trial ever, so a " +
								"re-provision after deprovisioning is charged immediately whatever this says.",
						},
						"name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Human-readable plan name.",
						},
						"features": schema.MapAttribute{
							Computed: true,
							MarkdownDescription: "The plan's feature bag, keyed as Porkbun keys it. The API " +
								"declares no schema for it, so every value is exposed as a string: a JSON string " +
								"appears unquoted, and a number, boolean, array or nested object appears as its " +
								"compact JSON text (`\"true\"`, `\"10\"`, `{\"a\":1}`). Null when the API sent no " +
								"feature bag at all, empty when it sent an empty one.",
							ElementType: types.StringType,
						},
					},
				},
			},
		},
	}
}

func (d *hostingPlansDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *hostingPlansDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config hostingPlansModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plans, err := d.client.ListHostingPlans(ctx, porkbun.ListHostingPlansOptions{
		Product:   config.Product.ValueString(),
		SKUPrefix: config.SKUPrefix.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Unable to list Porkbun hosting plans", err))
		return
	}

	skus := make([]string, 0, len(plans))
	rows := make([]hostingPlanModel, 0, len(plans))
	for _, p := range plans {
		skus = append(skus, p.SKU)

		row := hostingPlanModel{
			Product:        types.StringValue(p.Product),
			Plan:           types.StringValue(p.Plan),
			SKU:            types.StringValue(p.SKU),
			Interval:       types.StringValue(p.Interval),
			Price:          types.Int64Null(),
			PriceFormatted: types.StringValue(p.PriceFormatted),
			TrialDays:      types.Int64Null(),
			Name:           types.StringValue(p.Name),
			Features:       types.MapNull(types.StringType),
		}
		// A price or trial the API never sent must not reach state as 0:
		// `price == 0` reads as free, and 0 is also a legitimate answer.
		if p.Price != nil {
			row.Price = types.Int64Value(*p.Price)
		}
		if p.TrialDays != nil {
			row.TrialDays = types.Int64Value(*p.TrialDays)
		}
		if p.Features != nil {
			features, diags := types.MapValueFrom(ctx, types.StringType, p.Features)
			resp.Diagnostics.Append(diags...)
			row.Features = features
		}
		rows = append(rows, row)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// A filter that matches nothing is worth saying out loud but is not an
	// error: the call succeeded, and Porkbun does add and retire SKUs. A
	// for_each over a silently empty list is the failure this catches.
	if len(plans) == 0 && (!config.Product.IsNull() || !config.SKUPrefix.IsNull()) {
		resp.Diagnostics.AddWarning(
			"No hosting plans matched the filters",
			"Porkbun returned no plan matching product="+quotedOrAny(config.Product)+" and sku_prefix="+
				quotedOrAny(config.SKUPrefix)+". The filters are applied by the provider, so an unknown value "+
				"produces an empty list rather than an API error. Drop the filters to see every plan Porkbun "+
				"currently provisions.",
		)
	}

	skuSet, diags := types.SetValueFrom(ctx, types.StringType, skus)
	resp.Diagnostics.Append(diags...)

	planList, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: hostingPlanAttributeTypes()}, rows)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	config.SKUs = skuSet
	config.Plans = planList
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

func quotedOrAny(v types.String) string {
	if v.IsNull() {
		return "(any)"
	}
	return `"` + v.ValueString() + `"`
}
