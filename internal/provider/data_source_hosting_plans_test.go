package provider

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// hostingPlansFake serves /hosting/plans only. It lives here rather than in
// the shared fake so this surface's response shape — including the awkward
// parts: a null trial, an absent price, an untyped feature bag — is written
// next to the assertions about it.
func newHostingPlansFake(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") == "" || r.Header.Get("X-Secret-API-Key") == "" {
			writeErr(w, http.StatusBadRequest, "INVALID_API_KEYS_001", "Missing API credentials.")
			return
		}
		if r.URL.Path != "/hosting/plans" {
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "No such endpoint: "+r.URL.Path)
			return
		}
		writeJSON(w, map[string]any{
			"status": "SUCCESS",
			"plans": []any{
				map[string]any{
					"product":        "secureStaticHosting",
					"plan":           "monthly",
					"sku":            "PIXIESECURESTATICM2",
					"interval":       "month",
					"price":          300,
					"priceFormatted": "$3.00",
					"trialDays":      15,
					"name":           "Secure Static Hosting Monthly",
					"features":       map[string]any{"bandwidth": "unmetered", "sites": 1, "ssl": true},
				},
				map[string]any{
					"product":        "cloudWordpress",
					"plan":           "monthly",
					"sku":            "CLOUDWORDPRESSM1",
					"interval":       "month",
					"priceFormatted": "",
					// No price and a null trial: neither may reach state as 0.
					"trialDays": nil,
					"name":      "Cloud for WordPress Monthly",
				},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestAccHostingPlansDataSource(t *testing.T) {
	url := newHostingPlansFake(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_hosting_plans" "all" {}

data "porkbun_hosting_plans" "static" {
  sku_prefix = "pixiesecurestatic"
}

data "porkbun_hosting_plans" "wordpress" {
  product = "cloudwordpress"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				// Sorted by SKU, so WordPress leads.
				statecheck.ExpectKnownValue("data.porkbun_hosting_plans.all",
					tfjsonpath.New("skus"), knownvalue.SetExact([]knownvalue.Check{
						knownvalue.StringExact("CLOUDWORDPRESSM1"),
						knownvalue.StringExact("PIXIESECURESTATICM2"),
					})),
				statecheck.ExpectKnownValue("data.porkbun_hosting_plans.all",
					tfjsonpath.New("plans"), knownvalue.ListSizeExact(2)),
				statecheck.ExpectKnownValue("data.porkbun_hosting_plans.all",
					tfjsonpath.New("plans").AtSliceIndex(0).AtMapKey("sku"),
					knownvalue.StringExact("CLOUDWORDPRESSM1")),

				// An absent price and a null trial stay null. Were they
				// zeroed, `price == 0` would read as a free plan.
				statecheck.ExpectKnownValue("data.porkbun_hosting_plans.all",
					tfjsonpath.New("plans").AtSliceIndex(0).AtMapKey("price"), knownvalue.Null()),
				statecheck.ExpectKnownValue("data.porkbun_hosting_plans.all",
					tfjsonpath.New("plans").AtSliceIndex(0).AtMapKey("trial_days"), knownvalue.Null()),
				statecheck.ExpectKnownValue("data.porkbun_hosting_plans.all",
					tfjsonpath.New("plans").AtSliceIndex(0).AtMapKey("features"), knownvalue.Null()),

				// The static plan carries real values for all three.
				statecheck.ExpectKnownValue("data.porkbun_hosting_plans.all",
					tfjsonpath.New("plans").AtSliceIndex(1).AtMapKey("price"), knownvalue.Int64Exact(300)),
				statecheck.ExpectKnownValue("data.porkbun_hosting_plans.all",
					tfjsonpath.New("plans").AtSliceIndex(1).AtMapKey("trial_days"), knownvalue.Int64Exact(15)),
				statecheck.ExpectKnownValue("data.porkbun_hosting_plans.all",
					tfjsonpath.New("plans").AtSliceIndex(1).AtMapKey("price_formatted"),
					knownvalue.StringExact("$3.00")),
				// Mixed-typed feature values all arrive as strings.
				statecheck.ExpectKnownValue("data.porkbun_hosting_plans.all",
					tfjsonpath.New("plans").AtSliceIndex(1).AtMapKey("features"),
					knownvalue.MapExact(map[string]knownvalue.Check{
						"bandwidth": knownvalue.StringExact("unmetered"),
						"sites":     knownvalue.StringExact("1"),
						"ssl":       knownvalue.StringExact("true"),
					})),

				// Both filters are case-insensitive and applied in-provider.
				statecheck.ExpectKnownValue("data.porkbun_hosting_plans.static",
					tfjsonpath.New("skus"), knownvalue.SetExact([]knownvalue.Check{
						knownvalue.StringExact("PIXIESECURESTATICM2"),
					})),
				statecheck.ExpectKnownValue("data.porkbun_hosting_plans.wordpress",
					tfjsonpath.New("skus"), knownvalue.SetExact([]knownvalue.Check{
						knownvalue.StringExact("CLOUDWORDPRESSM1"),
					})),
			},
		}},
	})
}

// A filter matching nothing is a warning and an empty list, never an error:
// the provider filters, so Porkbun cannot reject the value.
func TestAccHostingPlansDataSourceUnmatchedFilter(t *testing.T) {
	url := newHostingPlansFake(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_hosting_plans" "none" {
  sku_prefix = "NOPE"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_hosting_plans.none",
					tfjsonpath.New("plans"), knownvalue.ListSizeExact(0)),
				statecheck.ExpectKnownValue("data.porkbun_hosting_plans.none",
					tfjsonpath.New("skus"), knownvalue.SetSizeExact(0)),
			},
		}},
	})
}
