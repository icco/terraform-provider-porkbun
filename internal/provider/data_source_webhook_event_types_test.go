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

// fakeEventTypesAPI serves /webhook/eventTypes only. It lives here rather
// than in the shared fake_api_test.go so this surface's pull request cannot
// conflict with its siblings.
type fakeEventTypesAPI struct {
	// body is what the eventTypes field is set to. A nil body omits the
	// field entirely, which is the case the data source renders as null.
	body     []string
	omit     bool
	apiError bool
}

func newFakeEventTypesAPI(t *testing.T, f *fakeEventTypesAPI) string {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return srv.URL
}

func (f *fakeEventTypesAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Mirror the real API's header auth so a provider that regresses to
	// body credentials fails here rather than silently passing.
	if r.Header.Get("X-API-Key") == "" || r.Header.Get("X-Secret-API-Key") == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_API_KEYS_001", "Missing API credentials.")
		return
	}

	switch strings.Trim(r.URL.Path, "/") {
	case "ping":
		writeJSON(w, map[string]any{"status": "SUCCESS", "yourIp": "203.0.113.7"})
	case "webhook/eventTypes":
		switch {
		case f.apiError:
			writeErr(w, http.StatusBadRequest, "RATE_LIMIT_EXCEEDED", "Too many requests.")
		case f.omit:
			// Porkbun answering SUCCESS with no eventTypes field at all.
			writeJSON(w, map[string]any{"status": "SUCCESS"})
		default:
			writeJSON(w, map[string]any{"status": "SUCCESS", "eventTypes": f.body})
		}
	default:
		writeErr(w, http.StatusBadRequest, "NOT_FOUND", "Unhandled path "+r.URL.Path)
	}
}

func TestAccWebhookEventTypesDataSource(t *testing.T) {
	// Hostile spelling on purpose: unsorted, padded, duplicated. A data
	// source that passed the raw array straight through would churn state
	// every time Porkbun regrouped the catalog.
	url := newFakeEventTypesAPI(t, &fakeEventTypesAPI{body: []string{
		"dns.record.updated",
		"  domain.registered  ",
		"cloudflare.connect.completed",
		"dns.record.updated",
		"",
		"domain.expiring",
	}})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_webhook_event_types" "all" {}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_webhook_event_types.all",
					tfjsonpath.New("event_types"), knownvalue.SetExact([]knownvalue.Check{
						knownvalue.StringExact("cloudflare.connect.completed"),
						knownvalue.StringExact("dns.record.updated"),
						knownvalue.StringExact("domain.expiring"),
						knownvalue.StringExact("domain.registered"),
					})),
			},
		}},
	})
}

// A response that carries no eventTypes field must reach state as null, not
// as an empty set: an empty set reads as "this account may subscribe to
// nothing", which Porkbun never said.
func TestAccWebhookEventTypesDataSourceAbsentField(t *testing.T) {
	url := newFakeEventTypesAPI(t, &fakeEventTypesAPI{omit: true})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_webhook_event_types" "absent" {}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_webhook_event_types.absent",
					tfjsonpath.New("event_types"), knownvalue.Null()),
			},
		}},
	})
}

// An empty array is a different claim from an absent field, and must not
// collapse into the null case.
func TestAccWebhookEventTypesDataSourceEmptyCatalog(t *testing.T) {
	url := newFakeEventTypesAPI(t, &fakeEventTypesAPI{body: []string{}})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_webhook_event_types" "empty" {}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_webhook_event_types.empty",
					tfjsonpath.New("event_types"), knownvalue.SetSizeExact(0)),
			},
		}},
	})
}

// The API error path must surface Porkbun's remediation rather than a bare
// decode failure.
func TestAccWebhookEventTypesDataSourceAPIError(t *testing.T) {
	url := newFakeEventTypesAPI(t, &fakeEventTypesAPI{apiError: true})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_webhook_event_types" "boom" {}
`,
			ExpectError: regexp.MustCompile(`RATE_LIMIT_EXCEEDED`),
		}},
	})
}
