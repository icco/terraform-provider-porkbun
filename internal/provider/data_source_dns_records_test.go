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

// fakeZoneAPI serves /dns/retrieve only, in the hostile spellings the real
// API uses: fully-qualified names, ttl as a string, prio and notes as null
// when unset, and records in no useful order. It lives here rather than in
// the shared fakeAPI because the zone-level `cloudflare` flag is the point of
// these data sources and the shared fake hardcodes "disabled".
type fakeZoneAPI struct {
	cloudflare string
	// records is served in this order deliberately: the data source has to
	// impose one of its own or the list attribute churns between reads.
	records []map[string]any
}

func newFakeZoneAPI(t *testing.T, cloudflare string, records []map[string]any) string {
	t.Helper()
	f := &fakeZoneAPI{cloudflare: cloudflare, records: records}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return srv.URL
}

func (f *fakeZoneAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-API-Key") == "" || r.Header.Get("X-Secret-API-Key") == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_API_KEYS_001", "Missing API credentials.")
		return
	}

	body := map[string]any{"status": "SUCCESS", "records": f.records}
	// An omitted flag is a real case: it must reach state as null, not false.
	if f.cloudflare != "" {
		body["cloudflare"] = f.cloudflare
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case len(parts) == 4 && parts[0] == "dns" && parts[1] == "retrieve":
		match := []map[string]any{}
		for _, rec := range f.records {
			if rec["id"] == parts[3] {
				match = append(match, rec)
			}
		}
		body["records"] = match
		writeJSON(w, body)

	case len(parts) == 3 && parts[0] == "dns" && parts[1] == "retrieve":
		writeJSON(w, body)

	default:
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "No such endpoint: "+r.URL.Path)
	}
}

// zoneFixture is one zone with every shape that has to survive the round
// trip: a null prio, a real prio, a null notes, a real notes, an apex record
// and a wildcard. Returned out of sorted order on purpose.
func zoneFixture() []map[string]any {
	return []map[string]any{
		{"id": "300", "name": "*.natwelch.com", "type": "A", "content": "203.0.113.9", "ttl": "600", "prio": nil, "notes": nil},
		{"id": "100", "name": "natwelch.com", "type": "MX", "content": "mx1.example.net", "ttl": "3600", "prio": "10", "notes": "primary mx"},
		{"id": "200", "name": "natwelch.com", "type": "A", "content": "203.0.113.7", "ttl": "600", "prio": nil, "notes": nil},
		{"id": "400", "name": "natwelch.com", "type": "MX", "content": "mx2.example.net", "ttl": "3600", "prio": "20", "notes": nil},
		{"id": "500", "name": "www.natwelch.com", "type": "CNAME", "content": "natwelch.com", "ttl": "600", "prio": nil, "notes": ""},
	}
}

func TestAccDNSRecordsDataSource(t *testing.T) {
	url := newFakeZoneAPI(t, "enabled", zoneFixture())

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_dns_records" "all" {
  domain = "natwelch.com"
}

data "porkbun_dns_records" "mx" {
  domain = "natwelch.com"
  type   = "MX"
}

