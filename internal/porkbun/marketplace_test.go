package porkbun

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMockMarketplaceListings is the tier-1 decode test: it pins the shape
// the live API actually serves for /marketplace/getAll.
//
// The mock ignores every filter parameter and reports filtered:true even for
// a request that carried none, so nothing about the filtering contract can
// be asserted here — only that the listing objects decode.
func TestMockMarketplaceListings(t *testing.T) {
	c := mockClient(t)

	res, err := c.ListMarketplaceListings(context.Background(), ListMarketplaceListingsOptions{})
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("ListMarketplaceListings: %v", err)
	}
	if len(res.Listings) == 0 {
		t.Fatal("expected at least one listing from the mock")
	}
	for _, l := range res.Listings {
		if l.Domain == "" || l.TLD == "" {
			t.Errorf("listing did not decode: %+v", l)
		}
	}
}

// TestListMarketplaceListingsPages covers the unfiltered branch: start
// advances by the requested limit, and a short page ends the read.
func TestListMarketplaceListingsPages(t *testing.T) {
	t.Parallel()

	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		// Five listings across pages of two. Price and sld_length alternate
		// between JSON strings and numbers, because the spec says number
		// and Porkbun is not reliably typed.
		pages := [][]string{
			{`{"domain":"a.com","tld":"com","sld_length":1,"price":"12.99","create_date":"2024-01-01"}`,
				`{"domain":"b.com","tld":"com","sld_length":"1","price":8,"create_date":"2024-01-02"}`},
			{`{"domain":"c.com","tld":"com","sld_length":1,"price":null,"create_date":"2024-01-03"}`,
				`{"domain":"d.io","tld":"io","sld_length":"1","price":"1500","create_date":"2024-01-04"}`},
			{`{"domain":"e.io","tld":"io","sld_length":1,"price":0,"create_date":"2024-01-05"}`},
		}
		idx := len(queries) - 1
		var items []string
		if idx < len(pages) {
			items = pages[idx]
		}
		_, _ = fmt.Fprintf(w, `{"status":"SUCCESS","count":%d,"filtered":false,"domains":[%s]}`,
			len(items), strings.Join(items, ","))
	}))
	defer srv.Close()

	res, err := testClient(t, srv.URL).ListMarketplaceListings(context.Background(),
		ListMarketplaceListingsOptions{pageSize: 2})
	if err != nil {
		t.Fatalf("ListMarketplaceListings: %v", err)
	}
	if len(res.Listings) != 5 {
		t.Fatalf("got %d listings, want 5", len(res.Listings))
	}
	if len(queries) != 3 {
		t.Fatalf("got %d requests, want 3: %v", len(queries), queries)
	}
	for i, want := range []string{"start=0&limit=2", "start=2&limit=2", "start=4&limit=2"} {
		for _, part := range strings.Split(want, "&") {
			if !strings.Contains(queries[i], part) {
				t.Errorf("request %d query %q missing %q", i, queries[i], part)
			}
		}
	}

	if p, ok := res.Listings[0].PriceUSD(); !ok || p != 12.99 {
		t.Errorf("decimal price from a JSON string: got %v ok=%v", p, ok)
	}
	if p, ok := res.Listings[1].PriceUSD(); !ok || p != 8 {
		t.Errorf("price from a JSON number: got %v ok=%v", p, ok)
	}
	if _, ok := res.Listings[2].PriceUSD(); ok {
		t.Error("a null price must not read as 0")
	}
	if res.Listings[1].SLDLength.Int64() != 1 {
		t.Errorf("sld_length from a JSON string: %+v", res.Listings[1])
	}
}

