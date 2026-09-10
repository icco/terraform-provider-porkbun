package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ resource.Resource                   = (*domainNameserversResource)(nil)
	_ resource.ResourceWithConfigure      = (*domainNameserversResource)(nil)
	_ resource.ResourceWithImportState    = (*domainNameserversResource)(nil)
	_ resource.ResourceWithIdentity       = (*domainNameserversResource)(nil)
	_ resource.ResourceWithValidateConfig = (*domainNameserversResource)(nil)
)

// NewDomainNameserversResource manages registry nameserver delegation.
func NewDomainNameserversResource() resource.Resource { return &domainNameserversResource{} }

type domainNameserversResource struct {
	client *porkbun.Client
}

type domainNameserversModel struct {
	ID          types.String `tfsdk:"id"`
	Domain      types.String `tfsdk:"domain"`
	Nameservers types.Set    `tfsdk:"nameservers"`
}

type domainNameserversIdentity struct {
	Domain types.String `tfsdk:"domain"`
}

func (r *domainNameserversResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_nameservers"
}

func (r *domainNameserversResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Sets the nameservers a domain is delegated to at the registry — the Terraform equivalent " +
			"of editing a domain's nameservers in the Porkbun web UI.\n\n" +
			"~> **`terraform destroy` does not restore Porkbun's nameservers.** Porkbun has no endpoint that unsets a " +
			"delegation, so destroying this resource makes no API call: it drops the resource from state and leaves " +
			"the domain delegated exactly where it is.\n\n" +
			"~> Repointing a DNSSEC-signed domain takes it **completely dark** on validating resolvers. If a DS " +
			"record exists at Porkbun (`/dns/getDnssecRecords`) and the new nameservers do not serve the matching " +
			"signed zone, clear the DS record before or with the switch.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The domain name. Always equal to `domain`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"domain": schema.StringAttribute{
				MarkdownDescription: "The domain to delegate, e.g. `example.com`. Must be in the authenticated " +
					"Porkbun account with API access enabled for it. Must be lowercase with no trailing dot: the " +
					"value is compared literally and forces replacement, so `Example.com` and `example.com` would be " +
					"two resources fighting over one delegation.",
				Required:      true,
				Validators:    []validator.String{stringvalidator.LengthAtLeast(3), canonicalDomain{}},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"nameservers": schema.SetAttribute{
				MarkdownDescription: "The nameserver hostnames to delegate to, e.g. the `name_servers` output of a " +
					"`google_dns_managed_zone`. Case, trailing dots and ordering are ignored, so another provider's " +
					"output can be passed straight through. Between 2 and 13 distinct hostnames are required, counted " +
					"after duplicate spellings collapse.",
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.Set{
					setvalidator.SizeBetween(porkbun.MinNameservers, porkbun.MaxNameservers),
					setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(3)),
				},
				PlanModifiers: []planmodifier.Set{suppressNameserverRespelling{}},
			},
		},
	}
}

func (r *domainNameserversResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"domain": identityschema.StringAttribute{
				RequiredForImport: true,
				Description:       "The domain name.",
			},
		},
	}
}

func (r *domainNameserversResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResourceConfigure(ctx, req, resp)
}

func (r *domainNameserversResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainNameserversModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, plan, &resp.Diagnostics, &resp.State, resp.Identity)
}

func (r *domainNameserversResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan domainNameserversModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, plan, &resp.Diagnostics, &resp.State, resp.Identity)
}

// write is Create and Update: they are the same call. Porkbun has no create
// and no delete for delegation, only POST /domain/updateNs.
func (r *domainNameserversResource) write(
	ctx context.Context,
	plan domainNameserversModel,
	diags *diag.Diagnostics,
	state *tfsdk.State,
	identity *tfsdk.ResourceIdentity,
) {
	domain := plan.Domain.ValueString()

	var want []string
	diags.Append(plan.Nameservers.ElementsAs(ctx, &want, false)...)
	if diags.HasError() {
		return
	}

	if err := r.client.UpdateNameservers(ctx, domain, want); err != nil {
		diags.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to set nameservers for %s", domain), err))
		return
	}

	// updateNs answers a bare {"status":"SUCCESS"} without echoing what was
	// applied, so read the delegation back — but report the answer, never
	// store it.
	//
	// `nameservers` is Required, so core demands the state written here equal
	// the planned value exactly. /domain/getNs reads live from the registry
	// and propagation is not synchronous, so a write that succeeded can read
	// back stale; storing that would abort the apply with "Provider produced
	// inconsistent result after apply … This is a bug in the provider". Warn
	// instead: the next refresh shows persistent divergence as ordinary drift.
	switch applied, err := r.client.GetNameservers(ctx, domain); {
	case err != nil:
		tflog.Warn(ctx, "could not read back nameserver delegation", map[string]any{"domain": domain, "error": err.Error()})
		diags.AddWarning(
			fmt.Sprintf("Nameservers for %s were updated but could not be read back", domain),
			"Porkbun accepted the change, so the registry has it. Verifying it failed:\n\n"+err.Error()+
				"\n\nThe configured nameservers have been written to state, so nothing needs importing. "+
				"Run `terraform plan` to refresh from the registry and confirm.",
		)
	case !porkbun.SameNameserverSet(applied, want):
		tflog.Warn(ctx, "registry disagrees with the applied nameserver delegation",
			map[string]any{"domain": domain, "sent": porkbun.NormalizeNameservers(want), "registry": applied})
		diags.AddWarning(
			fmt.Sprintf("Nameservers for %s do not yet match what was sent", domain),
			fmt.Sprintf("Sent %v; /domain/getNs currently reports %v.\n\n", porkbun.NormalizeNameservers(want), applied)+
				"getNs reads live from the registry, so this is usually propagation lag and resolves on its own. "+
				"If it persists, the registry rejected or rewrote part of the set — the next `terraform plan` will "+
				"show it as drift.",
		)
	default:
		tflog.Info(ctx, "applied nameserver delegation", map[string]any{"domain": domain, "nameservers": applied})
	}

	// plan.Nameservers rather than a rebuilt set: it is the planned value
	// itself, so it cannot fail core's consistency check.
	model := domainNameserversModel{
		ID:          types.StringValue(domain),
		Domain:      types.StringValue(domain),
		Nameservers: plan.Nameservers,
	}
	diags.Append(state.Set(ctx, &model)...)
	diags.Append(setNameserversIdentity(ctx, identity, domain)...)
}

