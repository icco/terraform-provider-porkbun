package porkbun

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// Paging bounds for /marketplace/getAll. The endpoint accepts a limit of up
// to 5000 per call, but the marketplace is a public catalog of tens of
// thousands of listings and a data source re-reads it on every plan, so
// reads are capped rather than exhaustive. 1000 is also the ceiling the API
// itself imposes in filtered mode, which keeps the two modes symmetric.
const (
	marketplaceDefaultMaxResults = 1000
	marketplaceDefaultPageSize   = 1000
	marketplaceMaxPageSize       = 5000
)

// MarketplaceListing is one domain listed for sale on Porkbun's marketplace.
type MarketplaceListing struct {
	Domain     string  `json:"domain"`
	TLD        string  `json:"tld"`
	CreateDate string  `json:"create_date"`
	SLDLength  flexInt `json:"sld_length"`
	// Price is the asking price in USD. The spec types it as a number, but
	// it is decoded through flexString so a decimal string form survives
	// too; flexInt would silently turn 12.99 into 12.
	Price flexString `json:"price"`
}

// PriceUSD parses Price. The bool is false when the API sent nothing or
// something unparseable, which a caller must render as unknown rather than
// as a free domain.
func (l MarketplaceListing) PriceUSD() (float64, bool) {
	s := strings.TrimSpace(string(l.Price))
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// ListMarketplaceListingsOptions filters /marketplace/getAll. A nil pointer
// or empty value means "no filter".
type ListMarketplaceListingsOptions struct {
	// Query matches SLD substrings. Space-separated terms, each optionally
	// prefixed +include or -exclude.
	Query        string
	TLDs         []string
	SLDLengthMin *int64
	SLDLengthMax *int64
	// SortName is domain, tld, price or sld_length; SortDirection is asc or
	// desc. Both only apply in filtered mode.
	SortName      string
	SortDirection string
	// MaxResults caps how many listings are returned across all pages. Zero
	// selects the package default.
	MaxResults int64
	// pageSize is the per-call limit. Unexported so that only this package's
	// tests can shrink it far enough to exercise the paging loop; no real
	// caller has a reason to set it.
	pageSize int64
}

// MarketplaceListings is one read of the marketplace.
type MarketplaceListings struct {
	Listings []MarketplaceListing
	// Filtered is the API's own report of whether it applied server-side
	// filtering.
	Filtered bool
	// Count is the total the API reported for the query, which can exceed
	// len(Listings).
	Count int64
	// Truncated reports that the read stopped on MaxResults rather than on
	// the end of the catalog. Without it a capped read is indistinguishable
	// from an exhaustive one, and "the marketplace has exactly 1000 domains
	// matching" is a conclusion a caller would otherwise draw from a cap.
	Truncated bool
}

// ListMarketplaceListings reads domains listed for sale on the Porkbun
// marketplace, up to opts.MaxResults.
//
// The endpoint has two modes and they page differently. Any of query, tlds,
// sldLengthMin, sldLengthMax or sortName switches it to filtered mode, where
// it answers with up to 1000 server-side matches and ignores start/limit —
// so a filtered read is a single call, because paging one would re-request
// the same page. Unfiltered reads page on start/limit here.
func (c *Client) ListMarketplaceListings(ctx context.Context, opts ListMarketplaceListingsOptions) (*MarketplaceListings, error) {
	base := url.Values{}
	if q := strings.TrimSpace(opts.Query); q != "" {
		base.Set("query", q)
	}
	for _, tld := range opts.TLDs {
		tld = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(tld)), ".")
		if tld != "" {
			base.Add("tlds[]", tld)
		}
	}
	if opts.SLDLengthMin != nil {
		base.Set("sldLengthMin", strconv.FormatInt(*opts.SLDLengthMin, 10))
	}
	if opts.SLDLengthMax != nil {
		base.Set("sldLengthMax", strconv.FormatInt(*opts.SLDLengthMax, 10))
	}
	if name := strings.TrimSpace(opts.SortName); name != "" {
		base.Set("sortName", name)
	}
	// sortDirection is deliberately not part of filterMode below: it is not
	// one of the parameters that trips the API into filtered mode, so on its
	// own it sorts nothing.
	if dir := strings.TrimSpace(opts.SortDirection); dir != "" {
		base.Set("sortDirection", dir)
	}

	filterMode := base.Get("query") != "" || base.Get("sldLengthMin") != "" ||
		base.Get("sldLengthMax") != "" || base.Get("sortName") != "" || len(base["tlds[]"]) > 0

	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = marketplaceDefaultMaxResults
	}
	pageSize := opts.pageSize
	if pageSize <= 0 {
		pageSize = marketplaceDefaultPageSize
	}
	if pageSize > marketplaceMaxPageSize {
		pageSize = marketplaceMaxPageSize
	}

	out := &MarketplaceListings{}
	for start := int64(0); ; {
		want := maxResults - int64(len(out.Listings))
		if want <= 0 {
			// Stopped on the cap rather than on the end of the catalog.
			out.Truncated = true
			break
		}

		q := url.Values{}
		for k, vs := range base {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		if !filterMode {
			if want > pageSize {
				want = pageSize
			}
			q.Set("start", strconv.FormatInt(start, 10))
			q.Set("limit", strconv.FormatInt(want, 10))
		}

		var page struct {
			Count    int                  `json:"count"`
			Filtered bool                 `json:"filtered"`
			Domains  []MarketplaceListing `json:"domains"`
		}
		if err := c.get(ctx, "marketplace/getAll", q, &page); err != nil {
			return nil, err
		}
		out.Filtered = page.Filtered
		out.Count = int64(page.Count)
		out.Listings = append(out.Listings, page.Domains...)

		// A short page is the end. page.Filtered on a request that carried
		// no filters is the other stop: such a response ignored start, so
		// asking for the next page would loop on the same one.
		if filterMode || page.Filtered || int64(len(page.Domains)) < want {
			break
		}
		// Advance by what came back, not by what was asked for: a server
		// that over-answers its own limit would otherwise leave start
		// behind and the next page would repeat listings.
		start += int64(len(page.Domains))
	}

	// Filtered mode has no limit parameter, so it is capped after the fact.
	if int64(len(out.Listings)) > maxResults {
		out.Listings = out.Listings[:maxResults]
		out.Truncated = true
	}
	return out, nil
}
