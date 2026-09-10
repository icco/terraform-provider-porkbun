package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// importIDFromAttributes builds a composite "a/b" import ID out of two
// attributes of an already-applied resource.
func importIDFromAttributes(resourceName, first, second string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("%s not found in state", resourceName)
		}
		return rs.Primary.Attributes[first] + "/" + rs.Primary.Attributes[second], nil
	}
}

func (f *fakeAPI) deleteAllRecords(domain string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.records, domain)
}

func TestAccDomainDataSource(t *testing.T) {
	fake, url := newFakeAPI(t)
	fake.seedDomain("natwelch.com", "curitiba.ns.porkbun.com", "fortaleza.ns.porkbun.com")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_domain" "test" {
  domain = "natwelch.com"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_domain.test",
					tfjsonpath.New("tld"), knownvalue.StringExact("com")),
				statecheck.ExpectKnownValue("data.porkbun_domain.test",
					tfjsonpath.New("api_access"), knownvalue.Bool(true)),
				// notLocal is 1 in the fake: the flag that tells you
				// porkbun_dns_record would apply green and do nothing.
				statecheck.ExpectKnownValue("data.porkbun_domain.test",
					tfjsonpath.New("not_local"), knownvalue.Bool(true)),
			},
		}},
	})
}

func TestAccDomainsDataSource(t *testing.T) {
	fake, url := newFakeAPI(t)
	fake.seedDomain("natwelch.com", "a.example.com", "b.example.com")
	fake.seedDomain("welch.io", "a.example.com", "b.example.com")
	fake.seedDomain("trout.quest", "a.example.com", "b.example.com")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_domains" "all" {
  api_access = true
}

data "porkbun_domains" "filtered" {
  name_contains = "welch"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_domains.all",
					tfjsonpath.New("domains"), knownvalue.SetExact([]knownvalue.Check{
						knownvalue.StringExact("natwelch.com"),
						knownvalue.StringExact("trout.quest"),
						knownvalue.StringExact("welch.io"),
					})),
				statecheck.ExpectKnownValue("data.porkbun_domains.all",
					tfjsonpath.New("details"), knownvalue.ListSizeExact(3)),
				statecheck.ExpectKnownValue("data.porkbun_domains.filtered",
					tfjsonpath.New("domains"), knownvalue.SetExact([]knownvalue.Check{
						knownvalue.StringExact("natwelch.com"),
						knownvalue.StringExact("welch.io"),
					})),
			},
		}},
	})
}