// ValidateConfig rejects a nameserver set that collapses below the floor.
//
// setvalidator.SizeBetween counts configured strings, but the wire payload is
// built from NormalizeNameservers, which folds case, strips trailing dots and
// de-duplicates: ["ns1.example.com", "ns1.example.com."] passes the size
// validator and delegates to one nameserver. Catching it here names the real
// problem at plan time; porkbun.UpdateNameservers refuses it again for
// anything that reaches the client by another route.
func (r *domainNameserversResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config domainNameserversModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A set that is unknown, or holds an unknown element — another provider's
	// name_servers output before apply — has nothing to count yet.
	if !setIsFullyKnown(config.Nameservers) {
		return
	}

	var ns []string
	resp.Diagnostics.Append(config.Nameservers.ElementsAs(ctx, &ns, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if normalized := porkbun.NormalizeNameservers(ns); len(normalized) < porkbun.MinNameservers {
		resp.Diagnostics.AddAttributeError(
			path.Root("nameservers"),
			"Too few distinct nameservers",
			fmt.Sprintf("%d configured nameservers collapse to %d distinct hostname(s): %v.\n\n", len(ns), len(normalized), normalized)+
				"Nameservers are compared with case folded, any trailing dot removed and duplicates dropped, so "+
				"entries such as \"ns1.example.com\" and \"ns1.example.com.\" are the same nameserver. A delegation "+
				fmt.Sprintf("needs at least %d distinct hostnames.", porkbun.MinNameservers),
		)
	}
}

// setIsFullyKnown reports whether ElementsAs can convert v: the set and every
// element is known and non-null.
func setIsFullyKnown(v types.Set) bool {
	if v.IsNull() || v.IsUnknown() {
		return false
	}
	for _, e := range v.Elements() {
		if e.IsNull() || e.IsUnknown() {
			return false
		}
	}
	return true
}

func (r *domainNameserversResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainNameserversModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain := state.Domain.ValueString()
	current, err := r.client.GetNameservers(ctx, domain)
	if err != nil {
		if porkbun.IsNotFound(err) {
			tflog.Warn(ctx, "domain no longer in the porkbun account, removing from state", map[string]any{"domain": domain})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to read nameservers for %s", domain), err))
		return
	}

	var prior []string
	resp.Diagnostics.Append(state.Nameservers.ElementsAs(ctx, &prior, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	refreshed, sdiags := newNameserversState(ctx, domain, current, prior)
	resp.Diagnostics.Append(sdiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &refreshed)...)
	resp.Diagnostics.Append(setNameserversIdentity(ctx, resp.Identity, domain)...)
}

// Delete deliberately makes no API call. There is no endpoint that unsets a
// domain's nameservers, and Porkbun's own defaults are documented nowhere;
// ns:[] is schema-legal but undocumented and could de-delegate the domain at
// the registry. Do not "fix" this by sending an empty list — destroy means
// "stop managing this delegation", and the warning says so out loud.
func (r *domainNameserversResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state domainNameserversModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := state.Domain.ValueString()
	tflog.Warn(ctx, "destroying porkbun_domain_nameservers without changing the registry", map[string]any{"domain": domain})
	resp.Diagnostics.AddWarning(
		fmt.Sprintf("Nameservers for %s were left unchanged at the registry", domain),
		"Destroying porkbun_domain_nameservers removes it from Terraform state and makes no API call: Porkbun has no "+
			"endpoint that unsets a domain's nameservers, and there is no documented list of its defaults to restore. "+
			fmt.Sprintf("%s remains delegated to whatever it is delegated to now. ", domain)+
			"Change it in the Porkbun web UI if that is not what you want.",
	)
}

// ImportState accepts either `terraform import ... example.com` or a
// Terraform 1.12+ import block carrying an identity of {domain = "..."}.
func (r *domainNameserversResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughWithIdentity(ctx, path.Root("domain"), path.Root("domain"), req, resp)
}

// setNameserversIdentity writes the resource identity, when the running
// Terraform supports one.
func setNameserversIdentity(ctx context.Context, identity *tfsdk.ResourceIdentity, domain string) diag.Diagnostics {
	if identity == nil {
		return nil
	}
	return identity.Set(ctx, domainNameserversIdentity{Domain: types.StringValue(domain)})
}

// newNameserversState builds the state value for a domain.
//
// When the registry's set matches the prior state, that spelling is kept so
// state stays equal to the configuration; otherwise the registry's
// normalized answer wins, which is what makes out-of-band drift visible.
func newNameserversState(ctx context.Context, domain string, current, prior []string) (domainNameserversModel, diag.Diagnostics) {
	value := porkbun.NormalizeNameservers(current)
	if porkbun.SameNameserverSet(current, prior) && len(prior) > 0 {
		value = prior
	}
	set, diags := types.SetValueFrom(ctx, types.StringType, value)
	return domainNameserversModel{
		ID:          types.StringValue(domain),
		Domain:      types.StringValue(domain),
		Nameservers: set,
	}, diags
}
