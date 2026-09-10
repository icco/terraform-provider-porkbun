package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ datasource.DataSource              = (*dnsRecordsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*dnsRecordsDataSource)(nil)
)

func init() { registerDataSource(NewDNSRecordsDataSource) }

// NewDNSRecordsDataSource lists the DNS records in a Porkbun-hosted zone.
func NewDNSRecordsDataSource() datasource.DataSource { return &dnsRecordsDataSource{} }

type dnsRecordsDataSource struct {
	client *porkbun.Client
}

type dnsRecordsModel struct {
	Domain types.String `tfsdk:"domain"`
	Type   types.String `tfsdk:"type"`
	Name   types.String `tfsdk:"name"`

	Cloudflare        types.String `tfsdk:"cloudflare"`
	CloudflareEnabled types.Bool   `tfsdk:"cloudflare_enabled"`
	Records           types.List   `tfsdk:"records"`
	TotalCount        types.Int64  `tfsdk:"total_count"`
}

// dnsRecordObjectModel is one record, shared by the nested objects of
// porkbun_dns_records.records and the flat attributes of porkbun_dns_record.
type dnsRecordObjectModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Subdomain types.String `tfsdk:"subdomain"`
	Type      types.String `tfsdk:"type"`
	Content   types.String `tfsdk:"content"`
	TTL       types.Int64  `tfsdk:"ttl"`
	Prio      types.Int64  `tfsdk:"prio"`
	Notes     types.String `tfsdk:"notes"`
}

func dnsRecordAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":        types.StringType,
		"name":      types.StringType,
		"subdomain": types.StringType,
		"type":      types.StringType,
		"content":   types.StringType,
		"ttl":       types.Int64Type,
		"prio":      types.Int64Type,
		"notes":     types.StringType,
	}
}

// dnsRecordComputedAttributes is the read-only shape of a record. The
// singular data source overrides "id" to Required; everything else is
// identical, which is the point — one record looks the same wherever it is
// read from.
func dnsRecordComputedAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "The Porkbun record ID, a decimal integer carried as a string.",
		},
		"name": schema.StringAttribute{
			Computed: true,
			MarkdownDescription: "The fully-qualified record name as Porkbun returns it, e.g. `www.example.com`. " +
				"The apex is the bare domain.",
		},
		"subdomain": schema.StringAttribute{
			Computed: true,
			MarkdownDescription: "`name` with the domain stripped off: `www`, `*` for a wildcard, or the empty " +
				"string for the apex. This — not `name` — is what `porkbun_dns_record.name` takes.",
		},
		"type": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "The DNS record type, e.g. `A`.",
		},
		"content": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "The record value, e.g. `1.2.3.4` for an `A` record.",
		},
		"ttl": schema.Int64Attribute{
			Computed:            true,
			MarkdownDescription: "Time to live, in seconds. Null if Porkbun reported no TTL for the record.",
		},
		"prio": schema.Int64Attribute{
			Computed: true,
			MarkdownDescription: "Priority, used by `MX` and `SRV` records. **Null when the record has no " +
				"priority**, which is what Porkbun sends for every other type; a priority of `0` is a real, " +
				"explicitly-set value and is reported as `0`.",
		},
		"notes": schema.StringAttribute{
			Computed: true,
			MarkdownDescription: "Free-text notes stored with the record at Porkbun, never served in DNS. Null " +
				"when the record carries none.",
		},
	}
}

// zoneCloudflareAttributes are the zone-level fields both DNS data sources
// carry. They are the answer to "is this zone still the one that resolves".
func zoneCloudflareAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"cloudflare": schema.StringAttribute{
			Computed: true,
			MarkdownDescription: "Porkbun's zone-level Cloudflare flag: `enabled` or `disabled`. Null when the " +
				"API did not report it — which is not the same as `disabled`.\n\n" +
				"When this is `enabled` the domain has been moved to Cloudflare, and Cloudflare's copy of the " +
				"zone is what resolvers answer from. The records here are Porkbun's copy: still readable, still " +
				"writable, and no longer authoritative.",
		},
		"cloudflare_enabled": schema.BoolAttribute{
			Computed: true,
			MarkdownDescription: "`cloudflare` as a boolean, for use in a `lifecycle.precondition`. Null when " +
				"Porkbun did not report the flag.",
		},
	}
}

func (d *dnsRecordsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_records"
}

