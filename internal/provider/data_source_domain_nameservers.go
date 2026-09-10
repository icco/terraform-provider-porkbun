package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ datasource.DataSource              = (*domainNameserversDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*domainNameserversDataSource)(nil)
)

// NewDomainNameserversDataSource reads a domain's registry delegation.
func NewDomainNameserversDataSource() datasource.DataSource { return &domainNameserversDataSource{} }

type domainNameserversDataSource struct {
	client *porkbun.Client
}

type domainNameserversDataSourceModel struct {
	Domain      types.String `tfsdk:"domain"`
	Nameservers types.Set    `tfsdk:"nameservers"`
}

func (d *domainNameserversDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_nameservers"
}

func (d *domainNameserversDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the nameservers a domain is currently delegated to at the registry, without managing " +
			"them. Useful for auditing a delegation before adopting it into Terraform, or for checking that a switch " +
			"landed.",
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				MarkdownDescription: "The domain to read, e.g. `example.com`.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(3), canonicalDomain{}},
			},
			"nameservers": schema.SetAttribute{
				MarkdownDescription: "The nameserver hostnames the registry lists, lowercased, sorted and without " +
					"trailing dots.",
				Computed:    true,
				ElementType: types.StringType,
			},
		},
	}
}

func (d *domainNameserversDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *domainNameserversDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config domainNameserversDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain := config.Domain.ValueString()
	ns, err := d.client.GetNameservers(ctx, domain)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to read nameservers for %s", domain), err))
		return
	}

	set, diags := types.SetValueFrom(ctx, types.StringType, ns)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	config.Nameservers = set
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
