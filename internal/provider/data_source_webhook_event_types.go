package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ datasource.DataSource              = (*webhookEventTypesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*webhookEventTypesDataSource)(nil)
)

func init() { registerDataSource(NewWebhookEventTypesDataSource) }

// NewWebhookEventTypesDataSource reads the webhook event type catalog.
func NewWebhookEventTypesDataSource() datasource.DataSource {
	return &webhookEventTypesDataSource{}
}

type webhookEventTypesDataSource struct {
	client *porkbun.Client
}

type webhookEventTypesModel struct {
	EventTypes types.Set `tfsdk:"event_types"`
}

func (d *webhookEventTypesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook_event_types"
}

func (d *webhookEventTypesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the catalog of event types a Porkbun webhook endpoint can subscribe to " +
			"(`/webhook/eventTypes`).\n\n" +
			"The catalog is read live and grows as Porkbun adds events, which is the reason to read it rather " +
			"than hard-code a list. Names look like `domain.renewed` and `dns.record.created`.\n\n" +
			"**The catalog is not the full set of legal subscription values.** The `events` field of " +
			"`/webhook/create` also accepts a prefix wildcard such as `dns.*`, and `*` for everything — and " +
			"`*` is what Porkbun recommends, since it picks up new event types automatically. Neither wildcard " +
			"is ever returned by this endpoint, so a comparison that requires every configured subscription to " +
			"appear in `event_types` will reject a valid `*`.",
		Attributes: map[string]schema.Attribute{
			"event_types": schema.SetAttribute{
				MarkdownDescription: "The subscribable event types, trimmed and de-duplicated. Null — not an " +
					"empty set — if Porkbun answered successfully but sent no `eventTypes` field at all; an " +
					"empty set means Porkbun really did report an empty catalog.",
				Computed:    true,
				ElementType: types.StringType,
			},
		},
	}
}

func (d *webhookEventTypesDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *webhookEventTypesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state webhookEventTypesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	eventTypes, err := d.client.ListWebhookEventTypes(ctx)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Unable to read Porkbun webhook event types", err))
		return
	}

	// A nil slice means the field was absent or null, which is not an empty
	// catalog. Writing an empty set would claim Porkbun said "nothing is
	// subscribable" when it said nothing at all.
	if eventTypes == nil {
		resp.Diagnostics.AddWarning(
			"Porkbun returned no webhook event type catalog",
			"Porkbun answered /webhook/eventTypes with status SUCCESS but the response carried no `eventTypes` "+
				"field, so `event_types` is null rather than an empty set. Treat this as the API being "+
				"unavailable, not as an account with nothing to subscribe to.",
		)
		state.EventTypes = types.SetNull(types.StringType)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	set, diags := types.SetValueFrom(ctx, types.StringType, eventTypes)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.EventTypes = set
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
