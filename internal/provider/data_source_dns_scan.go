package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ datasource.DataSource              = (*dnsScanDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*dnsScanDataSource)(nil)
)

func init() { registerDataSource(NewDNSScanDataSource) }

// NewDNSScanDataSource discovers what a domain publishes in live DNS.
func NewDNSScanDataSource() datasource.DataSource { return &dnsScanDataSource{} }

type dnsScanDataSource struct {
	client *porkbun.Client
}

type dnsScanModel struct {
	Domain      types.String `tfsdk:"domain"`
	RecordCount types.Int64  `tfsdk:"record_count"`
	Records     types.List   `tfsdk:"records"`
}

type dnsScanRecordModel struct {
	Name    types.String `tfsdk:"name"`
	Type    types.String `tfsdk:"type"`
	Content types.String `tfsdk:"content"`
	TTL     types.Int64  `tfsdk:"ttl"`
	Prio    types.Int64  `tfsdk:"prio"`
}

func dnsScanRecordAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"name":    types.StringType,
		"type":    types.StringType,
		"content": types.StringType,
		"ttl":     types.Int64Type,
		"prio":    types.Int64Type,
	}
}

func (d *dnsScanDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_scan"
}

func (d *dnsScanDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Discovers the records a domain currently **publishes in live DNS**, by querying its " +
			"authoritative nameservers (`GET /dns/scan/{domain}`). Nothing is written.\n\n" +

			"**This is not the Porkbun zone.** The `porkbun_dns_record` resource and `/dns/retrieve` read the zone " +
			"Porkbun *stores*; a scan reads what the delegation *answers with*. The two are the same only while the " +
			"domain is delegated to Porkbun's nameservers — check `porkbun_domain.not_local`. That gap is the point " +
			"of this data source: it is how you detect that the records in your configuration are not the records " +
			"the internet is getting, and it is the only way to read the zone of a domain that is mid-transfer, " +
			"because a registrar transfer moves the delegation alone and the losing registrar's records are " +
			"unrecoverable once it stops answering.\n\n" +

			"**Thorough, but never exhaustive.** DNS has no listing operation and AXFR is universally refused, so " +
			"the scan probes a wide list of well-known names — apex, common subdomains, MX, DKIM selectors, " +
			"provider verification hosts — and consolidates wildcards. A record it did not find may still exist. " +
			"A scan that shows no drift is therefore not proof that there is none, and a scan is not a backup: if " +
			"the old registrar exposes the zone through its own API, that is the authoritative copy.\n\n" +

			"**Rate limit: 20 calls per hour per account**, metered separately from the rest of the API because " +
			"each scan is roughly 90 DNS lookups. A data source is re-read on every plan *and* every apply, so a " +
			"`for_each` across a dozen domains can exhaust an hour's budget in a single `terraform plan`. Read one " +
			"domain at a time, and prefer running this deliberately during a migration over leaving it in a " +
			"configuration that is planned in CI.\n\n" +

			"The domain must be in the authenticated Porkbun account with API access enabled, even though the " +
			"records themselves come from external nameservers. A domain with a pending inbound transfer qualifies; " +
			"an arbitrary third-party domain does not.",
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				MarkdownDescription: "The domain to scan, e.g. `example.com`. Lowercase, with no trailing dot. It " +
					"must be in the authenticated account.",
				Required:   true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(3), canonicalDomain{}},
			},
			"record_count": schema.Int64Attribute{
				MarkdownDescription: "The number of records Porkbun reported for this scan. Null if the response " +
					"omitted the count. This is the API's own summary; compare it with `length(records)` rather " +
					"than assuming they agree.",
				Computed: true,
			},
			"records": schema.ListNestedAttribute{
				MarkdownDescription: "The records the nameservers answered with, sorted by `name`, then `type`, " +
					"then `content`. Porkbun does not document a stable ordering, so the provider imposes one to " +
					"keep plans quiet.",
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Computed: true,
							MarkdownDescription: "The subdomain only — `www`, `*` for a wildcard, or the empty " +
								"string at the apex. This is already the form `porkbun_dns_record.name` takes, so " +
								"the two compare directly. Note that `/dns/retrieve` and the " +
								"`porkbun_dns_record` *API* responses use fully-qualified names instead; a scan " +
								"may answer in either spelling and the provider normalizes it.",
						},
						"type": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "The DNS record type, e.g. `A`, `MX`, `TXT`.",
						},
						"content": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "The record's value, as the nameserver answered it.",
						},
						"ttl": schema.Int64Attribute{
							Computed: true,
							MarkdownDescription: "The TTL the nameserver returned, in seconds. Null when the " +
								"response carried none — never `0`, which would be a TTL no nameserver sent.",
						},
						"prio": schema.Int64Attribute{
							Computed: true,
							MarkdownDescription: "The priority, for `MX` and `SRV`. **Null on every other record " +
								"type**, because those have no priority at all; a `0` here means the nameserver " +
								"really did answer with priority zero.",
						},
					},
				},
			},
		},
	}
}

func (d *dnsScanDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *dnsScanDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config dnsScanModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain := config.Domain.ValueString()
	result, err := d.client.ScanDNS(ctx, domain)
	if err != nil {
		resp.Diagnostics.Append(dnsScanErrorDiagnostic(fmt.Sprintf("Unable to scan live DNS for %s", domain), err))
		return
	}

	records := make([]dnsScanRecordModel, 0, len(result.Records))
	for _, r := range result.Records {
		records = append(records, dnsScanRecordModel{
			Name:    types.StringValue(r.Name),
			Type:    types.StringValue(r.Type),
			Content: types.StringValue(r.Content),
			TTL:     optionalScanInt(r.TTL),
			Prio:    optionalScanInt(r.Prio),
		})
	}

	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: dnsScanRecordAttributeTypes()}, records)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	config.Records = list
	config.RecordCount = optionalScanInt(result.RecordCount)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// optionalScanInt keeps "the nameserver did not answer with one" out of the
// integer domain. Writing 0 for an absent TTL or an absent priority puts a
// value in state that the API never sent, and on a priority 0 is a legal
// value, so the two would be indistinguishable.
func optionalScanInt(v porkbun.ScannedInt) types.Int64 {
	if !v.Present() {
		return types.Int64Null()
	}
	return types.Int64Value(v.Int64())
}

// dnsScanErrorDiagnostic adds the one thing the shared remediation cannot
// know: /dns/scan has its own hourly budget, so a rate limit here is not
// evidence that the rest of the API is throttled, and waiting out the
// generic Retry-After is not the whole answer.
func dnsScanErrorDiagnostic(summary string, err error) diag.Diagnostic {
	d := apiErrorDiagnostic(summary, err)
	if porkbun.ErrorCode(err) != "RATE_LIMIT_EXCEEDED" {
		return d
	}
	return diag.NewErrorDiagnostic(d.Summary(), d.Detail()+
		"\n\n/dns/scan is metered separately at 20 calls per hour per account, because each scan is roughly 90 "+
		"DNS lookups. Data sources are re-read on every plan and every apply, so reading this for several domains "+
		"at once exhausts the budget quickly. Scan one domain at a time.")
}
