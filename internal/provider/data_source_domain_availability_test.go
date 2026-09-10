package provider

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// fakeCheckDomainAPI serves /domain/checkDomain only. It is separate from
// fakeAPI on purpose: this endpoint takes no account state, and the shared
// fake is edited by every branch that adds a surface.
type fakeCheckDomainAPI struct {
	mu sync.Mutex
	// calls counts checks per domain. The endpoint is rate limited to one
	// call per 10 seconds, so a data source that reads twice per plan is a
	// bug worth catching here rather than in production.
	calls map[string]int
}

func newFakeCheckDomainAPI(t *testing.T) (*fakeCheckDomainAPI, string) {
	t.Helper()
	f := &fakeCheckDomainAPI{calls: map[string]int{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

func (f *fakeCheckDomainAPI) callCount(domain string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[domain]
}

func (f *fakeCheckDomainAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-API-Key") == "" || r.Header.Get("X-Secret-API-Key") == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_API_KEYS_001", "Missing API credentials.")
		return
	}

	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(parts) == 3 && parts[0] == "domain" && parts[1] == "checkDomain" {
		f.checkDomain(w, r, parts[2])
		return
	}
	writeErr(w, http.StatusNotFound, "NOT_FOUND", "No such endpoint: "+path)
}

func (f *fakeCheckDomainAPI) checkDomain(w http.ResponseWriter, r *http.Request, domain string) {
	f.calls[domain]++

	// Reject a GET the way the real API does: this endpoint is a POST even
	// though it only reads, and a client that guessed wrong would 404.
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "checkDomain is POST only.")
		return
	}

	switch domain {
	case "trout.quest":
		// Registered, so unavailable — and priced in the hostile spellings
		// the API uses: minDuration as a string, the yes/no flags as 1/0
		// integers. Anything less tolerant reads this as available.
		writeJSON(w, map[string]any{
			"status": "SUCCESS",
			"response": map[string]any{
				"avail": "no",
				"type":  "registration",
				"price": "11.06",
				// The 0/1 integer spelling, sent in both directions on
				// purpose: asserting only the 0 case proves nothing,
				// because a decode that fell through entirely also yields
				// false.
				"firstYearPromo": 1,
				"regularPrice":   "11.06",
				"premium":        0,
				"minDuration":    "1",
				"additional": map[string]any{
					"renewal":  map[string]any{"type": "renewal", "price": "11.06", "regularPrice": "11.06"},
					"transfer": map[string]any{"type": "transfer", "price": "10.09", "regularPrice": "10.09"},
				},
			},
			"limits":       map[string]any{"TTL": 10, "limit": 1, "used": 1},
			"ttlRemaining": 9,
		})

	case "welch-is-available.quest":
		// Available on a promotional first year: price and regularPrice
		// differ, which is the case a caller must not conflate when
		// budgeting a renewal.
		writeJSON(w, map[string]any{
			"status": "SUCCESS",
			"response": map[string]any{
				"avail":          "yes",
				"type":           "registration",
				"price":          "1.11",
				"firstYearPromo": "yes",
				"regularPrice":   "11.06",
				"premium":        "no",
				"minDuration":    1,
				"additional": map[string]any{
					"renewal":  map[string]any{"type": "renewal", "price": "11.06", "regularPrice": "11.06"},
					"transfer": map[string]any{"type": "transfer", "price": "10.09", "regularPrice": "10.09"},
				},
			},
		})

	default:
		writeErr(w, http.StatusBadRequest, "INVALID_DOMAIN", "Invalid domain: "+domain)
	}
}

func TestAccDomainAvailabilityDataSource(t *testing.T) {
	fake, url := newFakeCheckDomainAPI(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_domain_availability" "taken" {
  domain = "trout.quest"
}

data "porkbun_domain_availability" "open" {
  domain = "welch-is-available.quest"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_domain_availability.taken",
					tfjsonpath.New("available"), knownvalue.Bool(false)),
				// The fake answers this one with the 0/1 integer spelling.
				// firstYearPromo is 1 and premium is 0, so the pair proves
				// the integer form decodes in both directions — asserting
				// only false would pass even if the flag were never read.
				statecheck.ExpectKnownValue("data.porkbun_domain_availability.taken",
					tfjsonpath.New("first_year_promo"), knownvalue.Bool(true)),
				statecheck.ExpectKnownValue("data.porkbun_domain_availability.taken",
					tfjsonpath.New("premium"), knownvalue.Bool(false)),
				// minDuration arrives as the string "1" for this domain.
				statecheck.ExpectKnownValue("data.porkbun_domain_availability.taken",
					tfjsonpath.New("min_duration"), knownvalue.Int64Exact(1)),
				statecheck.ExpectKnownValue("data.porkbun_domain_availability.taken",
					tfjsonpath.New("renewal").AtMapKey("price"), knownvalue.StringExact("11.06")),
				statecheck.ExpectKnownValue("data.porkbun_domain_availability.taken",
					tfjsonpath.New("transfer").AtMapKey("regular_price"), knownvalue.StringExact("10.09")),

				statecheck.ExpectKnownValue("data.porkbun_domain_availability.open",
					tfjsonpath.New("available"), knownvalue.Bool(true)),
				statecheck.ExpectKnownValue("data.porkbun_domain_availability.open",
					tfjsonpath.New("first_year_promo"), knownvalue.Bool(true)),
				statecheck.ExpectKnownValue("data.porkbun_domain_availability.open",
					tfjsonpath.New("price"), knownvalue.StringExact("1.11")),
				statecheck.ExpectKnownValue("data.porkbun_domain_availability.open",
					tfjsonpath.New("regular_price"), knownvalue.StringExact("11.06")),
				statecheck.ExpectKnownValue("data.porkbun_domain_availability.open",
					tfjsonpath.New("premium"), knownvalue.Bool(false)),
				statecheck.ExpectKnownValue("data.porkbun_domain_availability.open",
					tfjsonpath.New("price_type"), knownvalue.StringExact("registration")),
			},
		}},
	})

	// Proves the state checked above came from the endpoint and not from a
	// schema default. How many reads one plan-and-apply makes is Terraform's
	// business, so only the floor is asserted.
	if fake.callCount("trout.quest") == 0 {
		t.Error("the data source never called checkDomain")
	}
}

// TestAccDomainAvailabilityDataSourceError proves the INVALID_DOMAIN path
// reaches the operator as a diagnostic rather than as empty state.
func TestAccDomainAvailabilityDataSourceError(t *testing.T) {
	_, url := newFakeCheckDomainAPI(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_domain_availability" "nope" {
  domain = "not-served-by-the-fake.quest"
}
`,
			// apiErrorDiagnostic renders the INVALID_DOMAIN remediation.
			ExpectError: regexp.MustCompile(`INVALID_DOMAIN`),
		}},
	})
}