// TestListMarketplaceListingsFilteredIsSingleCall covers the other branch:
// filtered mode ignores start/limit, so sending them and paging on them
// would re-request the same page forever.
func TestListMarketplaceListingsFilteredIsSingleCall(t *testing.T) {
	t.Parallel()

	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"status":"SUCCESS","count":2,"filtered":true,"domains":[` +
			`{"domain":"ai.com","tld":"com","sld_length":2,"price":"9999.00"},` +
			`{"domain":"ai.io","tld":"io","sld_length":2,"price":"499.50"}]}`))
	}))
	defer srv.Close()

	minLen := int64(2)
	res, err := testClient(t, srv.URL).ListMarketplaceListings(context.Background(),
		ListMarketplaceListingsOptions{
			Query:         "ai -test",
			TLDs:          []string{".COM", "io"},
			SLDLengthMin:  &minLen,
			SortName:      "price",
			SortDirection: "asc",
			pageSize:      1,
		})
	if err != nil {
		t.Fatalf("ListMarketplaceListings: %v", err)
	}
	if !res.Filtered {
		t.Error("Filtered should mirror the API's own report")
	}
	if len(queries) != 1 {
		t.Fatalf("a filtered read must be one call, got %d: %v", len(queries), queries)
	}
	for _, want := range []string{"query=ai+-test", "tlds%5B%5D=com", "tlds%5B%5D=io",
		"sldLengthMin=2", "sortName=price", "sortDirection=asc"} {
		if !strings.Contains(queries[0], want) {
			t.Errorf("query %q missing %q", queries[0], want)
		}
	}
	for _, unwanted := range []string{"start=", "limit="} {
		if strings.Contains(queries[0], unwanted) {
			t.Errorf("query %q must not carry %q in filtered mode", queries[0], unwanted)
		}
	}
}

// TestListMarketplaceListingsRespectsMaxResults proves the cap bounds both
// the number of calls and the result, including in filtered mode where the
// API takes no limit parameter.
func TestListMarketplaceListingsRespectsMaxResults(t *testing.T) {
	t.Parallel()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"status":"SUCCESS","count":2,"filtered":false,"domains":[` +
			`{"domain":"a.com","tld":"com","price":1},{"domain":"b.com","tld":"com","price":2}]}`))
	}))
	defer srv.Close()

	res, err := testClient(t, srv.URL).ListMarketplaceListings(context.Background(),
		ListMarketplaceListingsOptions{MaxResults: 3, pageSize: 2})
	if err != nil {
		t.Fatalf("ListMarketplaceListings: %v", err)
	}
	if len(res.Listings) != 3 {
		t.Errorf("got %d listings, want the cap of 3", len(res.Listings))
	}
	if calls != 2 {
		t.Errorf("got %d calls, want 2 to fill a cap of 3 at page size 2", calls)
	}
}

// A read that stops on the cap must say so. Without Truncated a caller
// cannot tell a capped read from an exhaustive one, and "the marketplace
// has exactly max_results matches" is the wrong conclusion it would
// otherwise draw.
func TestListMarketplaceListingsReportsTruncation(t *testing.T) {
	t.Parallel()

	// An endless catalog: every page is full, so only the cap stops it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"SUCCESS","count":9999,"filtered":false,"domains":[` +
			`{"domain":"a.com","tld":"com","price":1},{"domain":"b.com","tld":"com","price":2}]}`))
	}))
	defer srv.Close()

	res, err := testClient(t, srv.URL).ListMarketplaceListings(context.Background(),
		ListMarketplaceListingsOptions{MaxResults: 4, pageSize: 2})
	if err != nil {
		t.Fatalf("ListMarketplaceListings: %v", err)
	}
	if !res.Truncated {
		t.Error("Truncated is false after stopping on the cap")
	}
	if res.Count != 9999 {
		t.Errorf("Count = %d, want the 9999 the API reported", res.Count)
	}
	if len(res.Listings) != 4 {
		t.Errorf("got %d listings, want the cap of 4", len(res.Listings))
	}
}

// The complement: a catalog that ends before the cap is not truncated.
// Without this, Truncated could be hardcoded true and the test above would
// still pass.
func TestListMarketplaceListingsNotTruncatedWhenCatalogEnds(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"SUCCESS","count":1,"filtered":false,"domains":[` +
			`{"domain":"only.com","tld":"com","price":1}]}`))
	}))
	defer srv.Close()

	res, err := testClient(t, srv.URL).ListMarketplaceListings(context.Background(),
		ListMarketplaceListingsOptions{MaxResults: 50, pageSize: 10})
	if err != nil {
		t.Fatalf("ListMarketplaceListings: %v", err)
	}
	if res.Truncated {
		t.Error("Truncated is true for a catalog that ended on its own")
	}
	if len(res.Listings) != 1 {
		t.Errorf("got %d listings, want 1", len(res.Listings))
	}
}
