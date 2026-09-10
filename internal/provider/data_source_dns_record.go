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
	_ datasource.DataSource              = (*dnsRecordDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*dnsRecordDataSource)(nil)
)

func init() { registerDataSource(NewDNSRecordDataSource) }

// NewDNSRecordDataSource reads one DNS record by its Porkbun ID.
func NewDNSRecordDataSource() datasource.DataSource { return &dnsRecordDataSource{} }

type dnsRecordDataSource struct {
	client *porkbun.Client
}

type dnsRecordDataSourceModel struct {
	Domain            types.String `tfsdk:"domain"`
	Cloudflare        types.String `tfsdk:"cloudflare"`
	CloudflareEnabled types.Bool   `tfsdk:"cloudflare_enabled"`

	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Subdomain types.String `tfsdk:"subdomain"`
	Type      types.String `tfsdk:"type"`
	Content   types.String `tfsdk:"content"`
	TTL       types.Int64  `tfsdk:"ttl"`
	Prio      types.Int64  `tfsdk:"prio"`
	Notes     types.String `tfsdk:"notes"`
}

func (d *dnsRecordDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_record"
}

func (d *dnsRecordDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := dnsRecordComputedAttributes()
	attrs["id"] = schema.StringAttribute{
		MarkdownDescription: "The Porkbun record ID to read, a decimal integer carried as a string. Record IDs " +
			"are visible in the Porkbun DNS UI, in `porkbun_dns_record` resource state, and in the `records` " +
			"list of the `porkbun_dns_records` data source.",
		Required:   true,
		Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
	}
	attrs["domain"] = schema.StringAttribute{
		MarkdownDescription: "The domain the record belongs to, e.g. `example.com`. Lowercase, with no trailing " +
			"dot. Required: every Porkbun DNS call is scoped to a domain, and a record ID alone will not resolve.",
		Required:   true,
		Validators: []validator.String{stringvalidator.LengthAtLeast(3), canonicalDomain{}},
	}
	for k, v := range zoneCloudflareAttributes() {
		attrs[k] = v
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads one DNS record from a Porkbun-hosted zone by its record ID.\n\n" +
			"Use `porkbun_dns_records` to find an ID by name or type; this data source is for the case where the " +
			"ID is already known, such as a record created outside Terraform that is about to be imported.\n\n" +
			"~> A record ID that does not exist is an error, not an empty result. Porkbun answers a missing ID " +
			"with an empty list rather than a 404, and a data source that passed that through as zero values " +
			"would let a plan build on a record that is not there.",
		Attributes: attrs,
	}
}

func (d *dnsRecordDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *dnsRecordDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config dnsRecordDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain := config.Domain.ValueString()
	id := config.ID.ValueString()

	zone, err := d.client.RetrieveZoneRecord(ctx, domain, id)
	// A record deleted out of band is reported both ways depending on the
	// endpoint's mood: RECORD_NOT_FOUND, or SUCCESS with an empty list. Both
	// have to fail here — unlike the resource, a data source has no state to
	// drop, and its consumers would otherwise interpolate nulls.
	if err != nil && !porkbun.IsNotFound(err) {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to read DNS record %s on %s", id, domain), err))
		return
	}
	if err != nil || len(zone.Records) == 0 {
		resp.Diagnostics.AddError(
			fmt.Sprintf("No DNS record with ID %s on %s", id, domain),
			fmt.Sprintf("Porkbun has no record %s in the zone for %s. Record IDs are per-domain and are not "+
				"reused, so a deleted record's ID never comes back. List the zone with the "+
				"`porkbun_dns_records` data source to find the current ID.", id, domain),
		)
		return
	}

	// `id` is left exactly as configured. It is a Required attribute, and
	// Terraform rejects a data source that hands back a different value for
	// one than the configuration asked for.
	record := dnsRecordToModel(zone.Records[0], domain)
	config.Name = record.Name
	config.Subdomain = record.Subdomain
	config.Type = record.Type
	config.Content = record.Content
	config.TTL = record.TTL
	config.Prio = record.Prio
	config.Notes = record.Notes
	setZoneCloudflare(zone, &config.Cloudflare, &config.CloudflareEnabled)

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
