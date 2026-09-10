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

// balanceFake is this data source's own stand-in for /account/balance,
// separate from the shared fakeAPI so that each API surface's tests stay in
// one file. It is stateless: a balance is a read-only account attribute, so
// there is no lifecycle to model.
type balanceFake struct {
	// balance is written into the response as-is, so a test can serve the
	// integer the spec documents or the string form Porkbun uses elsewhere.
	balance any
	display string
	// errCode, when set, makes every call fail with it.
	errCode string
}

func newBalanceFake(t *testing.T, f balanceFake) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") == "" || r.Header.Get("X-Secret-API-Key") == "" {
			writeErr(w, http.StatusBadRequest, "INVALID_API_KEYS_001", "Missing API credentials.")
			return
		}
		if f.errCode != "" {
			// Porkbun answers HTTP 200 on some failures, so error responses
			// here do too: it is the body's status the client must branch on.
			writeErr(w, http.StatusOK, f.errCode, "Balance lookup refused.")
			return
		}
		if path := strings.Trim(r.URL.Path, "/"); path != "account/balance" {
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "No such endpoint: "+path)
			return
		}
		writeJSON(w, map[string]any{"status": "SUCCESS", "balance": f.balance, "display": f.display})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestAccAccountBalanceDataSource(t *testing.T) {
	url := newBalanceFake(t, balanceFake{balance: 1234, display: "$12.34"})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_account_balance" "current" {}

# The intended use: refuse to plan a batch of registrations the account
# cannot pay for. Cents throughout, so no float rounding decides it.
check "sufficient_credit" {
  assert {
    condition     = data.porkbun_account_balance.current.balance_cents >= 1000
    error_message = "not enough Porkbun credit"
  }
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_account_balance.current",
					tfjsonpath.New("balance_cents"), knownvalue.Int64Exact(1234)),
				statecheck.ExpectKnownValue("data.porkbun_account_balance.current",
					tfjsonpath.New("display"), knownvalue.StringExact("$12.34")),
			},
		}},
	})
}

// The spec types `balance` as an integer, but Porkbun sends integers as
// decimal strings on other endpoints. A string that failed to decode would
// break every plan reading this data source, so it is exercised end to end
// and not only at the client boundary.
func TestAccAccountBalanceDataSourceStringAmount(t *testing.T) {
	url := newBalanceFake(t, balanceFake{balance: "50000", display: "$500.00"})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_account_balance" "current" {}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_account_balance.current",
					tfjsonpath.New("balance_cents"), knownvalue.Int64Exact(50000)),
			},
		}},
	})
}

// A zero balance must read as zero credit rather than as an absent value:
// this is the state a fresh account is in, and it is exactly the case a
// spend guard has to catch.
func TestAccAccountBalanceDataSourceZero(t *testing.T) {
	url := newBalanceFake(t, balanceFake{balance: 0, display: "$0.00"})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_account_balance" "current" {}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_account_balance.current",
					tfjsonpath.New("balance_cents"), knownvalue.Int64Exact(0)),
				statecheck.ExpectKnownValue("data.porkbun_account_balance.current",
					tfjsonpath.New("display"), knownvalue.StringExact("$0.00")),
			},
		}},
	})
}

// The account endpoints are the ones a misconfigured key hits first, so the
// credential remediation has to reach the user from here.
func TestAccAccountBalanceDataSourceCredentialError(t *testing.T) {
	url := newBalanceFake(t, balanceFake{errCode: "INVALID_API_KEYS_002"})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_account_balance" "current" {}
`,
			ExpectError: regexp.MustCompile(`secretapikey`),
		}},
	})
}
