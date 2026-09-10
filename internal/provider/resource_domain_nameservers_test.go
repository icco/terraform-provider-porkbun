package provider

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func nameserversConfig(baseURL, domain string, ns []string) string {
	quoted := ""
	for i, n := range ns {
		if i > 0 {
			quoted += ", "
		}
		quoted += fmt.Sprintf("%q", n)
	}
	return providerConfig(baseURL) + fmt.Sprintf(`
resource "porkbun_domain_nameservers" "test" {
  domain      = %q
  nameservers = [%s]
}
`, domain, quoted)
}

// TestAccDomainNameserversLifecycle is the highest-value test in the suite.
//
// The fake deliberately answers getNs with the delegation reversed,
// uppercased and dot-suffixed. If the provider modelled nameservers as an
// ordered list, or normalized on only one side, this test would fail with a
// non-empty plan — which is precisely what would happen on all 27 real
// domains, on every single run.
func TestAccDomainNameserversLifecycle(t *testing.T) {
	fake, url := newFakeAPI(t)
	fake.seedDomain("trout.quest", "curitiba.ns.porkbun.com", "fortaleza.ns.porkbun.com")

	cloudDNS := []string{
		"ns-cloud-a1.googledomains.com.",
		"ns-cloud-a2.googledomains.com.",
		"ns-cloud-a3.googledomains.com.",
		"ns-cloud-a4.googledomains.com.",
	}
	updated := []string{
		"ns-cloud-b1.googledomains.com.",
		"ns-cloud-b2.googledomains.com.",
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: nameserversConfig(url, "trout.quest", cloudDNS),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("porkbun_domain_nameservers.test",
						tfjsonpath.New("id"), knownvalue.StringExact("trout.quest")),
					statecheck.ExpectKnownValue("porkbun_domain_nameservers.test",
						tfjsonpath.New("nameservers"), knownvalue.SetSizeExact(4)),
				},
			},
			// Refresh and re-plan against a registry that scrambles the
			// order, case and trailing dots. The plan must be empty.
			{
				Config: nameserversConfig(url, "trout.quest", cloudDNS),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			// The registry re-orders and re-cases the RRset on its own,
			// which it is entitled to do. Still not a change.
			{
				PreConfig: func() { fake.churnNameservers("trout.quest") },
				Config:    nameserversConfig(url, "trout.quest", cloudDNS),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			// The same delegation respelled in the configuration — different
			// order, different case, no trailing dots — is not a change
			// either. This is what makes an imported domain adoptable
			// without a cosmetic first apply.
			{
				Config: nameserversConfig(url, "trout.quest", []string{
					"NS-CLOUD-A3.googledomains.com",
					"ns-cloud-a1.googledomains.com",
					"ns-cloud-a4.GOOGLEDOMAINS.com",
					"ns-cloud-a2.googledomains.com",
				}),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: nameserversConfig(url, "trout.quest", updated),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("porkbun_domain_nameservers.test",
						tfjsonpath.New("nameservers"), knownvalue.SetSizeExact(2)),
				},
			},
			// Import by ID. State after import holds the registry's own
			// normalized spelling, because an import has no configuration to
			// take a spelling from; ImportStateVerify would therefore trip
			// over the trailing dots this test's config uses, so the imported
			// attributes are checked directly instead.
			{
				ResourceName:     "porkbun_domain_nameservers.test",
				ImportState:      true,
				ImportStateId:    "trout.quest",
				ImportStateCheck: checkImportedNameservers("trout.quest", "ns-cloud-b1.googledomains.com", "ns-cloud-b2.googledomains.com"),
			},
			// Import by resource identity (Terraform 1.12+), the mechanism a
			// fleet of already hand-configured domains migrates through.
			{
				ResourceName:     "porkbun_domain_nameservers.test",
				ImportState:      true,
				ImportStateKind:  resource.ImportBlockWithResourceIdentity,
				ImportStateCheck: checkImportedNameservers("trout.quest", "ns-cloud-b1.googledomains.com", "ns-cloud-b2.googledomains.com"),
			},
		},
	})

	// Destroy ran at the end of the test case. The registry must be untouched.
	if got := fake.currentNameservers("trout.quest"); !reflect.DeepEqual(got, []string{
		"ns-cloud-b1.googledomains.com", "ns-cloud-b2.googledomains.com",
	}) {
		t.Errorf("destroy changed the registry delegation: %v", got)
	}
	if fake.deleteNsCalls != 0 {
		t.Errorf("destroy made %d nameserver-clearing API calls, want 0", fake.deleteNsCalls)
	}
}

// checkImportedNameservers asserts the attributes an import produced.
func checkImportedNameservers(domain string, ns ...string) resource.ImportStateCheckFunc {
	return func(states []*terraform.InstanceState) error {
		if len(states) != 1 {
			return fmt.Errorf("expected 1 imported instance, got %d", len(states))
		}
		attrs := states[0].Attributes
		if attrs["domain"] != domain {
			return fmt.Errorf("domain = %q, want %q", attrs["domain"], domain)
		}
		if attrs["id"] != domain {
			return fmt.Errorf("id = %q, want %q", attrs["id"], domain)
		}
		got := map[string]bool{}
		for k, v := range attrs {
			if strings.HasPrefix(k, "nameservers.") && k != "nameservers.#" {
				got[v] = true
			}
		}
		if len(got) != len(ns) {
			return fmt.Errorf("imported %d nameservers, want %d: %v", len(got), len(ns), attrs)
		}
		for _, want := range ns {
			if !got[want] {
				return fmt.Errorf("imported nameservers missing %q: %v", want, got)
			}
		}
		return nil
	}
}

