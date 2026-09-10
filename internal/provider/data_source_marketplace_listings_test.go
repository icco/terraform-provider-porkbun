package provider

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// marketplaceFake serves /marketplace/getAll only, in the shape the real API
// documents: filtered mode ignores start/limit and reports filtered:true,
// unfiltered mode pages. It is deliberately loose about types — price and
// sld_length come back as JSON strings for some listings and numbers for
// others, because the spec says number and Porkbun is not reliably typed. A
// well-typed fake would never run the flexInt/flexString decode paths.
type marketplaceFake struct {
	listings []marketplaceFakeListing
}

type marketplaceFakeListing struct {
	domain string
	tld    string
	price  string
	// stringly emits price and sld_length as JSON strings rather than numbers.
	stringly bool
}

func newMarketplaceFake(t *testing.T, listings ...marketplaceFakeListing) (*marketplaceFake, string) {
	t.Helper()
	f := &marketplaceFake{listings: listings}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

func (f *marketplaceFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-API-Key") == "" || r.Header.Get("X-Secret-API-Key") == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_API_KEYS_001", "Missing API credentials.")
		return
	}

	path := strings.Trim(r.URL.Path, "/")
	if path == "ping" {
		writeJSON(w, map[string]any{"status": "SUCCESS", "yourIp": "203.0.113.7"})
		return
	}
	if path != "marketplace/getAll" {
		writeErr(w, http.StatusBadRequest, "INVALID_REQUEST", "Unexpected path "+path)
		return
	}

	q := r.URL.Query()

	filtered := q.Get("query") != "" || q.Get("sldLengthMin") != "" || q.Get("sldLengthMax") != "" ||
		q.Get("sortName") != "" || len(q["tlds[]"]) > 0

	matched := make([]marketplaceFakeListing, 0, len(f.listings))
	for _, l := range f.listings {
		if term := q.Get("query"); term != "" && !strings.Contains(l.domain, term) {
			continue
		}
		if tlds := q["tlds[]"]; len(tlds) > 0 && !containsString(tlds, l.tld) {
			continue
		}
		matched = append(matched, l)
	}

	// Only unfiltered mode honours start/limit; filtered mode returns the
	// whole match set the way the API does.
	if !filtered {
		start, _ := strconv.Atoi(q.Get("start"))
		limit, _ := strconv.Atoi(q.Get("limit"))
		if start > len(matched) {
			start = len(matched)
		}
		matched = matched[start:]
		if limit > 0 && limit < len(matched) {
			matched = matched[:limit]
		}
	}

	out := make([]map[string]any, 0, len(matched))
	for _, l := range matched {
		sld := strings.TrimSuffix(l.domain, "."+l.tld)
		entry := map[string]any{
			"domain":      l.domain,
			"tld":         l.tld,
			"create_date": "2024-06-01 12:00:00",
		}
		if l.stringly {
			entry["price"] = l.price
			entry["sld_length"] = strconv.Itoa(len(sld))
		} else {
			// A JSON number, as the spec types it. json.Number keeps 12.99
			// out of float formatting.
			entry["price"] = mustNumber(l.price)
			entry["sld_length"] = len(sld)
		}
		out = append(out, entry)
	}
	writeJSON(w, map[string]any{
		"status": "SUCCESS", "count": len(out), "filtered": filtered, "domains": out,
	})
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// mustNumber emits a price as a bare JSON number without going through a
// float, so "12.99" stays "12.99" on the wire.
type jsonNumber string

func (n jsonNumber) MarshalJSON() ([]byte, error) { return []byte(n), nil }

func mustNumber(s string) jsonNumber {
	if s == "" {
		return "null"
	}
	return jsonNumber(s)
}

func TestAccMarketplaceListingsDataSource(t *testing.T) {
	_, url := newMarketplaceFake(t,
		marketplaceFakeListing{domain: "trout.quest", tld: "quest", price: "1200.50"},
		marketplaceFakeListing{domain: "welch.io", tld: "io", price: "88", stringly: true},
		marketplaceFakeListing{domain: "pigs.com", tld: "com", price: "12.99", stringly: true},
		// No price at all: null must not read as free.
		marketplaceFakeListing{domain: "unpriced.com", tld: "com"},
	)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_marketplace_listings" "all" {}

data "porkbun_marketplace_listings" "com_only" {
  tlds           = ["COM", ".io"]
  sort_name      = "price"
  sort_direction = "asc"
}

data "porkbun_marketplace_listings" "capped" {
  max_results = 2
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				// Unfiltered: everything, sorted by name whatever order the
				// catalog answered in.
				statecheck.ExpectKnownValue("data.porkbun_marketplace_listings.all",
					tfjsonpath.New("domains"), knownvalue.SetExact([]knownvalue.Check{
						knownvalue.StringExact("pigs.com"),
						knownvalue.StringExact("trout.quest"),
						knownvalue.StringExact("unpriced.com"),
						knownvalue.StringExact("welch.io"),
					})),
				statecheck.ExpectKnownValue("data.porkbun_marketplace_listings.all",
					tfjsonpath.New("filtered"), knownvalue.Bool(false)),
				statecheck.ExpectKnownValue("data.porkbun_marketplace_listings.all",
					tfjsonpath.New("listings").AtSliceIndex(0).AtMapKey("domain"),
					knownvalue.StringExact("pigs.com")),
				// A decimal price sent as a JSON string still decodes.
				statecheck.ExpectKnownValue("data.porkbun_marketplace_listings.all",
					tfjsonpath.New("listings").AtSliceIndex(0).AtMapKey("price"),
					knownvalue.Float64Exact(12.99)),
				statecheck.ExpectKnownValue("data.porkbun_marketplace_listings.all",
					tfjsonpath.New("listings").AtSliceIndex(0).AtMapKey("sld_length"),
					knownvalue.Int64Exact(4)),
				// A decimal price sent as a JSON number, per the spec.
				statecheck.ExpectKnownValue("data.porkbun_marketplace_listings.all",
					tfjsonpath.New("listings").AtSliceIndex(1).AtMapKey("price"),
					knownvalue.Float64Exact(1200.50)),
				statecheck.ExpectKnownValue("data.porkbun_marketplace_listings.all",
					tfjsonpath.New("listings").AtSliceIndex(2).AtMapKey("price"),
					knownvalue.Null()),

				// Filtered: the tlds filter reached the server, case and
				// leading dot folded away, and the API says so.
				statecheck.ExpectKnownValue("data.porkbun_marketplace_listings.com_only",
					tfjsonpath.New("filtered"), knownvalue.Bool(true)),
				statecheck.ExpectKnownValue("data.porkbun_marketplace_listings.com_only",
					tfjsonpath.New("domains"), knownvalue.SetExact([]knownvalue.Check{
						knownvalue.StringExact("pigs.com"),
						knownvalue.StringExact("unpriced.com"),
						knownvalue.StringExact("welch.io"),
					})),

				statecheck.ExpectKnownValue("data.porkbun_marketplace_listings.capped",
					tfjsonpath.New("listings"), knownvalue.ListSizeExact(2)),
			},
		}},
	})
}

// A direction with no sort_name never reaches the API as a sort, so it is
// rejected at plan time rather than silently returning unsorted results.
func TestAccMarketplaceListingsSortDirectionRequiresSortName(t *testing.T) {
	_, url := newMarketplaceFake(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_marketplace_listings" "bad" {
  sort_direction = "desc"
}
`,
			ExpectError: regexp.MustCompile(`sort_name`),
		}},
	})
}
