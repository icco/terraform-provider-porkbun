package provider

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// newAPISettingsServer serves /account/apiSettings and nothing else, with a
// body given verbatim so a test can pin one exact spelling of Porkbun's
// inconsistent types. It is deliberately separate from the shared fakeAPI:
// this data source has no state to keep, and the only thing worth varying
// is the wire form.
func newAPISettingsServer(t *testing.T, settingsJSON, monthlySpendJSON string) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") == "" || r.Header.Get("X-Secret-API-Key") == "" {
			writeErr(w, http.StatusBadRequest, "INVALID_API_KEYS_001", "Missing API credentials.")
			return
		}
		switch strings.Trim(r.URL.Path, "/") {
		case "ping":
			writeJSON(w, map[string]any{"status": "SUCCESS", "yourIp": "203.0.113.7"})
		case "account/apiSettings":
			if r.Method != http.MethodGet {
				t.Errorf("apiSettings should be read with GET, got %s", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"SUCCESS","settings":` + settingsJSON +
				`,"monthlySpend":` + monthlySpendJSON + `}`))
		default:
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "No such endpoint: "+r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestAccAPISettingsDataSource(t *testing.T) {
	url := newAPISettingsServer(t,
		`{"monthlySpendLimit":50000,"lowBalanceAlert":2500,"autoTopup":true,`+
			`"topupThreshold":1000,"topupAmount":20000}`,
		`3141`)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_api_settings" "current" {}

# The shape of the guard this data source exists for.
output "monthly_headroom_cents" {
  value = data.porkbun_api_settings.current.monthly_spend_limit == null ? -1 : (
    data.porkbun_api_settings.current.monthly_spend_limit -
    data.porkbun_api_settings.current.monthly_spend
  )
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_api_settings.current",
					tfjsonpath.New("monthly_spend_limit"), knownvalue.Int64Exact(50000)),
				statecheck.ExpectKnownValue("data.porkbun_api_settings.current",
					tfjsonpath.New("low_balance_alert"), knownvalue.Int64Exact(2500)),
				statecheck.ExpectKnownValue("data.porkbun_api_settings.current",
					tfjsonpath.New("auto_topup"), knownvalue.Bool(true)),
				statecheck.ExpectKnownValue("data.porkbun_api_settings.current",
					tfjsonpath.New("topup_threshold"), knownvalue.Int64Exact(1000)),
				statecheck.ExpectKnownValue("data.porkbun_api_settings.current",
					tfjsonpath.New("topup_amount"), knownvalue.Int64Exact(20000)),
				statecheck.ExpectKnownValue("data.porkbun_api_settings.current",
					tfjsonpath.New("monthly_spend"), knownvalue.Int64Exact(3141)),
			},
		}},
	})
}

// TestAccAPISettingsDataSourceUnsetLimits covers the two wire forms the live
// mock never serves: 0/1 for autoTopup, and null for a limit that is not
// configured. An unset limit must reach state as null — 0 would read as
// "no API spending permitted", the opposite of "no cap".
func TestAccAPISettingsDataSourceUnsetLimits(t *testing.T) {
	url := newAPISettingsServer(t,
		`{"monthlySpendLimit":null,"lowBalanceAlert":null,"autoTopup":0,"topupAmount":0}`,
		`"0"`)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_api_settings" "current" {}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_api_settings.current",
					tfjsonpath.New("monthly_spend_limit"), knownvalue.Null()),
				statecheck.ExpectKnownValue("data.porkbun_api_settings.current",
					tfjsonpath.New("low_balance_alert"), knownvalue.Null()),
				// Absent from the body entirely, not null.
				statecheck.ExpectKnownValue("data.porkbun_api_settings.current",
					tfjsonpath.New("topup_threshold"), knownvalue.Null()),
				// A real 0 must survive as 0.
				statecheck.ExpectKnownValue("data.porkbun_api_settings.current",
					tfjsonpath.New("topup_amount"), knownvalue.Int64Exact(0)),
				statecheck.ExpectKnownValue("data.porkbun_api_settings.current",
					tfjsonpath.New("auto_topup"), knownvalue.Bool(false)),
				// monthlySpend arrived as the string "0".
				statecheck.ExpectKnownValue("data.porkbun_api_settings.current",
					tfjsonpath.New("monthly_spend"), knownvalue.Int64Exact(0)),
			},
		}},
	})
}

// TestAccAPISettingsDataSourceAPIError proves a failed read surfaces as a
// diagnostic carrying Porkbun's code, not as an empty state object.
func TestAccAPISettingsDataSourceAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Trim(r.URL.Path, "/") == "ping" {
			writeJSON(w, map[string]any{"status": "SUCCESS", "yourIp": "203.0.113.7"})
			return
		}
		writeErr(w, http.StatusBadRequest, "INVALID_API_KEYS_001", "Invalid API key.")
	}))
	defer srv.Close()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(srv.URL) + `
data "porkbun_api_settings" "current" {}
`,
			// apiErrorDiagnostic must attach the credential remediation, not
			// just echo the API's own message.
			ExpectError: regexp.MustCompile(`PORKBUN_SECRET_KEY`),
		}},
	})
}
