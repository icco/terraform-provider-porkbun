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
	_ datasource.DataSource              = (*domainAvailabilityDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*domainAvailabilityDataSource)(nil)
)

func init() { registerDataSource(NewDomainAvailabilityDataSource) }

// NewDomainAvailabilityDataSource checks whether one domain can be
// registered, and at what price.
func NewDomainAvailabilityDataSource() datasource.DataSource {
	return &domainAvailabilityDataSource{}
}

type domainAvailabilityDataSource struct {
	client *porkbun.Client
}

type domainAvailabilityModel struct {
	Domain         types.String `tfsdk:"domain"`
	Available      types.Bool   `tfsdk:"available"`
	PriceType      types.String `tfsdk:"price_type"`
	Price          types.String `tfsdk:"price"`
	RegularPrice   types.String `tfsdk:"regular_price"`
	FirstYearPromo types.Bool   `tfsdk:"first_year_promo"`
	Premium        types.Bool   `tfsdk:"premium"`
	MinDuration    types.Int64  `tfsdk:"min_duration"`
	Renewal        types.Object `tfsdk:"renewal"`
	Transfer       types.Object `tfsdk:"transfer"`
}

// domainPriceModel is the shape of the renewal and transfer objects.
type domainPriceModel struct {
	PriceType    types.String `tfsdk:"price_type"`
	Price        types.String `tfsdk:"price"`
	RegularPrice types.String `tfsdk:"regular_price"`
}

// domainPriceAttributeTypes mirrors domainPriceModel for the nested objects.
func domainPriceAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"price_type":    types.StringType,
		"price":         types.StringType,
		"regular_price": types.StringType,
	}
}

func domainPriceObject(ctx context.Context, p porkbun.DomainPrice) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, domainPriceAttributeTypes(), domainPriceModel{
		PriceType:    types.StringValue(p.Type),
		Price:        types.StringValue(p.Price),
		RegularPrice: types.StringValue(p.RegularPrice),
	})
}

func (d *domainAvailabilityDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_availability"
}

// domainPriceAttributes describes one nested price object. Every attribute
// carries Computed itself: tfplugindocs reads the inner attributes, not the
// wrapper, so leaving them off renders the block as configurable input.
func domainPriceAttributes(kind string) map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"price_type": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Porkbun's label for this price line, e.g. `" + kind + "`.",
		},
		"price": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "The " + kind + " price in USD, as a decimal string.",
		},
		"regular_price": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "The standard, non-promotional " + kind + " price in USD, as a decimal string.",
		},
	}
}

func (d *domainAvailabilityDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Checks whether a domain is available to register, and what Porkbun currently charges " +
			"to register, renew and transfer it. Unlike the other data sources here, this works for any domain name — " +
			"it does not have to be in the authenticated account.\n\n" +
			"~> **This endpoint is rate limited to one check per 10 seconds per account** by default (configurable " +
			"per API key). Data sources are read on every `terraform plan`, not just on apply, so a configuration " +
			"holding several of these burns the budget on every plan. Porkbun answers a tripped limit either with " +
			"an HTTP 429, which the provider waits out and retries, or with a `RATE_LIMIT_EXCEEDED` body on an " +
			"otherwise-ordinary response, which it cannot tell is worth retrying — in that case the plan fails " +
			"rather than waiting. Check one domain at a time, or run with `-parallelism=1`.\n\n" +
			"Prices are decimal strings in USD, kept as strings so no rounding happens between Porkbun and " +
			"Terraform. `/domain/create` wants the amount in pennies.",
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				MarkdownDescription: "The domain to check, e.g. `example.com`. Lowercase, with no trailing dot.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(3), canonicalDomain{}},
			},
			"available": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether the domain is available for registration. False for a name that is " +
					"already registered, including one registered in this same account.",
			},
			"price_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Porkbun's label for the primary price, always `registration`.",
			},
			"price": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The registration price per year in USD, as a decimal string, e.g. `9.73`. " +
					"This is the promotional price when `first_year_promo` is true.",
			},
			"regular_price": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The standard, non-promotional registration price in USD. Equal to `price` " +
					"unless `first_year_promo` is true.",
			},
			"first_year_promo": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether `price` is a discounted first-year rate. When true, renewals cost " +
					"`regular_price`, which for some TLDs is several times the first year.",
			},
			"premium": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether the registry prices this name as a premium. **Premium domains cannot " +
					"be registered through the API**, at any price.",
			},
			"min_duration": schema.Int64Attribute{
				Computed: true,
				MarkdownDescription: "The registry's minimum registration term in years, usually `1`. Registrations " +
					"are always for exactly this term.",
			},
			"renewal": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "What renewing this domain costs, from the response's `additional` pricing.",
				Attributes:          domainPriceAttributes("renewal"),
			},
			"transfer": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "What transferring this domain in costs, from the response's `additional` pricing.",
				Attributes:          domainPriceAttributes("transfer"),
			},
		},
	}
}

func (d *domainAvailabilityDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *domainAvailabilityDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config domainAvailabilityModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := config.Domain.ValueString()
	avail, err := d.client.CheckDomain(ctx, name)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to check availability of %s", name), err))
		return
	}

	renewal, diags := domainPriceObject(ctx, avail.Renewal)
	resp.Diagnostics.Append(diags...)
	transfer, diags := domainPriceObject(ctx, avail.Transfer)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := domainAvailabilityModel{
		// The API never echoes the name back, so this comes from config.
		Domain:         types.StringValue(name),
		Available:      types.BoolValue(avail.Available),
		PriceType:      types.StringValue(avail.Registration.Type),
		Price:          types.StringValue(avail.Registration.Price),
		RegularPrice:   types.StringValue(avail.Registration.RegularPrice),
		FirstYearPromo: types.BoolValue(avail.FirstYearPromo),
		Premium:        types.BoolValue(avail.Premium),
		MinDuration:    types.Int64Value(avail.MinDuration),
		Renewal:        renewal,
		Transfer:       transfer,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
