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

// tldRequirementsAPI serves /domain/getRegistrationRequirements/{tld} for
// two deliberately opposite TLDs, so every flag is exercised both ways: an
// assertion whose expected value is false or null also passes when the
// decode never read the field.
//
// It lives in this file rather than in the shared fake_api_test.go so that
// the sibling branches adding other API surfaces do not conflict here.
type tldRequirementsAPI struct{}

func newTLDRequirementsAPI(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(&tldRequirementsAPI{})
	t.Cleanup(srv.Close)
	return srv.URL
}

func (a *tldRequirementsAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-API-Key") == "" || r.Header.Get("X-Secret-API-Key") == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_API_KEYS_001", "Missing API credentials.")
		return
	}

	const prefix = "domain/getRegistrationRequirements/"
	path := strings.Trim(r.URL.Path, "/")
	if !strings.HasPrefix(path, prefix) {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "No such endpoint: "+path)
		return
	}

	// Porkbun's own /mock echoes "tld":"us" whatever TLD is asked for, so
	// this fake does too: the data source must keep the configured spelling
	// rather than trust the echo, or Terraform rejects the read.
	switch strings.TrimPrefix(path, prefix) {
	case "com":
		writeJSON(w, map[string]any{
			"status":                    "SUCCESS",
			"tld":                       "us",
			"apiRegisterable":           true,
			"registrationDurationYears": 1,
			"maxRegistrationYears":      10,
			"whoisPrivacySupported":     true,
			"requiresValidatedAddress":  false,
			"registrantOnly":            false,
			"requestSchema": map[string]any{
				"$schema":  "https://json-schema.org/draft/2020-12/schema",
				"title":    "domain/create request for .com",
				"type":     "object",
				"required": []string{"cost", "agreeToTerms"},
			},
			// No structured extra requirements: null, never {}.
			"registryRequirements": nil,
		})
	case "us":
		writeJSON(w, map[string]any{
			"status":          "SUCCESS",
			"tld":             "us",
			"apiRegisterable": false,
			// Sent as strings, the way Porkbun types numbers and booleans on
			// several other endpoints.
			"registrationDurationYears": "1",
			// Omitted entirely: "no maximum stated", which must not become 0.
			"whoisPrivacySupported":    "yes",
			"requiresValidatedAddress": 1,
			"registrantOnly":           true,
			"requestSchema": map[string]any{
				"$schema": "https://json-schema.org/draft/2020-12/schema",
				"type":    "object",
			},
			"registryRequirements": map[string]any{
				"$schema": "https://json-schema.org/draft/2020-12/schema",
				"title":   "Registry eligibility data for .us",
				"type":    "object",
				"properties": map[string]any{
					"purpose":  map[string]any{"type": "string", "enum": []string{"P1", "P2"}},
					"category": map[string]any{"type": "string", "enum": []string{"C11", "C12"}},
				},
				"x-policyNote": "Inaccurate nexus data can lead to seizure without refund.",
			},
			"notApiRegisterableReason": "Registry eligibility data cannot be submitted through the API.",
		})
	default:
		writeErr(w, http.StatusBadRequest, "MISSING_PARAMETER", "Unknown TLD.")
	}
}

func TestAccTLDRegistrationRequirementsDataSource(t *testing.T) {
	url := newTLDRequirementsAPI(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_tld_registration_requirements" "com" {
  tld = "com"
}

data "porkbun_tld_registration_requirements" "us" {
  tld = "us"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				// The fake answers "tld":"us" for .com as well; state must
				// still say com.
				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.com",
					tfjsonpath.New("tld"), knownvalue.StringExact("com")),

				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.com",
					tfjsonpath.New("api_registerable"), knownvalue.Bool(true)),
				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.us",
					tfjsonpath.New("api_registerable"), knownvalue.Bool(false)),

				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.com",
					tfjsonpath.New("whois_privacy_supported"), knownvalue.Bool(true)),
				// "yes" must decode as true, not as an unparsed false.
				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.us",
					tfjsonpath.New("whois_privacy_supported"), knownvalue.Bool(true)),

				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.com",
					tfjsonpath.New("requires_validated_address"), knownvalue.Bool(false)),
				// 1 must decode as true.
				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.us",
					tfjsonpath.New("requires_validated_address"), knownvalue.Bool(true)),

				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.com",
					tfjsonpath.New("registrant_only"), knownvalue.Bool(false)),
				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.us",
					tfjsonpath.New("registrant_only"), knownvalue.Bool(true)),

				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.com",
					tfjsonpath.New("registration_duration_years"), knownvalue.Int64Exact(1)),
				// Sent as "1".
				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.us",
					tfjsonpath.New("registration_duration_years"), knownvalue.Int64Exact(1)),

				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.com",
					tfjsonpath.New("max_registration_years"), knownvalue.Int64Exact(10)),
				// Absent upstream: null, not 0, which would read as "no
				// years permitted".
				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.us",
					tfjsonpath.New("max_registration_years"), knownvalue.Null()),

				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.com",
					tfjsonpath.New("not_api_registerable_reason"), knownvalue.Null()),
				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.us",
					tfjsonpath.New("not_api_registerable_reason"), knownvalue.StringExact(
						"Registry eligibility data cannot be submitted through the API.")),

				// The schemas reach state as JSON strings jsondecode() reads.
				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.com",
					tfjsonpath.New("request_schema"), knownvalue.StringRegexp(
						regexp.MustCompile(`"title":"domain/create request for \.com"`))),
				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.us",
					tfjsonpath.New("registry_requirements"), knownvalue.StringRegexp(
						regexp.MustCompile(`"x-policyNote":`))),
				// A TLD with no extra registry data must be null, so
				// `registry_requirements != null` is a usable test.
				statecheck.ExpectKnownValue("data.porkbun_tld_registration_requirements.com",
					tfjsonpath.New("registry_requirements"), knownvalue.Null()),
			},
		}},
	})
}

// A leading dot is the same lookup spelled differently, and must be rejected
// at validate time rather than turned into an unknown-TLD API error.
func TestAccTLDRegistrationRequirementsRejectsLeadingDot(t *testing.T) {
	url := newTLDRequirementsAPI(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_tld_registration_requirements" "dotted" {
  tld = ".us"
}
`,
			ExpectError: regexp.MustCompile(`TLD must not start with a dot`),
		}},
	})
}