func (d *dnsRecordsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"domain": schema.StringAttribute{
			MarkdownDescription: "The domain whose zone to read, e.g. `example.com`. Lowercase, with no trailing dot.",
			Required:            true,
			Validators:          []validator.String{stringvalidator.LengthAtLeast(3), canonicalDomain{}},
		},
		"type": schema.StringAttribute{
			MarkdownDescription: "Return only records of this type, e.g. `MX`. Uppercase. Omit for no filter. " +
				"Filtering happens in the provider, so `total_count` still counts the whole zone.",
			Optional:   true,
			Validators: []validator.String{stringvalidator.OneOf(porkbun.RecordTypes...)},
		},
		"name": schema.StringAttribute{
			MarkdownDescription: "Return only records whose `subdomain` matches, compared case-insensitively: " +
				"`www`, or `\"\"` for the zone apex. Omitting the attribute and setting it to `\"\"` mean " +
				"different things — omitted is no filter, empty matches the apex only.",
			Optional: true,
		},
		"records": schema.ListNestedAttribute{
			MarkdownDescription: "The matching records, sorted by `name`, then `type`, then `content`, then `id`, " +
				"so the list is stable across reads.",
			Computed:     true,
			NestedObject: schema.NestedAttributeObject{Attributes: dnsRecordComputedAttributes()},
		},
		"total_count": schema.Int64Attribute{
			MarkdownDescription: "How many records the zone holds before `type` and `name` are applied. Compare " +
				"it against `length(records)` to see how much a filter removed.\n\n" +
				"It is not the size of the zone Porkbun serves: `/dns/retrieve` omits the SOA record and " +
				"Porkbun's own default NS records, and neither is counted here.",
			Computed: true,
		},
	}
	for k, v := range zoneCloudflareAttributes() {
		attrs[k] = v
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads every editable DNS record in a Porkbun-hosted zone, optionally filtered by " +
			"type or name.\n\n" +
			"~> The result is **not the full zone**. Porkbun excludes the SOA record and its own default NS " +
			"records from this endpoint, so a zone that looks empty here may still be answering queries. Check " +
			"`cloudflare` too: when it is `enabled`, Cloudflare — not Porkbun — is what resolvers read.",
		Attributes: attrs,
	}
}

func (d *dnsRecordsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *dnsRecordsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config dnsRecordsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain := config.Domain.ValueString()
	zone, err := d.client.RetrieveZone(ctx, domain)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to read DNS records for %s", domain), err))
		return
	}

	// Count before filtering: a filtered list that reports its own length as
	// the zone size hides how much was dropped.
	config.TotalCount = types.Int64Value(int64(len(zone.Records)))
	setZoneCloudflare(zone, &config.Cloudflare, &config.CloudflareEnabled)

	records := make([]dnsRecordObjectModel, 0, len(zone.Records))
	for _, rec := range zone.Records {
		model := dnsRecordToModel(rec, domain)
		if !config.Type.IsNull() && !strings.EqualFold(model.Type.ValueString(), config.Type.ValueString()) {
			continue
		}
		if !config.Name.IsNull() && !strings.EqualFold(model.Subdomain.ValueString(), config.Name.ValueString()) {
			continue
		}
		records = append(records, model)
	}
	sortDNSRecordModels(records)

	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: dnsRecordAttributeTypes()}, records)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	config.Records = list

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// setZoneCloudflare writes the zone flag, leaving both attributes null when
// Porkbun did not report it.
func setZoneCloudflare(zone *porkbun.Zone, raw *types.String, enabled *types.Bool) {
	on, reported := zone.ProxiedByCloudflare()
	if !reported {
		*raw = types.StringNull()
		*enabled = types.BoolNull()
		return
	}
	*raw = types.StringValue(zone.Cloudflare)
	*enabled = types.BoolValue(on)
}

// dnsRecordToModel converts one record, keeping Porkbun's nulls as Terraform
// nulls. A null prio rendered as 0 would claim a priority the API never sent.
func dnsRecordToModel(rec porkbun.ZoneRecord, domain string) dnsRecordObjectModel {
	model := dnsRecordObjectModel{
		ID:        types.StringValue(rec.ID),
		Name:      types.StringValue(rec.Name),
		Subdomain: types.StringValue(porkbun.SubdomainOf(rec.Name, domain)),
		Type:      types.StringValue(rec.Type),
		Content:   types.StringValue(rec.Content),
		TTL:       types.Int64Null(),
		Prio:      types.Int64Null(),
		Notes:     types.StringNull(),
	}
	if rec.TTLSet {
		model.TTL = types.Int64Value(rec.TTL.Int64())
	}
	if rec.PrioSet {
		model.Prio = types.Int64Value(rec.Prio.Int64())
	}
	if rec.NotesSet {
		model.Notes = types.StringValue(rec.Notes)
	}
	return model
}

// sortDNSRecordModels imposes a total order. Porkbun promises none, and a
// list attribute is positional: an unsorted list re-orders itself between
// reads and shows a diff that is not a change.
func sortDNSRecordModels(records []dnsRecordObjectModel) {
	sort.Slice(records, func(i, j int) bool {
		a, b := records[i], records[j]
		for _, pair := range [][2]string{
			{a.Name.ValueString(), b.Name.ValueString()},
			{a.Type.ValueString(), b.Type.ValueString()},
			{a.Content.ValueString(), b.Content.ValueString()},
			{a.ID.ValueString(), b.ID.ValueString()},
		} {
			if pair[0] != pair[1] {
				return pair[0] < pair[1]
			}
		}
		return false
	})
}
