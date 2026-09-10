package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ datasource.DataSource              = (*ipDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*ipDataSource)(nil)
)

func init() { registerDataSource(NewIPDataSource) }

// NewIPDataSource reads the public IP Porkbun sees the caller as.
func NewIPDataSource() datasource.DataSource { return &ipDataSource{} }

type ipDataSource struct {
	client *porkbun.Client
}

type ipModel struct {
	ValidateCredentials types.Bool   `tfsdk:"validate_credentials"`
	IP                  types.String `tfsdk:"ip"`
	XForwardedFor       types.String `tfsdk:"x_forwarded_for"`
	CredentialsValid    types.Bool   `tfsdk:"credentials_valid"`
}

func (d *ipDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ip"
}

func (d *ipDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reports the public IP address Porkbun sees this Terraform run coming from, and whether " +
			"the configured credentials are valid.\n\n" +
			"This is the diagnostic for the `IP_NOT_ALLOWED` error. A Porkbun API key can be scoped to a source IP " +
			"allowlist, and when the runner's egress address is not on it, every other call fails; `ip` is the " +
			"address to add at [porkbun.com/account/api](https://porkbun.com/account/api). Runner IPs are rarely " +
			"stable, so scoping a key by domain is usually the better fix.\n\n" +
			"The value is re-read on every plan and refresh, so a `porkbun_dns_record` whose `content` comes from " +
			"here follows the runner's address around — which is dynamic DNS if that is what you wanted, and a " +
			"surprise if it is not.",
		Attributes: map[string]schema.Attribute{
			"validate_credentials": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "Whether to check the configured credentials while reading the address. " +
					"Defaults to `true`, which calls `/ping`.\n\n" +
					"Set it to `false` to call `/ip` instead. `/ip` ignores credentials entirely and still reports " +
					"the address when the key is rejected — including when it is rejected by the very allowlist you " +
					"are trying to fix, which `/ping` would only answer with an error.",
			},
			"ip": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The caller's public IP address, as Porkbun's edge sees it. May be IPv6; " +
					"point `base_url` at `https://api-ipv4.porkbun.com/api/json/v3` to force an IPv4 answer.",
			},
			"x_forwarded_for": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The raw `X-Forwarded-For` header value Porkbun received, which can be a " +
					"comma-separated proxy chain rather than a single address. Null when Porkbun did not send it.",
			},
			"credentials_valid": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether Porkbun confirmed the configured API credentials. Null — not `false` " +
					"— when Porkbun said nothing: with `validate_credentials = false`, or when no credentials are " +
					"configured. Invalid credentials are an error, never a `false` here.",
			},
		},
	}
}

func (d *ipDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *ipDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ipModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	validate := config.ValidateCredentials.IsNull() || config.ValidateCredentials.IsUnknown() ||
		config.ValidateCredentials.ValueBool()

	var (
		info *porkbun.IPInfo
		err  error
	)
	if validate {
		info, err = d.client.PingInfo(ctx)
	} else {
		info, err = d.client.CallerIP(ctx)
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Unable to read the caller IP address from Porkbun", err))
		return
	}

	// Null-first: anything Porkbun did not send stays null rather than
	// landing in state as "" or false, which would read as an answer.
	state := ipModel{
		ValidateCredentials: config.ValidateCredentials,
		IP:                  types.StringNull(),
		XForwardedFor:       types.StringNull(),
		CredentialsValid:    types.BoolNull(),
	}
	if info.YourIP != "" {
		state.IP = types.StringValue(info.YourIP)
	}
	if info.XForwardedFor != "" {
		state.XForwardedFor = types.StringValue(info.XForwardedFor)
	}
	if info.CredentialsValid != nil {
		state.CredentialsValid = types.BoolValue(*info.CredentialsValid)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
