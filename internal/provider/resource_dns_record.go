package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ resource.Resource                = (*dnsRecordResource)(nil)
	_ resource.ResourceWithConfigure   = (*dnsRecordResource)(nil)
	_ resource.ResourceWithImportState = (*dnsRecordResource)(nil)
	_ resource.ResourceWithIdentity    = (*dnsRecordResource)(nil)
)

// NewDNSRecordResource manages a record in a Porkbun-hosted DNS zone.
func NewDNSRecordResource() resource.Resource { return &dnsRecordResource{} }

type dnsRecordResource struct {
	client *porkbun.Client
}

type dnsRecordModel struct {
	ID      types.String `tfsdk:"id"`
	Domain  types.String `tfsdk:"domain"`
	Name    types.String `tfsdk:"name"`
	Type    types.String `tfsdk:"type"`
	Content types.String `tfsdk:"content"`
	TTL     types.Int64  `tfsdk:"ttl"`
	Prio    types.Int64  `tfsdk:"prio"`
	Notes   types.String `tfsdk:"notes"`
}

type dnsRecordIdentity struct {
	Domain   types.String `tfsdk:"domain"`
	RecordID types.String `tfsdk:"record_id"`
}

func (r *dnsRecordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_record"
}

func (r *dnsRecordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A DNS record in a zone hosted on Porkbun's nameservers.\n\n" +
			"~> **Only useful while the domain is still on Porkbun's nameservers.** Once it is delegated elsewhere " +
			"(`not_local` is true on the `porkbun_domain` data source), Porkbun keeps accepting these writes and " +
			"answering `SUCCESS`, but no resolver ever reads that zone: the apply goes green and nothing changes. " +
			"Manage the records where the zone actually lives instead — `google_dns_record_set` for a zone in " +
			"Cloud DNS, and so on.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The Porkbun record ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"domain": schema.StringAttribute{
				MarkdownDescription: "The domain the record belongs to, e.g. `example.com`. Lowercase, with no " +
					"trailing dot. Changing it replaces the record.",
				Required:      true,
				Validators:    []validator.String{stringvalidator.LengthAtLeast(3), canonicalDomain{}},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The subdomain, without the domain itself: `www`, `*` for a wildcard, or the " +
					"empty string for the zone apex. Defaults to the apex. Changing it replaces the record.",
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString(""),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured()},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "The DNS record type: one of `A`, `AAAA`, `MX`, `CNAME`, `ALIAS`, `TXT`, `NS`, " +
					"`SRV`, `TLSA`, `CAA`, `SSHFP`, `HTTPS`, `SVCB`. Changing it replaces the record.",
				Required:      true,
				Validators:    []validator.String{stringvalidator.OneOf(porkbun.RecordTypes...)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"content": schema.StringAttribute{
				MarkdownDescription: "The record value, e.g. `1.2.3.4` for an `A` record.",
				Required:            true,
			},
			"ttl": schema.Int64Attribute{
				MarkdownDescription: "Time to live, in seconds. Porkbun's minimum is 600, which is also the default.",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(600),
				Validators:          []validator.Int64{int64validator.AtLeast(600)},
			},
			"prio": schema.Int64Attribute{
				MarkdownDescription: "Priority, used by `MX` and `SRV` records. Defaults to 0.",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(0),
				Validators:          []validator.Int64{int64validator.AtLeast(0)},
			},
			"notes": schema.StringAttribute{
				MarkdownDescription: "Free-text notes stored with the record at Porkbun. Not served in DNS.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
			},
		},
	}
}

func (r *dnsRecordResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"domain": identityschema.StringAttribute{
				RequiredForImport: true,
				Description:       "The domain the record belongs to.",
			},
			"record_id": identityschema.StringAttribute{
				RequiredForImport: true,
				Description:       "The Porkbun record ID, a decimal integer. Record IDs are visible in the Porkbun DNS UI.",
			},
		},
	}
}

func (r *dnsRecordResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResourceConfigure(ctx, req, resp)
}

func (r *dnsRecordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, wc := withWarnings(ctx)
	defer appendWarnings(&resp.Diagnostics, wc)

	domain := plan.Domain.ValueString()
	id, existingID, err := r.client.CreateRecord(ctx, domain, recordInput(plan))
	if err != nil {
		summary := fmt.Sprintf("Unable to create DNS record on %s", domain)
		if existingID != "" {
			summary += fmt.Sprintf(" (an identical record already exists with ID %s; import it with `terraform import ... %s/%s`)", existingID, domain, existingID)
		}
		resp.Diagnostics.Append(apiErrorDiagnostic(summary, err))
		return
	}

	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(setDNSRecordIdentity(ctx, resp.Identity, domain, id)...)
}

