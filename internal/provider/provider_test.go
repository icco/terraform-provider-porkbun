package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"porkbun": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck keeps credentials out of the acceptance tier. Every test
// here talks to an in-process fake, so there is nothing to check beyond
// TF_ACC, which the harness enforces itself.
func testAccPreCheck(t *testing.T) { t.Helper() }

// providerConfig points the provider at a fake API with throwaway credentials.
func providerConfig(baseURL string) string {
	return fmt.Sprintf(`
provider "porkbun" {
  api_key     = "pk1_test"
  secret_key  = "sk1_test"
  base_url    = %q
  max_retries = 0
}
`, baseURL)
}

// TestAccProviderBaseURLFromConfig proves base_url is honoured from the
// configuration and not only from PORKBUN_BASE_URL. Every test in this
// package depends on that, and it used to be broken.
func TestAccProviderBaseURLFromConfig(t *testing.T) {
	t.Setenv("PORKBUN_BASE_URL", "https://invalid.example/should-not-be-used")

	fake, url := newFakeAPI(t)
	fake.seedDomain("trout.quest", "curitiba.ns.porkbun.com", "fortaleza.ns.porkbun.com")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_domain_nameservers" "example" {
  domain = "trout.quest"
}
`,
		}},
	})
}
