package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ datasource.DataSource              = (*domainDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*domainDataSource)(nil)
)

// NewDomainDataSource reads metadata for one domain in the account.
func NewDomainDataSource() datasource.DataSource { return &domainDataSource{} }

type domainDataSource struct {
	client *porkbun.Client
}

type domainModel struct {
	Domain       types.String `tfsdk:"domain"`
	Status       types.String `tfsdk:"status"`
	TLD          types.String `tfsdk:"tld"`
	CreateDate   types.String `tfsdk:"create_date"`
	ExpireDate   types.String `tfsdk:"expire_date"`
	AutoRenew    types.Bool   `tfsdk:"auto_renew"`
	SecurityLock types.Bool   `tfsdk:"security_lock"`
	WhoisPrivacy types.Bool   `tfsdk:"whois_privacy"`
	APIAccess    types.Bool   `tfsdk:"api_access"`
	NotLocal     types.Bool   `tfsdk:"not_local"`
}

// domainAttributeTypes mirrors domainModel for the object list in
// porkbun_domains.details.
func domainAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"domain":        types.StringType,
		"status":        types.StringType,
		"tld":           types.StringType,
		"create_date":   types.StringType,
		"expire_date":   types.StringType,
		"auto_renew":    types.BoolType,
		"security_lock": types.BoolType,
		"whois_privacy": types.BoolType,
		"api_access":    types.BoolType,
		"not_local":     types.BoolType,
	}
}

func domainToModel(d porkbun.Domain) domainModel {
	return domainModel{
		Domain:       types.StringValue(d.Domain),
		Status:       types.StringValue(d.Status),
		TLD:          types.StringValue(d.TLD),
		CreateDate:   types.StringValue(d.CreateDate),
		ExpireDate:   types.StringValue(d.ExpireDate),
		AutoRenew:    types.BoolValue(d.AutoRenew.Bool()),
		SecurityLock: types.BoolValue(d.SecurityLock.Bool()),
		WhoisPrivacy: types.BoolValue(d.WhoisPrivacy.Bool()),
		APIAccess:    types.BoolValue(d.APIAccess.Bool()),
		NotLocal:     types.BoolValue(d.NotLocal.Bool()),
	}
}

// domainSchemaAttributes is shared by porkbun_domain and the nested objects
// of porkbun_domains.details.
func domainSchemaAttributes(computedDomain bool) map[string]schema.Attribute {
	domainAttr := schema.StringAttribute{
		MarkdownDescription: "The fully qualified domain name.",
	}
	if computedDomain {
		domainAttr.Computed = true
	} else {
		domainAttr.Required = true
		domainAttr.Validators = []validator.String{stringvalidator.LengthAtLeast(3)}
	}

	return map[string]schema.Attribute{
		"domain":      domainAttr,
		"status":      schema.StringAttribute{Computed: true, MarkdownDescription: "Registration status, e.g. `ACTIVE`."},
		"tld":         schema.StringAttribute{Computed: true, MarkdownDescription: "The top-level domain, without a leading dot."},
		"create_date": schema.StringAttribute{Computed: true, MarkdownDescription: "When the domain was registered, as Porkbun reports it."},
		"expire_date": schema.StringAttribute{Computed: true, MarkdownDescription: "When the registration expires, as Porkbun reports it."},
		"auto_renew":  schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether auto-renew is enabled."},
		"security_lock": schema.BoolAttribute{
			Computed:            true,
			MarkdownDescription: "Whether the registrar transfer lock is enabled.",
		},
		"whois_privacy": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether WHOIS privacy is enabled."},
		"api_access": schema.BoolAttribute{
			Computed: true,
			MarkdownDescription: "Whether this domain is opted in to API access. **A key cannot operate on a domain " +
				"where this is false**, however well scoped it is. Toggle it per domain at porkbun.com/account, or " +
				"globally with the \"Opt In All Domains\" API setting.",
		},
		"not_local": schema.BoolAttribute{
			Computed: true,
			MarkdownDescription: "Whether the domain is delegated away from Porkbun's nameservers. When true, " +
				"`porkbun_dns_record` still applies successfully against Porkbun's copy of the zone, but no resolver " +
				"ever queries it — the records have no effect. Manage DNS wherever the domain is actually delegated.",
		},
	}
}

func (d *domainDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (d *domainDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the registrar metadata Porkbun holds for one domain in the authenticated account.",
		Attributes:          domainSchemaAttributes(false),
	}
}

func (d *domainDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *domainDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config domainModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := config.Domain.ValueString()
	dom, err := d.client.GetDomain(ctx, name)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to read domain %s", name), err))
		return
	}

	state := domainToModel(*dom)
	if state.Domain.ValueString() == "" {
		state.Domain = types.StringValue(name)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