func (r *dnsRecordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, wc := withWarnings(ctx)
	defer appendWarnings(&resp.Diagnostics, wc)

	domain := state.Domain.ValueString()
	id := state.ID.ValueString()

	record, found, err := r.client.RetrieveRecord(ctx, domain, id)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to read DNS record %s on %s", id, domain), err))
		return
	}
	if !found {
		tflog.Warn(ctx, "dns record no longer exists, removing from state", map[string]any{"domain": domain, "id": id})
		resp.State.RemoveResource(ctx)
		return
	}

	// Refresh every attribute: anything omitted here is drift Terraform can
	// never detect.
	state.ID = types.StringValue(id)
	state.Name = types.StringValue(porkbun.SubdomainOf(record.Name, domain))
	state.Type = types.StringValue(record.Type)
	state.Content = types.StringValue(record.Content)
	state.TTL = types.Int64Value(record.TTL.Int64())
	state.Prio = types.Int64Value(record.Prio.Int64())
	state.Notes = types.StringValue(record.Notes)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(setDNSRecordIdentity(ctx, resp.Identity, domain, id)...)
}

func (r *dnsRecordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state dnsRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, wc := withWarnings(ctx)
	defer appendWarnings(&resp.Diagnostics, wc)

	domain := plan.Domain.ValueString()
	id := state.ID.ValueString()

	if err := r.client.EditRecord(ctx, domain, id, recordInput(plan)); err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to update DNS record %s on %s", id, domain), err))
		return
	}

	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(setDNSRecordIdentity(ctx, resp.Identity, domain, id)...)
}

func (r *dnsRecordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, wc := withWarnings(ctx)
	defer appendWarnings(&resp.Diagnostics, wc)

	domain := state.Domain.ValueString()
	id := state.ID.ValueString()

	if err := r.client.DeleteRecord(ctx, domain, id); err != nil {
		if porkbun.IsNotFound(err) {
			return
		}
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to delete DNS record %s on %s", id, domain), err))
	}
}

// ImportState accepts `domain/record_id`, or a Terraform 1.12+ import block
// with an identity of {domain, record_id}. The record ID alone is not enough:
// every /dns/* call needs the domain too.
func (r *dnsRecordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.Identity != nil && !req.Identity.Raw.IsNull() {
		var identity dnsRecordIdentity
		resp.Diagnostics.Append(req.Identity.Get(ctx, &identity)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if identity.Domain.ValueString() != "" && identity.RecordID.ValueString() != "" {
			r.setImportedState(ctx, resp, identity.Domain.ValueString(), identity.RecordID.ValueString())
			return
		}
	}

	domain, id, ok := strings.Cut(req.ID, "/")
	if !ok || domain == "" || id == "" {
		resp.Diagnostics.AddError(
			"Invalid import identifier",
			"Import porkbun_dns_record as \"domain/record_id\", e.g. "+
				"`terraform import porkbun_dns_record.www example.com/123456789`. "+
				"Record IDs are visible in the Porkbun DNS UI and in the `porkbun_dns_record` state of other records.",
		)
		return
	}
	r.setImportedState(ctx, resp, domain, id)
}

func (r *dnsRecordResource) setImportedState(ctx context.Context, resp *resource.ImportStateResponse, domain, id string) {
	recordID, err := porkbun.ParseRecordID(id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid record ID",
			fmt.Sprintf("%q is not a Porkbun record ID; record IDs are numeric.", id),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain"), domain)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), recordID)...)
	resp.Diagnostics.Append(setDNSRecordIdentity(ctx, resp.Identity, domain, recordID)...)
}

func setDNSRecordIdentity(ctx context.Context, identity *tfsdk.ResourceIdentity, domain, id string) diag.Diagnostics {
	if identity == nil {
		return nil
	}
	return identity.Set(ctx, dnsRecordIdentity{
		Domain:   types.StringValue(domain),
		RecordID: types.StringValue(id),
	})
}

func recordInput(m dnsRecordModel) porkbun.RecordInput {
	return porkbun.RecordInput{
		Name:    m.Name.ValueString(),
		Type:    m.Type.ValueString(),
		Content: m.Content.ValueString(),
		TTL:     m.TTL.ValueInt64(),
		Prio:    m.Prio.ValueInt64(),
		Notes:   m.Notes.ValueString(),
	}
}
