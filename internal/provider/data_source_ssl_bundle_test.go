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

// fakeSSLAPI serves /ssl/retrieve/{domain} for the domains it was seeded
// with. It is deliberately separate from fakeAPI: that fake is shared with
// every other surface's tests, and SSL needs only this one endpoint.
type fakeSSLAPI struct {
	bundles map[string]map[string]any
}

func newFakeSSLAPI(t *testing.T, bundles map[string]map[string]any) string {
	t.Helper()
	srv := httptest.NewServer(&fakeSSLAPI{bundles: bundles})
	t.Cleanup(srv.Close)
	return srv.URL
}

func (f *fakeSSLAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-API-Key") == "" || r.Header.Get("X-Secret-API-Key") == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_API_KEYS_001", "Missing API credentials.")
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "ssl" || parts[1] != "retrieve" {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "No such endpoint: "+r.URL.Path)
		return
	}

	bundle, ok := f.bundles[parts[2]]
	if !ok {
		writeErr(w, http.StatusBadRequest, "DOMAIN_NOT_FOUND", "Domain is not in the account.")
		return
	}
	body := map[string]any{"status": "SUCCESS"}
	for k, v := range bundle {
		body[k] = v
	}
	writeJSON(w, body)
}

const testCertChain = "-----BEGIN CERTIFICATE-----\nMIIBleaf\n-----END CERTIFICATE-----\n" +
	"-----BEGIN CERTIFICATE-----\nMIIBintermediate\n-----END CERTIFICATE-----\n"

func TestAccSSLBundleDataSource(t *testing.T) {
	url := newFakeSSLAPI(t, map[string]map[string]any{
		"trout.quest": {
			"certificatechain": testCertChain,
			"privatekey":       "-----BEGIN PRIVATE KEY-----\nMIIBsecret\n-----END PRIVATE KEY-----\n",
			"publickey":        "-----BEGIN PUBLIC KEY-----\nMIIBpublic\n-----END PUBLIC KEY-----\n",
		},
	})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_ssl_bundle" "test" {
  domain = "trout.quest"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_ssl_bundle.test",
					tfjsonpath.New("certificate_chain"), knownvalue.StringExact(testCertChain)),
				statecheck.ExpectKnownValue("data.porkbun_ssl_bundle.test",
					tfjsonpath.New("private_key"),
					knownvalue.StringExact("-----BEGIN PRIVATE KEY-----\nMIIBsecret\n-----END PRIVATE KEY-----\n")),
				statecheck.ExpectKnownValue("data.porkbun_ssl_bundle.test",
					tfjsonpath.New("public_key"),
					knownvalue.StringExact("-----BEGIN PUBLIC KEY-----\nMIIBpublic\n-----END PUBLIC KEY-----\n")),
			},
		}},
	})
}

// A domain mid-issuance can carry a null where a PEM belongs. That must read
// as an empty string rather than erroring or leaving the attribute unknown,
// which the framework rejects with "value is not known after apply".
func TestAccSSLBundleDataSourceNullPEMs(t *testing.T) {
	url := newFakeSSLAPI(t, map[string]map[string]any{
		"natwelch.com": {
			"certificatechain": testCertChain,
			"privatekey":       nil,
			"publickey":        nil,
		},
	})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_ssl_bundle" "partial" {
  domain = "natwelch.com"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_ssl_bundle.partial",
					tfjsonpath.New("private_key"), knownvalue.StringExact("")),
				statecheck.ExpectKnownValue("data.porkbun_ssl_bundle.partial",
					tfjsonpath.New("public_key"), knownvalue.StringExact("")),
			},
		}},
	})
}

// Porkbun has no SSL-specific error code, so a domain with no certificate
// surfaces as a plain domain error; the diagnostic must name the domain.
func TestAccSSLBundleDataSourceUnknownDomain(t *testing.T) {
	url := newFakeSSLAPI(t, map[string]map[string]any{})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_ssl_bundle" "missing" {
  domain = "welch.io"
}
`,
			ExpectError: regexp.MustCompile(`Unable to read the SSL bundle for welch\.io`),
		}},
	})
}

// The canonical-domain validator must reject a respelling before any request
// is made, the same way it does for every other domain input.
func TestAccSSLBundleDataSourceRejectsNonCanonicalDomain(t *testing.T) {
	url := newFakeSSLAPI(t, map[string]map[string]any{})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_ssl_bundle" "shouty" {
  domain = "Trout.Quest."
}
`,
			ExpectError: regexp.MustCompile(`Domain is not in canonical form`),
		}},
	})
}
