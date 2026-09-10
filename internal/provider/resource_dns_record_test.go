package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func dnsRecordConfig(baseURL, content string, ttl int64) string {
	return providerConfig(baseURL) + fmt.Sprintf(`
resource "porkbun_dns_record" "test" {
  domain  = "trout.quest"
  name    = "www"
  type    = "A"
  content = %q
  ttl     = %d
}
`, content, ttl)
}

// TestAccDNSRecordLifecycle covers create, an idempotent re-plan, update,
// and import by "domain/record_id".
//
// The re-plan step is the one that matters: the API returns ttl as a string,
// prio as null and name fully qualified, so a provider that does not convert
// on read shows a permanent diff on every record.
func TestAccDNSRecordLifecycle(t *testing.T) {
	fake, url := newFakeAPI(t)
	fake.seedDomain("trout.quest", "curitiba.ns.porkbun.com", "fortaleza.ns.porkbun.com")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: dnsRecordConfig(url, "1.2.3.4", 600),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("porkbun_dns_record.test",
						tfjsonpath.New("name"), knownvalue.StringExact("www")),
					statecheck.ExpectKnownValue("porkbun_dns_record.test",
						tfjsonpath.New("ttl"), knownvalue.Int64Exact(600)),
					statecheck.ExpectKnownValue("porkbun_dns_record.test",
						tfjsonpath.New("prio"), knownvalue.Int64Exact(0)),
				},
			},
			{
				Config: dnsRecordConfig(url, "1.2.3.4", 600),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: dnsRecordConfig(url, "5.6.7.8", 900),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("porkbun_dns_record.test",
						tfjsonpath.New("content"), knownvalue.StringExact("5.6.7.8")),
					statecheck.ExpectKnownValue("porkbun_dns_record.test",
						tfjsonpath.New("ttl"), knownvalue.Int64Exact(900)),
				},
			},
			{
				ResourceName:            "porkbun_dns_record.test",
				ImportState:             true,
				ImportStateIdFunc:       importIDFromAttributes("porkbun_dns_record.test", "domain", "id"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{},
			},
		},
	})
}

// TestAccDNSRecordApexName covers the empty-name case, where the API returns
// the bare domain as the record name and the provider must convert it back
// to "".
func TestAccDNSRecordApexName(t *testing.T) {
	fake, url := newFakeAPI(t)
	fake.seedDomain("trout.quest", "curitiba.ns.porkbun.com", "fortaleza.ns.porkbun.com")

	config := providerConfig(url) + `
resource "porkbun_dns_record" "apex" {
  domain  = "trout.quest"
  type    = "A"
  content = "1.2.3.4"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("porkbun_dns_record.apex",
						tfjsonpath.New("name"), knownvalue.StringExact("")),
				},
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// TestAccDNSRecordDisappears covers out-of-band deletion: Read must drop the
// resource from state so the next plan recreates it, not return an error.
func TestAccDNSRecordDisappears(t *testing.T) {
	fake, url := newFakeAPI(t)
	fake.seedDomain("trout.quest", "curitiba.ns.porkbun.com", "fortaleza.ns.porkbun.com")

	config := dnsRecordConfig(url, "1.2.3.4", 600)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { fake.deleteAllRecords("trout.quest") },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccDNSRecordRejectsBadInput(t *testing.T) {
	_, url := newFakeAPI(t)

	for name, tc := range map[string]struct {
		config string
		expect *regexp.Regexp
	}{
		"unknown type": {
			config: providerConfig(url) + `
resource "porkbun_dns_record" "bad" {
  domain  = "trout.quest"
  type    = "WKS"
  content = "1.2.3.4"
}`,
			expect: regexp.MustCompile(`Invalid Attribute Value Match`),
		},
		"ttl below minimum": {
			config: providerConfig(url) + `
resource "porkbun_dns_record" "bad" {
  domain  = "trout.quest"
  type    = "A"
  content = "1.2.3.4"
  ttl     = 60
}`,
			expect: regexp.MustCompile(`at least 600`),
		},
	} {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { testAccPreCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps:                    []resource.TestStep{{Config: tc.config, ExpectError: tc.expect}},
			})
		})
	}
}

func TestAccDNSRecordImportRejectsBareID(t *testing.T) {
	fake, url := newFakeAPI(t)
	fake.seedDomain("trout.quest", "curitiba.ns.porkbun.com", "fortaleza.ns.porkbun.com")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: dnsRecordConfig(url, "1.2.3.4", 600)},
			{
				ResourceName:  "porkbun_dns_record.test",
				ImportState:   true,
				ImportStateId: "123456",
				ExpectError:   regexp.MustCompile(`Invalid import identifier`),
			},
		},
	})
}
