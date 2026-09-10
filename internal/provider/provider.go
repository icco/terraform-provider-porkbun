package provider

import (
	"context"
	"os"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var _ provider.Provider = (*porkbunProvider)(nil)

type porkbunProvider struct {
	version string
}

// New returns the provider constructor goreleaser-stamped with a version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &porkbunProvider{version: version}
	}
}

type providerModel struct {
	APIKey     types.String `tfsdk:"api_key"`
	SecretKey  types.String `tfsdk:"secret_key"`
	BaseURL    types.String `tfsdk:"base_url"`
	MaxRetries types.Int64  `tfsdk:"max_retries"`
}

func (p *porkbunProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "porkbun"
	resp.Version = p.version
}

func (p *porkbunProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage [Porkbun](https://porkbun.com) domains: registry nameserver delegation and DNS records.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				MarkdownDescription: "Porkbun API key (`pk1_...`). May also be set with the `PORKBUN_API_KEY` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"secret_key": schema.StringAttribute{
				MarkdownDescription: "Porkbun secret API key (`sk1_...`). May also be set with the `PORKBUN_SECRET_KEY` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"base_url": schema.StringAttribute{
				MarkdownDescription: "Override the Porkbun API root. Defaults to `" + porkbun.DefaultBaseURL + "`. " +
					"Use `https://api-ipv4.porkbun.com/api/json/v3`, which resolves A records only, from networks without working IPv6. " +
					"May also be set with the `PORKBUN_BASE_URL` environment variable.",
				Optional: true,
			},
			"max_retries": schema.Int64Attribute{
				MarkdownDescription: "How many times to retry a failed API call. Defaults to `3`; `0` disables retries. " +
					"May also be set with the `PORKBUN_MAX_RETRIES` environment variable.",
				Optional:   true,
				Validators: []validator.Int64{int64validator.AtLeast(0)},
			},
		},
	}
}

func (p *porkbunProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// An unknown value at plan time (a credential wired from another resource)
	// must be an error, not a warning: warning and returning leaves every
	// resource holding a nil client, which panics on first use.
	for name, attr := range map[string]types.String{
		"api_key":    config.APIKey,
		"secret_key": config.SecretKey,
		"base_url":   config.BaseURL,
	} {
		if attr.IsUnknown() {
			resp.Diagnostics.AddAttributeError(
				path.Root(name),
				"Unknown Porkbun provider configuration",
				"The provider cannot be configured because "+name+" is not known until apply. "+
					"Either set it to a static value, or set the matching environment variable.",
			)
		}
	}
	if config.MaxRetries.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("max_retries"),
			"Unknown Porkbun provider configuration",
			"The provider cannot be configured because max_retries is not known until apply.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	apiKey := firstNonEmpty(config.APIKey.ValueString(), os.Getenv("PORKBUN_API_KEY"))
	secretKey := firstNonEmpty(config.SecretKey.ValueString(), os.Getenv("PORKBUN_SECRET_KEY"))
	baseURL := firstNonEmpty(config.BaseURL.ValueString(), os.Getenv("PORKBUN_BASE_URL"), porkbun.DefaultBaseURL)

	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing Porkbun API key",
			"Set the provider's api_key attribute or the PORKBUN_API_KEY environment variable.",
		)
	}
	if secretKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("secret_key"),
			"Missing Porkbun secret key",
			"Set the provider's secret_key attribute or the PORKBUN_SECRET_KEY environment variable.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	maxRetries := 3
	switch {
	case !config.MaxRetries.IsNull():
		maxRetries = int(config.MaxRetries.ValueInt64())
	case os.Getenv("PORKBUN_MAX_RETRIES") != "":
		parsed, err := strconv.Atoi(os.Getenv("PORKBUN_MAX_RETRIES"))
		if err != nil {
			resp.Diagnostics.AddError(
				"Invalid PORKBUN_MAX_RETRIES",
				"PORKBUN_MAX_RETRIES must be an integer, got "+strconv.Quote(os.Getenv("PORKBUN_MAX_RETRIES"))+".",
			)
			return
		}
		maxRetries = parsed
	}

	client, err := porkbun.New(porkbun.Config{
		APIKey:     apiKey,
		SecretKey:  secretKey,
		BaseURL:    baseURL,
		MaxRetries: maxRetries,
		UserAgent:  "terraform-provider-porkbun/" + p.version,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Porkbun API client", err.Error())
		return
	}

	tflog.Debug(ctx, "configured porkbun client", map[string]any{"base_url": client.BaseURL(), "max_retries": maxRetries})

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *porkbunProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewDomainNameserversResource,
		NewDNSRecordResource,
	}
}

func (p *porkbunProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDomainNameserversDataSource,
		NewDomainDataSource,
		NewDomainsDataSource,
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