data "porkbun_dns_records" "apex" {
  domain = "natwelch.com"
  name   = ""
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				// The zone flag: "enabled" must survive as true, not as the
				// bool zero value a never-read field would leave behind.
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("cloudflare"), knownvalue.StringExact("enabled")),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("cloudflare_enabled"), knownvalue.Bool(true)),

				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("total_count"), knownvalue.Int64Exact(5)),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records"), knownvalue.ListSizeExact(5)),

				// Sorted by name, then type, then content, then id — so the
				// apex A comes before the apex MXs, and the wildcard first.
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(0).AtMapKey("id"), knownvalue.StringExact("300")),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(1).AtMapKey("id"), knownvalue.StringExact("200")),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(2).AtMapKey("id"), knownvalue.StringExact("100")),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(3).AtMapKey("id"), knownvalue.StringExact("400")),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(4).AtMapKey("id"), knownvalue.StringExact("500")),

				// The wildcard: null prio and null notes stay null.
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(0).AtMapKey("subdomain"), knownvalue.StringExact("*")),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(0).AtMapKey("prio"), knownvalue.Null()),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(0).AtMapKey("notes"), knownvalue.Null()),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(0).AtMapKey("ttl"), knownvalue.Int64Exact(600)),

				// The apex A: subdomain is "", name stays fully qualified.
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(1).AtMapKey("subdomain"), knownvalue.StringExact("")),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(1).AtMapKey("name"), knownvalue.StringExact("natwelch.com")),

				// The MX: a non-zero prio and real notes, which a decode that
				// never read the fields could not produce.
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(2).AtMapKey("prio"), knownvalue.Int64Exact(10)),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(2).AtMapKey("notes"), knownvalue.StringExact("primary mx")),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(2).AtMapKey("ttl"), knownvalue.Int64Exact(3600)),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(3).AtMapKey("prio"), knownvalue.Int64Exact(20)),

				// An empty-string notes is not a note.
				statecheck.ExpectKnownValue("data.porkbun_dns_records.all",
					tfjsonpath.New("records").AtSliceIndex(4).AtMapKey("notes"), knownvalue.Null()),

				// A filter narrows `records` but must not touch total_count,
				// or a filtered read looks like the whole zone.
				statecheck.ExpectKnownValue("data.porkbun_dns_records.mx",
					tfjsonpath.New("records"), knownvalue.ListSizeExact(2)),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.mx",
					tfjsonpath.New("total_count"), knownvalue.Int64Exact(5)),

				// name = "" is the apex filter, not "no filter": it must drop
				// the wildcard and the www CNAME.
				statecheck.ExpectKnownValue("data.porkbun_dns_records.apex",
					tfjsonpath.New("records"), knownvalue.ListSizeExact(3)),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.apex",
					tfjsonpath.New("total_count"), knownvalue.Int64Exact(5)),
			},
		}},
	})
}

// A zone Porkbun says nothing about must not report itself as un-proxied.
func TestAccDNSRecordsDataSourceCloudflareUnreported(t *testing.T) {
	url := newFakeZoneAPI(t, "", zoneFixture())

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_dns_records" "quiet" {
  domain = "natwelch.com"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_dns_records.quiet",
					tfjsonpath.New("cloudflare"), knownvalue.Null()),
				statecheck.ExpectKnownValue("data.porkbun_dns_records.quiet",
					tfjsonpath.New("cloudflare_enabled"), knownvalue.Null()),
			},
		}},
	})
}

func TestAccDNSRecordDataSource(t *testing.T) {
	url := newFakeZoneAPI(t, "disabled", zoneFixture())

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_dns_record" "mx" {
  domain = "natwelch.com"
  id     = "100"
}

data "porkbun_dns_record" "wildcard" {
  domain = "natwelch.com"
  id     = "300"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.porkbun_dns_record.mx",
					tfjsonpath.New("type"), knownvalue.StringExact("MX")),
				statecheck.ExpectKnownValue("data.porkbun_dns_record.mx",
					tfjsonpath.New("name"), knownvalue.StringExact("natwelch.com")),
				statecheck.ExpectKnownValue("data.porkbun_dns_record.mx",
					tfjsonpath.New("subdomain"), knownvalue.StringExact("")),
				statecheck.ExpectKnownValue("data.porkbun_dns_record.mx",
					tfjsonpath.New("prio"), knownvalue.Int64Exact(10)),
				statecheck.ExpectKnownValue("data.porkbun_dns_record.mx",
					tfjsonpath.New("ttl"), knownvalue.Int64Exact(3600)),
				statecheck.ExpectKnownValue("data.porkbun_dns_record.mx",
					tfjsonpath.New("notes"), knownvalue.StringExact("primary mx")),
				statecheck.ExpectKnownValue("data.porkbun_dns_record.mx",
					tfjsonpath.New("cloudflare_enabled"), knownvalue.Bool(false)),

				statecheck.ExpectKnownValue("data.porkbun_dns_record.wildcard",
					tfjsonpath.New("subdomain"), knownvalue.StringExact("*")),
				statecheck.ExpectKnownValue("data.porkbun_dns_record.wildcard",
					tfjsonpath.New("prio"), knownvalue.Null()),
				statecheck.ExpectKnownValue("data.porkbun_dns_record.wildcard",
					tfjsonpath.New("notes"), knownvalue.Null()),
			},
		}},
	})
}

// Porkbun answers an unknown record ID with SUCCESS and an empty array. That
// must be an error: silently handing back nulls would let a plan build on a
// record that does not exist.
func TestAccDNSRecordDataSourceMissingIsAnError(t *testing.T) {
	url := newFakeZoneAPI(t, "disabled", zoneFixture())

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "porkbun_dns_record" "gone" {
  domain = "natwelch.com"
  id     = "999999"
}
`,
			ExpectError: regexp.MustCompile(`No DNS record with ID 999999`),
		}},
	})
}
