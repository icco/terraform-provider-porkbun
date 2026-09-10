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

// ipFake serves /ip and /ping the way the live API does, which the shared
// fakeAPI cannot: it gates every path on credentials, and the whole point of
// /ip is that it does not.
type ipFake struct {
	// rejectPing makes /ping answer the way it does for a key that is
	// invalid or blocked by its own IP allowlist. /ip keeps working, which
	// is what makes validate_credentials = false an escape hatch rather
	// than decoration.
	rejectPing bool
}

func (f *ipFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch strings.Trim(r.URL.Path, "/") {
	case "ip":
		// Verified against the live API: /ip answers SUCCESS even for a
		// bogus key, and never sends credentialsValid or, here,
		// xForwardedFor — both must reach state as null.
		writeJSON(w, map[string]any{"status": "SUCCESS", "yourIp": "198.51.100.9"})

	case "ping":
		if f.rejectPing {
			writeErr(w, http.StatusBadRequest, "INVALID_API_KEYS_001", "Invalid API key.")
			return
		}
		if r.Header.Get("X-API-Key") == "" || r.Header.Get("X-Secret-API-Key") == "" {
			// No credentials: an IP, but nothing said about a key.
			writeJSON(w, map[string]any{"status": "SUCCESS", "yourIp": "198.51.100.9"})
			return
		}
		writeJSON(w, map[string]any{
			"status":           "SUCCESS",
			"yourIp":           "198.51.100.9",
			"xForwardedFor":    "198.51.100.9, 10.0.0.1",
			"credentialsValid": true,
		})

	default:
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "No such endpoint: "+r.URL.Path)
	}
}

func newIPFake(t *testing.T, rejectPing bool) string {
	t.Helper()
	srv := httptest.NewServer(&ipFake{rejectPing: rejectPing})
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestAccIPDataSource(t *testing.T) {
	url := newIPFake(t, false)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_ip" "checked" {}

data "porkbun_ip" "anonymous" {
  validate_credentials = false
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_ip.checked",
					tfjsonpath.New("ip"), knownvalue.StringExact("198.51.100.9")),
				// True, not false: a decode that never read the field would
				// pass a false assertion.
				statecheck.ExpectKnownValue("data.porkbun_ip.checked",
					tfjsonpath.New("credentials_valid"), knownvalue.Bool(true)),
				statecheck.ExpectKnownValue("data.porkbun_ip.checked",
					tfjsonpath.New("x_forwarded_for"), knownvalue.StringExact("198.51.100.9, 10.0.0.1")),

				statecheck.ExpectKnownValue("data.porkbun_ip.anonymous",
					tfjsonpath.New("ip"), knownvalue.StringExact("198.51.100.9")),
				// /ip says nothing about the key, so this must stay null.
				// false would claim Porkbun rejected it.
				statecheck.ExpectKnownValue("data.porkbun_ip.anonymous",
					tfjsonpath.New("credentials_valid"), knownvalue.Null()),
				// Absent, so null rather than "".
				statecheck.ExpectKnownValue("data.porkbun_ip.anonymous",
					tfjsonpath.New("x_forwarded_for"), knownvalue.Null()),
			},
		}},
	})
}

// TestAccIPDataSourceRejectedCredentials is the reason validate_credentials
// exists: when the key is refused, /ping cannot tell you the address, and
// the address is what you need to fix the key.
func TestAccIPDataSourceRejectedCredentials(t *testing.T) {
	url := newIPFake(t, true)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(url) + `
data "porkbun_ip" "checked" {}
`,
				ExpectError: regexp.MustCompile(`Invalid API key`),
			},
			{
				Config: providerConfig(url) + `
data "porkbun_ip" "anonymous" {
  validate_credentials = false
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.porkbun_ip.anonymous",
						tfjsonpath.New("ip"), knownvalue.StringExact("198.51.100.9")),
					statecheck.ExpectKnownValue("data.porkbun_ip.anonymous",
						tfjsonpath.New("credentials_valid"), knownvalue.Null()),
				},
			},
		},
	})
}
