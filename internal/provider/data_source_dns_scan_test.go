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

// scanAPI is a stand-in for /dns/scan only, kept in this file so the shared
// fake stays untouched.
//
// It answers in the shapes the endpoint is documented to allow rather than
// the tidiest one: a fully-qualified name alongside `@` and a bare
// subdomain, a ttl as a string on one record and an integer on another, and
// a null prio everywhere except the MX. A provider that normalizes none of
// that produces a records list that cannot be compared with
// porkbun_dns_record.
type scanAPI struct {
	mu sync.Mutex
	// status, when non-zero, makes the next scan fail with that HTTP status
	// and code instead of answering.
	status int
	code   string
	calls  int
}

func newScanAPI(t *testing.T) (*scanAPI, string) {
	t.Helper()
	f := &scanAPI{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

func (f *scanAPI) failWith(status int, code string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.code = status, code
}

func (f *scanAPI) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *scanAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-API-Key") == "" || r.Header.Get("X-Secret-API-Key") == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_API_KEYS_001", "Missing API credentials.")
		return
	}

	path := strings.Trim(r.URL.Path, "/")
	if path == "ping" {
		writeJSON(w, map[string]any{"status": "SUCCESS", "yourIp": "203.0.113.7"})
		return
	}

	domain, ok := strings.CutPrefix(path, "dns/scan/")
	if !ok {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "No such endpoint: "+path)
		return
	}

	f.mu.Lock()
	f.calls++
	status, code := f.status, f.code
	f.mu.Unlock()

	if status != 0 {
		writeErr(w, status, code, "The scan could not be run.")
		return
	}
	if domain != "trout.quest" {
		writeErr(w, http.StatusBadRequest, "INVALID_DOMAIN", "Invalid domain.")
		return
	}

	writeJSON(w, map[string]any{
		"status":      "SUCCESS",
		"domain":      domain,
		"recordCount": 4,
		"records": []any{
			// Fully qualified, ttl as a string, no priority.
			map[string]any{
				"name": "www." + domain, "type": "A", "content": "203.0.113.10",
				"ttl": "600", "prio": nil,
			},
			// The apex as "@", and prio omitted entirely rather than null.
			map[string]any{
				"name": "@", "type": "TXT", "content": "v=spf1 include:example.net -all",
				"ttl": 300,
			},
			// The non-zero flag case: a real MX priority must survive.
			map[string]any{
				"name": "", "type": "MX", "content": "mail.example.net",
				"ttl": 3600, "prio": 10,
			},
			// A wildcard, which the scan consolidates rather than expands.
			map[string]any{
				"name": "*", "type": "A", "content": "203.0.113.11",
				"ttl": "600", "prio": nil,
			},
		},
	})
}

func TestAccDNSScanDataSource(t *testing.T) {
	_, url := newScanAPI(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_dns_scan" "test" {
  domain = "trout.quest"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_dns_scan.test",
					tfjsonpath.New("record_count"), knownvalue.Int64Exact(4)),

				// Sorted by name, type, content: apex MX, apex TXT, then
				// "*", then "www". Every name is in the subdomain form
				// porkbun_dns_record uses, whichever spelling arrived.
				statecheck.ExpectKnownValue("data.porkbun_dns_scan.test",
					tfjsonpath.New("records"), knownvalue.ListSizeExact(4)),
				statecheck.ExpectKnownValue("data.porkbun_dns_scan.test",
					tfjsonpath.New("records").AtSliceIndex(0), knownvalue.ObjectExact(map[string]knownvalue.Check{
						"name":    knownvalue.StringExact(""),
						"type":    knownvalue.StringExact("MX"),
						"content": knownvalue.StringExact("mail.example.net"),
						"ttl":     knownvalue.Int64Exact(3600),
						// The non-zero case: a priority that survived decode.
						"prio": knownvalue.Int64Exact(10),
					})),
				statecheck.ExpectKnownValue("data.porkbun_dns_scan.test",
					tfjsonpath.New("records").AtSliceIndex(1), knownvalue.ObjectExact(map[string]knownvalue.Check{
						"name":    knownvalue.StringExact(""),
						"type":    knownvalue.StringExact("TXT"),
						"content": knownvalue.StringExact("v=spf1 include:example.net -all"),
						"ttl":     knownvalue.Int64Exact(300),
						// prio was absent, not zero. A 0 here would be a
						// priority the nameserver never answered with.
						"prio": knownvalue.Null(),
					})),
				statecheck.ExpectKnownValue("data.porkbun_dns_scan.test",
					tfjsonpath.New("records").AtSliceIndex(2).AtMapKey("name"),
					knownvalue.StringExact("*")),
				statecheck.ExpectKnownValue("data.porkbun_dns_scan.test",
					tfjsonpath.New("records").AtSliceIndex(3), knownvalue.ObjectExact(map[string]knownvalue.Check{
						"name":    knownvalue.StringExact("www"),
						"type":    knownvalue.StringExact("A"),
						"content": knownvalue.StringExact("203.0.113.10"),
						// The string form decoded to a real number.
						"ttl":  knownvalue.Int64Exact(600),
						"prio": knownvalue.Null(),
					})),
			},
		}},
	})
}

// A rate limit on /dns/scan is the failure a real migration hits, because
// the endpoint has its own 20/hour budget that the rest of the API's
// remediation text does not mention.
func TestAccDNSScanDataSourceRateLimited(t *testing.T) {
	fake, url := newScanAPI(t)
	fake.failWith(http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_dns_scan" "test" {
  domain = "trout.quest"
}
`,
			ExpectError: regexp.MustCompile(`20\s+calls\s+per\s+hour`),
		}},
	})

	if fake.callCount() == 0 {
		t.Error("the data source never called /dns/scan")
	}
}

// A domain the key cannot see is the other common failure, and it must
// carry the shared INVALID_DOMAIN remediation rather than a bare API error.
func TestAccDNSScanDataSourceUnknownDomain(t *testing.T) {
	_, url := newScanAPI(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_dns_scan" "test" {
  domain = "not-in-this-account.example"
}
`,
			ExpectError: regexp.MustCompile(`porkbun_domains`),
		}},
	})
}

// The canonical spelling is enforced before any call is made: an uppercase
// or dotted domain is a config error, not a scan that burns rate budget.
func TestAccDNSScanDataSourceRejectsNonCanonicalDomain(t *testing.T) {
	fake, url := newScanAPI(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_dns_scan" "test" {
  domain = "Trout.Quest."
}
`,
			ExpectError: regexp.MustCompile(`Domain is not in canonical form`),
		}},
	})

	if n := fake.callCount(); n != 0 {
		t.Errorf("validation should reject the domain before calling the API, got %d calls", n)
	}
}
