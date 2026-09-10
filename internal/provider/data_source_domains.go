package provider

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ datasource.DataSource              = (*domainsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*domainsDataSource)(nil)
)

func init() { registerDataSource(NewDomainsDataSource) }

// NewDomainsDataSource lists domains in the account.
func NewDomainsDataSource() datasource.DataSource { return &domainsDataSource{} }

type domainsDataSource struct {
	client *porkbun.Client
}

type domainsModel struct {
	APIAccess          types.Bool   `tfsdk:"api_access"`
	AutoRenew          types.Bool   `tfsdk:"auto_renew"`
	NameContains       types.String `tfsdk:"name_contains"`
	TLDs               types.Set    `tfsdk:"tlds"`
	ExpiringWithinDays types.Int64  `tfsdk:"expiring_within_days"`

	Domains types.Set  `tfsdk:"domains"`
	Details types.List `tfsdk:"details"`
}

func (d *domainsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domains"
}

func (d *domainsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists domains in the authenticated Porkbun account, optionally filtered. Paging is handled " +
			"internally.\n\nFilter on `api_access = true` to get just the domains this key is allowed to operate on, " +
			"and check that set against the domains you manage before a wide apply.",
		Attributes: map[string]schema.Attribute{
			"api_access": schema.BoolAttribute{
				MarkdownDescription: "Filter to domains opted in to API access (`true`) or not opted in (`false`). " +
					"Omit for no filter.",
				Optional: true,
			},
			"auto_renew": schema.BoolAttribute{
				MarkdownDescription: "Filter to domains with auto-renew on or off. Omit for no filter.",
				Optional:            true,
			},
			"name_contains": schema.StringAttribute{
				MarkdownDescription: "Substring match against the full domain name, passed to Porkbun's " +
					"`nameContains` filter.",
				Optional: true,
			},
			"tlds": schema.SetAttribute{
				MarkdownDescription: "Limit results to these top-level domains, e.g. `com`. Case and a leading dot are ignored.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"expiring_within_days": schema.Int64Attribute{
				MarkdownDescription: "Limit results to domains expiring within this many days.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.AtLeast(0)},
			},
			"domains": schema.SetAttribute{
				MarkdownDescription: "The matching domain names, for set arithmetic.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"details": schema.ListNestedAttribute{
				MarkdownDescription: "Full metadata for each matching domain, sorted by name. Same shape as the " +
					"`porkbun_domain` data source.",
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: domainSchemaAttributes(true),
				},
			},
		},
	}
}

func (d *domainsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *domainsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config domainsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := porkbun.ListDomainsOptions{NameContains: config.NameContains.ValueString()}
	if !config.APIAccess.IsNull() {
		v := config.APIAccess.ValueBool()
		opts.APIAccess = &v
	}
	if !config.AutoRenew.IsNull() {
		v := config.AutoRenew.ValueBool()
		opts.AutoRenew = &v
	}
	if !config.ExpiringWithinDays.IsNull() {
		v := config.ExpiringWithinDays.ValueInt64()
		opts.ExpiringWithinDays = &v
	}
	if !config.TLDs.IsNull() {
		resp.Diagnostics.Append(config.TLDs.ElementsAs(ctx, &opts.TLDs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	domains, err := d.client.ListDomains(ctx, opts)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Unable to list Porkbun domains", err))
		return
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i].Domain < domains[j].Domain })

	names := make([]string, 0, len(domains))
	details := make([]domainModel, 0, len(domains))
	for _, dom := range domains {
		names = append(names, dom.Domain)
		details = append(details, domainToModel(dom))
	}

	nameSet, diags := types.SetValueFrom(ctx, types.StringType, names)
	resp.Diagnostics.Append(diags...)

	detailList, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: domainAttributeTypes()}, details)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	config.Domains = nameSet
	config.Details = detailList
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
