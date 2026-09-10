package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ datasource.DataSource              = (*accountBalanceDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*accountBalanceDataSource)(nil)
)

func init() { registerDataSource(NewAccountBalanceDataSource) }

// NewAccountBalanceDataSource reads the account's available credit.
func NewAccountBalanceDataSource() datasource.DataSource { return &accountBalanceDataSource{} }

type accountBalanceDataSource struct {
	client *porkbun.Client
}

type accountBalanceModel struct {
	BalanceCents types.Int64  `tfsdk:"balance_cents"`
	Display      types.String `tfsdk:"display"`
}

func (d *accountBalanceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_account_balance"
}

func (d *accountBalanceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the available account credit for the authenticated Porkbun account " +
			"(`GET /account/balance`). It takes no arguments — the credentials select the account.\n\n" +
			"Registrations, renewals and transfers draw down this credit, so the usual use is a `check` block " +
			"that fails the plan before a wide apply runs the account dry halfway through.\n\n" +
			"The balance is read live on every plan and refresh, so any configuration that depends on it is " +
			"never fully known until apply.",
		Attributes: map[string]schema.Attribute{
			"balance_cents": schema.Int64Attribute{
				Computed: true,
				MarkdownDescription: "Available credit **in cents**, as Porkbun reports it. Cents, not a " +
					"decimal amount: comparing money as a float is how a $50.00 floor passes at $49.99, so do the " +
					"arithmetic here in whole cents and use `display` for anything a human reads.",
			},
			"display": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Porkbun's own rendering of the balance, e.g. `$12.34`. It is the only " +
					"part of the response that names a currency, so it is passed through verbatim rather than " +
					"formatted from `balance_cents`. Treat it as display text, not as something to parse.",
			},
		},
	}
}

func (d *accountBalanceDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *accountBalanceDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	bal, err := d.client.GetBalance(ctx)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Unable to read the Porkbun account balance", err))
		return
	}

	state := accountBalanceModel{
		BalanceCents: types.Int64Value(bal.Cents),
		Display:      types.StringValue(bal.Display),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
