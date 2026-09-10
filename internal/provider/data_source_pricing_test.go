package provider

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

// fakePricingAPI serves /pricing/get the way the real catalog does, which
// means both coupon shapes: an empty JSON array for the TLDs with no
// promotion (json_encode of an empty PHP associative array) and an object
// for the one that has one. A fake that emitted only one shape would let a
// regression in the other ship green.
type fakePricingAPI struct {
	calls int
}

func newFakePricingAPI(t *testing.T) (*fakePricingAPI, string) {
	t.Helper()
	f := &fakePricingAPI{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

func (f *fakePricingAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch strings.Trim(r.URL.Path, "/") {
	case "ping":
		writeJSON(w, map[string]any{"status": "SUCCESS", "yourIp": "203.0.113.7"})
	case "pricing/get":
		f.calls++
		writeJSON(w, map[string]any{"status": "SUCCESS", "pricing": map[string]any{
			"com": map[string]any{
				"registration": "11.08", "renewal": "11.08", "transfer": "11.08",
				"coupons": []any{},
			},
			// Grouped thousands, the format that breaks a bare tonumber().
			"security": map[string]any{
				"registration": "2,060.25", "renewal": "2,060.25", "transfer": "2,060.25",
				"coupons": []any{},
			},
			// Free transfer, so "0.00" is real data and not a missing entry.
			"co.uk": map[string]any{
				"registration": "4.32", "renewal": "5.66", "transfer": "0.00",
				"coupons": []any{},
			},
			"0z": map[string]any{
				"registration": "42.52", "renewal": "16.77", "transfer": "16.77",
				"specialType": "handshake", "coupons": []any{},
			},
			"quest": map[string]any{
				"registration": "1.10", "renewal": "22.62", "transfer": "22.62",
				"specialType": nil,
				"coupons": map[string]any{"registration": map[string]any{
					"code": "AWESOMENESS", "max_per_user": "1",
					"first_year_only": "yes", "type": "amount", "amount": 1.5,
				}},
			},
		}})
	default:
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "No such endpoint: "+r.URL.Path)
	}
}

func TestAccPricingDataSource(t *testing.T) {
	fake, url := newFakePricingAPI(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_pricing" "all" {}

# Mixed spellings, plus a TLD Porkbun does not sell.
data "porkbun_pricing" "filtered" {
  tlds = [".COM", "Quest", "nosuchtld"]
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_pricing.all",
					tfjsonpath.New("pricing"), knownvalue.MapSizeExact(5)),
				statecheck.ExpectKnownValue("data.porkbun_pricing.all",
					tfjsonpath.New("pricing").AtMapKey("security").AtMapKey("registration"),
					knownvalue.StringExact("2,060.25")),
				statecheck.ExpectKnownValue("data.porkbun_pricing.all",
					tfjsonpath.New("pricing").AtMapKey("co.uk").AtMapKey("transfer"),
					knownvalue.StringExact("0.00")),
				statecheck.ExpectKnownValue("data.porkbun_pricing.all",
					tfjsonpath.New("pricing").AtMapKey("0z").AtMapKey("special_type"),
					knownvalue.StringExact("handshake")),
				statecheck.ExpectKnownValue("data.porkbun_pricing.all",
					tfjsonpath.New("pricing").AtMapKey("com").AtMapKey("special_type"),
					knownvalue.StringExact("")),
				// "coupons": [] must land as an empty map, not null.
				statecheck.ExpectKnownValue("data.porkbun_pricing.all",
					tfjsonpath.New("pricing").AtMapKey("com").AtMapKey("coupons"),
					knownvalue.MapSizeExact(0)),
				statecheck.ExpectKnownValue("data.porkbun_pricing.all",
					tfjsonpath.New("pricing").AtMapKey("quest").AtMapKey("coupons").AtMapKey("registration"),
					knownvalue.ObjectExact(map[string]knownvalue.Check{
						"code":            knownvalue.StringExact("AWESOMENESS"),
						"type":            knownvalue.StringExact("amount"),
						"amount":          knownvalue.StringExact("1.5"),
						"max_per_user":    knownvalue.Int64Exact(1),
						"first_year_only": knownvalue.Bool(true),
					})),

				// The filter folds case and a leading dot, and drops the TLD
				// the catalog has no entry for rather than inventing one.
				statecheck.ExpectKnownValue("data.porkbun_pricing.filtered",
					tfjsonpath.New("pricing"), knownvalue.MapSizeExact(2)),
				statecheck.ExpectKnownValue("data.porkbun_pricing.filtered",
					tfjsonpath.New("pricing").AtMapKey("com").AtMapKey("renewal"),
					knownvalue.StringExact("11.08")),
				statecheck.ExpectKnownValue("data.porkbun_pricing.filtered",
					tfjsonpath.New("pricing").AtMapKey("quest").AtMapKey("registration"),
					knownvalue.StringExact("1.10")),
			},
		}},
	})

	if fake.calls == 0 {
		t.Error("the data source never called /pricing/get")
	}
}

// The warning is not assertable through resource.Test, so the decision it
// rests on is tested directly.
func TestMissingTLDs(t *testing.T) {
	t.Parallel()

	got := missingTLDs(
		[]string{".COM", "nosuchtld", "NoSuchTld", "zzz"},
		map[string]porkbun.TLDPricing{"com": {}},
	)
	want := []string{"nosuchtld", "zzz"}
	if !equalStrings(got, want) {
		t.Errorf("missingTLDs = %v, want %v", got, want)
	}
	if got := missingTLDs(nil, nil); len(got) != 0 {
		t.Errorf("missingTLDs(nil, nil) = %v", got)
	}
}
