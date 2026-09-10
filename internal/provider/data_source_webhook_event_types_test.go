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

// fakeEventTypesAPI serves /webhook/eventTypes only. It lives here rather
// than in the shared fake_api_test.go so this surface's pull request cannot
// conflict with its siblings.
type fakeEventTypesAPI struct {
	mu sync.Mutex

	// body is what the eventTypes field is set to.
	body []string
	// omit drops the eventTypes field entirely, which is the case the data
	// source renders as null rather than as an empty set.
	omit     bool
	apiError bool

	// catalogMethods records the HTTP method of every /webhook/eventTypes
	// request, so a test can prove the read happened and that it was a GET.
	catalogMethods []string
}

// newFakeEventTypesAPI hands back the fake as well as its URL, so a test can
// flip a field mid-run or assert on what was called.
func newFakeEventTypesAPI(t *testing.T, f *fakeEventTypesAPI) (*fakeEventTypesAPI, string) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

func (f *fakeEventTypesAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Mirror the real API's header auth so a provider that regresses to
	// body credentials fails here rather than silently passing.
	if r.Header.Get("X-API-Key") == "" || r.Header.Get("X-Secret-API-Key") == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_API_KEYS_001", "Missing API credentials.")
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	switch strings.Trim(r.URL.Path, "/") {
	case "ping":
		writeJSON(w, map[string]any{"status": "SUCCESS", "yourIp": "203.0.113.7"})
	case "webhook/eventTypes":
		f.catalogMethods = append(f.catalogMethods, r.Method)
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

// assertCatalogReadWithGet proves the data source actually reached the
// endpoint, and reached it read-only. A POST here would carry an
// Idempotency-Key for a call that changes nothing.
func assertCatalogReadWithGet(t *testing.T, f *fakeEventTypesAPI) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.catalogMethods) == 0 {
		t.Error("the data source never called /webhook/eventTypes")
	}
	for _, m := range f.catalogMethods {
		if m != http.MethodGet {
			t.Errorf("/webhook/eventTypes was read with %s, want GET", m)
		}
	}
}

func TestAccWebhookEventTypesDataSource(t *testing.T) {
	// Hostile spelling on purpose: unsorted, padded, duplicated. A data
	// source that passed the raw array straight through would churn state
	// every time Porkbun regrouped the catalog.
	fake, url := newFakeEventTypesAPI(t, &fakeEventTypesAPI{body: []string{
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

	assertCatalogReadWithGet(t, fake)
}

// A response that carries no eventTypes field must reach state as null, not
// as an empty set: an empty set reads as "this account may subscribe to
// nothing", which Porkbun never said.
func TestAccWebhookEventTypesDataSourceAbsentField(t *testing.T) {
	fake, url := newFakeEventTypesAPI(t, &fakeEventTypesAPI{omit: true})

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

	assertCatalogReadWithGet(t, fake)
}

// An empty array is a different claim from an absent field, and must not
// collapse into the null case.
func TestAccWebhookEventTypesDataSourceEmptyCatalog(t *testing.T) {
	fake, url := newFakeEventTypesAPI(t, &fakeEventTypesAPI{body: []string{}})

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

	assertCatalogReadWithGet(t, fake)
}

// The API error path must surface Porkbun's remediation rather than a bare
// decode failure.
func TestAccWebhookEventTypesDataSourceAPIError(t *testing.T) {
	fake, url := newFakeEventTypesAPI(t, &fakeEventTypesAPI{apiError: true})

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

	assertCatalogReadWithGet(t, fake)
}
