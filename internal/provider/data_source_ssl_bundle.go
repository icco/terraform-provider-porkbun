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
	_ datasource.DataSource              = (*sslBundleDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*sslBundleDataSource)(nil)
)

func init() { registerDataSource(NewSSLBundleDataSource) }

// NewSSLBundleDataSource reads a domain's free Let's Encrypt certificate.
func NewSSLBundleDataSource() datasource.DataSource { return &sslBundleDataSource{} }

type sslBundleDataSource struct {
	client *porkbun.Client
}

type sslBundleDataSourceModel struct {
	Domain           types.String `tfsdk:"domain"`
	CertificateChain types.String `tfsdk:"certificate_chain"`
	PrivateKey       types.String `tfsdk:"private_key"`
	PublicKey        types.String `tfsdk:"public_key"`
}

func (d *sslBundleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssl_bundle"
}

func (d *sslBundleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the free Let's Encrypt SSL certificate bundle Porkbun issues for a domain hosted on " +
			"its nameservers, for feeding into a load balancer, ingress or CDN certificate resource.\n\n" +
			"The certificate must already be issued. Porkbun provisions it after the domain is delegated to Porkbun's " +
			"nameservers, and this read **errors** — it does not return an empty bundle — until the certificate " +
			"reaches its `HAVECERT` state, so a freshly delegated domain may need a later apply.\n\n" +
			"~> **This data source puts a private key in Terraform state.** State is stored in plaintext regardless " +
			"of how the attribute is marked, so use a backend that encrypts at rest and restricts who can read it. " +
			"Porkbun renews the certificate on its own schedule; re-running Terraform is what picks up the new " +
			"bundle, so anything consuming it should tolerate the value changing.",
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				MarkdownDescription: "The domain to read the certificate for, e.g. `example.com`. Lowercase, with no " +
					"trailing dot.",
				Required:   true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(3), canonicalDomain{}},
			},
			// The chain and the public key are sent to every client in the
			// TLS handshake, so redacting them buys nothing and makes the
			// plan output useless for checking which cert was fetched.
			"certificate_chain": schema.StringAttribute{
				MarkdownDescription: "The PEM-encoded certificate chain: the leaf certificate followed by the " +
					"intermediates. This is what most TLS servers want as their certificate file.",
				Computed: true,
			},
			"private_key": schema.StringAttribute{
				MarkdownDescription: "The PEM-encoded private key for the certificate. Marked sensitive, which keeps " +
					"it out of CLI and plan output only — it is still written to Terraform state in plaintext.",
				Computed:  true,
				Sensitive: true,
			},
			"public_key": schema.StringAttribute{
				MarkdownDescription: "The PEM-encoded public key. Rarely needed: a TLS server is configured with " +
					"`certificate_chain` and `private_key`.",
				Computed: true,
			},
		},
	}
}

func (d *sslBundleDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *sslBundleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config sslBundleDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain := config.Domain.ValueString()
	bundle, err := d.client.RetrieveSSLBundle(ctx, domain)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to read the SSL bundle for %s", domain), err))
		return
	}

	config.CertificateChain = types.StringValue(string(bundle.CertificateChain))
	config.PrivateKey = types.StringValue(string(bundle.PrivateKey))
	config.PublicKey = types.StringValue(string(bundle.PublicKey))
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
