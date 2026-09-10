package provider

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

var (
	_ datasource.DataSource              = (*marketplaceListingsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*marketplaceListingsDataSource)(nil)
)

func init() { registerDataSource(NewMarketplaceListingsDataSource) }

// NewMarketplaceListingsDataSource lists domains for sale on the Porkbun
// marketplace.
func NewMarketplaceListingsDataSource() datasource.DataSource {
	return &marketplaceListingsDataSource{}
}

type marketplaceListingsDataSource struct {
	client *porkbun.Client
}

type marketplaceListingsModel struct {
	Query         types.String `tfsdk:"query"`
	TLDs          types.Set    `tfsdk:"tlds"`
	SLDLengthMin  types.Int64  `tfsdk:"sld_length_min"`
	SLDLengthMax  types.Int64  `tfsdk:"sld_length_max"`
	SortName      types.String `tfsdk:"sort_name"`
	SortDirection types.String `tfsdk:"sort_direction"`
	MaxResults    types.Int64  `tfsdk:"max_results"`

	Filtered types.Bool `tfsdk:"filtered"`
	Domains  types.Set  `tfsdk:"domains"`
	Listings types.List `tfsdk:"listings"`
}

type marketplaceListingModel struct {
	Domain     types.String  `tfsdk:"domain"`
	TLD        types.String  `tfsdk:"tld"`
	SLDLength  types.Int64   `tfsdk:"sld_length"`
	Price      types.Float64 `tfsdk:"price"`
	CreateDate types.String  `tfsdk:"create_date"`
}

func marketplaceListingAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"domain":      types.StringType,
		"tld":         types.StringType,
		"sld_length":  types.Int64Type,
		"price":       types.Float64Type,
		"create_date": types.StringType,
	}
}

func marketplaceListingToModel(l porkbun.MarketplaceListing) marketplaceListingModel {
	price := types.Float64Null()
	if p, ok := l.PriceUSD(); ok {
		price = types.Float64Value(p)
	}
	return marketplaceListingModel{
		Domain:     types.StringValue(l.Domain),
		TLD:        types.StringValue(l.TLD),
		SLDLength:  types.Int64Value(l.SLDLength.Int64()),
		Price:      price,
		CreateDate: types.StringValue(l.CreateDate),
	}
}

func (d *marketplaceListingsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_marketplace_listings"
}