// TestAccDomainNameserversDisappears covers a domain leaving the account
// out of band: Read must drop the resource rather than fail forever.
func TestAccDomainNameserversDisappears(t *testing.T) {
	fake, url := newFakeAPI(t)
	fake.seedDomain("sadnat.com", "curitiba.ns.porkbun.com", "fortaleza.ns.porkbun.com")

	config := nameserversConfig(url, "sadnat.com", []string{"a.example.com", "b.example.com"})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { fake.forgetDomain("sadnat.com") },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestAccDomainNameserversTooFew checks the schema rejects a delegation that
// could not work before any API call happens.
func TestAccDomainNameserversTooFew(t *testing.T) {
	_, url := newFakeAPI(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      nameserversConfig(url, "trout.quest", []string{"only-one.example.com"}),
			ExpectError: regexp.MustCompile(`at least 2 elements`),
		}},
	})
}

// TestAccDomainNameserversStaleReadBack covers the registry not yet showing
// the delegation that was just written.
//
// /domain/getNs reads live from the registry and propagation is not
// synchronous, so the read-back immediately after updateNs can still return
// the previous set. `nameservers` is Required, so if the provider stored
// that stale answer Terraform core would abort the apply with "Provider
// produced inconsistent result after apply … This is a bug in the provider"
// — on a change that in fact succeeded. The apply must complete, state must
// hold the configured value, and the following plan must be empty once the
// registry catches up.
func TestAccDomainNameserversStaleReadBack(t *testing.T) {
	fake, url := newFakeAPI(t)
	fake.seedDomain("laggy.quest", "curitiba.ns.porkbun.com", "fortaleza.ns.porkbun.com")

	cloudDNS := []string{
		"ns-cloud-a1.googledomains.com.",
		"ns-cloud-a2.googledomains.com.",
	}
	config := nameserversConfig(url, "laggy.quest", cloudDNS)

	// One getNs after the write still reports the old delegation.
	fake.serveStaleReads(1)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					// The configured spelling, not the registry's stale answer.
					statecheck.ExpectKnownValue("porkbun_domain_nameservers.test",
						tfjsonpath.New("nameservers"), knownvalue.SetExact([]knownvalue.Check{
							knownvalue.StringExact("ns-cloud-a1.googledomains.com."),
							knownvalue.StringExact("ns-cloud-a2.googledomains.com."),
						})),
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

	if got := fake.currentNameservers("laggy.quest"); !reflect.DeepEqual(got, []string{
		"ns-cloud-a1.googledomains.com", "ns-cloud-a2.googledomains.com",
	}) {
		t.Errorf("registry holds %v, want the applied delegation", got)
	}
}

// TestAccDomainNameserversCollapsingDuplicates covers a config that passes
// the element-count validator and still delegates to one nameserver.
//
// Two spellings of one hostname are two strings, so setvalidator.SizeBetween
// is satisfied; NormalizeNameservers folds them to one before the payload is
// built. Nothing downstream would notice — Read normalizes both sides, so
// two-in-state against one-at-the-registry compares equal forever.
func TestAccDomainNameserversCollapsingDuplicates(t *testing.T) {
	fake, url := newFakeAPI(t)
	fake.seedDomain("dupe.quest", "curitiba.ns.porkbun.com", "fortaleza.ns.porkbun.com")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      nameserversConfig(url, "dupe.quest", []string{"ns1.example.com", "NS1.example.com."}),
			ExpectError: regexp.MustCompile(`Too few distinct nameservers`),
		}},
	})

	// The delegation must be untouched: the config never reached updateNs.
	if got := fake.currentNameservers("dupe.quest"); !reflect.DeepEqual(got, []string{
		"curitiba.ns.porkbun.com", "fortaleza.ns.porkbun.com",
	}) {
		t.Errorf("registry was modified by a rejected config: %v", got)
	}
}

// TestAccDomainNameserversRequiresCanonicalDomain covers `domain` being
// compared literally while DNS names are case-insensitive.
func TestAccDomainNameserversRequiresCanonicalDomain(t *testing.T) {
	_, url := newFakeAPI(t)

	for name, domain := range map[string]string{
		"mixed case":   "Case.Quest",
		"trailing dot": "case.quest.",
	} {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { testAccPreCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config:      nameserversConfig(url, domain, []string{"a.example.com", "b.example.com"}),
					ExpectError: regexp.MustCompile(`Domain is not in canonical form`),
				}},
			})
		})
	}
}

// TestAccDomainNameserversDataSource reads a delegation without managing it.
func TestAccDomainNameserversDataSource(t *testing.T) {
	fake, url := newFakeAPI(t)
	fake.seedDomain("welch.io", "curitiba.ns.porkbun.com", "fortaleza.ns.porkbun.com")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_domain_nameservers" "test" {
  domain = "welch.io"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_domain_nameservers.test",
					tfjsonpath.New("nameservers"), knownvalue.SetExact([]knownvalue.Check{
						knownvalue.StringExact("curitiba.ns.porkbun.com"),
						knownvalue.StringExact("fortaleza.ns.porkbun.com"),
					})),
			},
		}},
	})
}
