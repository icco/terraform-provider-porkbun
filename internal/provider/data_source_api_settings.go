package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ datasource.DataSource              = (*apiSettingsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*apiSettingsDataSource)(nil)
)

func init() { registerDataSource(NewAPISettingsDataSource) }

// NewAPISettingsDataSource reads the account's API spend controls.
func NewAPISettingsDataSource() datasource.DataSource { return &apiSettingsDataSource{} }

type apiSettingsDataSource struct {
	client *porkbun.Client
}

type apiSettingsModel struct {
	MonthlySpendLimit types.Int64 `tfsdk:"monthly_spend_limit"`
	LowBalanceAlert   types.Int64 `tfsdk:"low_balance_alert"`
	AutoTopup         types.Bool  `tfsdk:"auto_topup"`
	TopupThreshold    types.Int64 `tfsdk:"topup_threshold"`
	TopupAmount       types.Int64 `tfsdk:"topup_amount"`
	MonthlySpend      types.Int64 `tfsdk:"monthly_spend"`
}

func (d *apiSettingsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_settings"
}

func (d *apiSettingsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the account-wide API spend controls, and this calendar month's API spend so far, " +
			"from `/account/apiSettings`.\n\n**Every amount is in cents**, the same unit `porkbun_domain` prices and " +
			"the `cost` field of a registration use.\n\nThe limits are account-wide rather than per key, and Porkbun " +
			"enforces them itself on registrations, renewals and transfers: an apply that would breach " +
			"`monthly_spend_limit` fails at the API. Read them here to fail earlier, in a `precondition`, with a " +
			"message that says why.\n\nEach limit is `null` when it is not configured, which means *no limit*. That " +
			"is not the same as `0`, which would mean no API spending is permitted at all — compare with " +
			"`== null`, never with `== 0`.",
		Attributes: map[string]schema.Attribute{
			"monthly_spend_limit": schema.Int64Attribute{
				Computed: true,
				MarkdownDescription: "Cap on API spend per calendar month, in cents. `null` when no cap is " +
					"configured; Porkbun rejects the spend that would cross a configured cap.",
			},
			"low_balance_alert": schema.Int64Attribute{
				Computed: true,
				MarkdownDescription: "Balance, in cents, below which Porkbun emails the account. `null` when the " +
					"alert is off.",
			},
			"auto_topup": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether automatic balance top-up is enabled. When false, `topup_threshold` " +
					"and `topup_amount` are whatever was last configured and have no effect.",
			},
			"topup_threshold": schema.Int64Attribute{
				Computed: true,
				MarkdownDescription: "Balance, in cents, at which an automatic top-up is triggered. `null` when no " +
					"threshold is configured.",
			},
			"topup_amount": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Amount, in cents, an automatic top-up adds. `null` when unset.",
			},
			"monthly_spend": schema.Int64Attribute{
				Computed: true,
				MarkdownDescription: "API spend so far this calendar month, in cents. This is a running total, not " +
					"a setting: it changes under Terraform, so it will differ between a plan and the next refresh.",
			},
		},
	}
}

func (d *apiSettingsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *apiSettingsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	settings, err := d.client.GetAPISettings(ctx)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Unable to read Porkbun API settings", err))
		return
	}

	// Int64PointerValue carries the unset limits through as null rather than
	// 0, which is the whole point of the client returning pointers.
	state := apiSettingsModel{
		MonthlySpendLimit: types.Int64PointerValue(settings.MonthlySpendLimit),
		LowBalanceAlert:   types.Int64PointerValue(settings.LowBalanceAlert),
		AutoTopup:         types.BoolValue(settings.AutoTopup),
		TopupThreshold:    types.Int64PointerValue(settings.TopupThreshold),
		TopupAmount:       types.Int64PointerValue(settings.TopupAmount),
		MonthlySpend:      types.Int64Value(settings.MonthlySpend),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