func (d *marketplaceListingsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists domains for sale on the [Porkbun marketplace](https://porkbun.com/market), " +
			"optionally filtered.\n\nThe marketplace is a public catalog of tens of thousands of listings and this " +
			"is re-read on every plan, so results are capped at `max_results` (1000 by default) rather than " +
			"fetched exhaustively.\n\nThe endpoint has two modes. Setting any of `query`, `tlds`, `sld_length_min`, " +
			"`sld_length_max` or `sort_name` filters and sorts server-side, and returns at most 1000 matches; with " +
			"none of them set the raw catalog is paged through instead. The `filtered` attribute reports which mode " +
			"the API used. Nothing about a listing is stable — a domain can sell between plan and apply — so treat " +
			"this as an input to a report or an alert, not as a source of identifiers.",
		Attributes: map[string]schema.Attribute{
			"query": schema.StringAttribute{
				MarkdownDescription: "Substring search against the SLD (the part before the TLD). Space-separated " +
					"terms are combined; prefix a term with `-` to exclude it, e.g. `ai -test`.",
				Optional: true,
			},
			"tlds": schema.SetAttribute{
				MarkdownDescription: "Limit results to these top-level domains, e.g. `com`. Case and a leading dot " +
					"are ignored.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"sld_length_min": schema.Int64Attribute{
				MarkdownDescription: "Only list domains whose SLD is at least this many characters.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.AtLeast(1)},
			},
			"sld_length_max": schema.Int64Attribute{
				MarkdownDescription: "Only list domains whose SLD is at most this many characters.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.AtLeast(1)},
			},
			"sort_name": schema.StringAttribute{
				MarkdownDescription: "Field the API sorts the filtered results by: `domain`, `tld`, `price` or " +
					"`sld_length`. Porkbun's own default is `sld_length` ascending when `query` is set and " +
					"`create_date` descending otherwise. `listings` is re-sorted by domain name regardless, so this " +
					"only decides which listings survive the 1000-result ceiling.",
				Optional:   true,
				Validators: []validator.String{stringvalidator.OneOf("domain", "tld", "price", "sld_length")},
			},
			"sort_direction": schema.StringAttribute{
				MarkdownDescription: "Sort direction, `asc` or `desc`. Requires `sort_name`: direction is not one " +
					"of the parameters that puts the API into filtered mode, so on its own it sorts nothing.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("asc", "desc"),
					stringvalidator.AlsoRequires(path.MatchRoot("sort_name")),
				},
			},
			"max_results": schema.Int64Attribute{
				MarkdownDescription: "Maximum number of listings to return, across as many API calls as it takes. " +
					"Defaults to `1000`, which is also the ceiling the API imposes on a filtered read. Results past " +
					"the cap are dropped silently, so raise it deliberately rather than assuming a short list means " +
					"a small marketplace.",
				Optional:   true,
				Validators: []validator.Int64{int64validator.AtLeast(1)},
			},
			"filtered": schema.BoolAttribute{
				MarkdownDescription: "Whether the API applied server-side filtering, as it reports it. False means " +
					"the response is a raw page of the catalog and `sort_name`/`sort_direction` had no effect.",
				Computed: true,
			},
			"domains": schema.SetAttribute{
				MarkdownDescription: "The matching domain names, for set arithmetic against domains you already own.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"listings": schema.ListNestedAttribute{
				MarkdownDescription: "Each matching listing, sorted by domain name.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"domain": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "The domain being sold.",
						},
						"tld": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Its top-level domain, without a leading dot.",
						},
						"sld_length": schema.Int64Attribute{
							Computed:            true,
							MarkdownDescription: "Character length of the SLD, the part of the domain before the TLD.",
						},
						"price": schema.Float64Attribute{
							Computed: true,
							MarkdownDescription: "Asking price in USD. Null when Porkbun sent no usable price, " +
								"which is not the same as free — the registration fee is charged separately either way.",
						},
						"create_date": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "When the listing was created, as Porkbun reports it.",
						},
					},
				},
			},
		},
	}
}

func (d *marketplaceListingsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(ctx, req, resp)
}

func (d *marketplaceListingsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config marketplaceListingsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := porkbun.ListMarketplaceListingsOptions{
		Query:         config.Query.ValueString(),
		SortName:      config.SortName.ValueString(),
		SortDirection: config.SortDirection.ValueString(),
		MaxResults:    config.MaxResults.ValueInt64(),
	}
	if !config.SLDLengthMin.IsNull() {
		v := config.SLDLengthMin.ValueInt64()
		opts.SLDLengthMin = &v
	}
	if !config.SLDLengthMax.IsNull() {
		v := config.SLDLengthMax.ValueInt64()
		opts.SLDLengthMax = &v
	}
	if !config.TLDs.IsNull() {
		resp.Diagnostics.Append(config.TLDs.ElementsAs(ctx, &opts.TLDs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	res, err := d.client.ListMarketplaceListings(ctx, opts)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Unable to list Porkbun marketplace domains", err))
		return
	}

	// Sorted by name, not by whatever order the API answered in: the
	// catalog's own order shifts as listings are added and sold, and an
	// unstable list attribute is a diff on every plan for anything
	// referencing it by index.
	listings := res.Listings
	sort.Slice(listings, func(i, j int) bool { return listings[i].Domain < listings[j].Domain })

	names := make([]string, 0, len(listings))
	details := make([]marketplaceListingModel, 0, len(listings))
	for _, l := range listings {
		names = append(names, l.Domain)
		details = append(details, marketplaceListingToModel(l))
	}

	nameSet, diags := types.SetValueFrom(ctx, types.StringType, names)
	resp.Diagnostics.Append(diags...)

	detailList, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: marketplaceListingAttributeTypes()}, details)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	config.Filtered = types.BoolValue(res.Filtered)
	config.Domains = nameSet
	config.Listings = detailList
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
