package porkbun

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// hostingMockBody fetches a mock endpoint's raw body.
//
// /hosting/plans cannot go through the client: the mock renders the
// envelope's `status` from the spec's untyped example, so hosting endpoints
// answer `"status":"string"` and the client correctly rejects anything that
// is not SUCCESS. The field names and value shapes below are still the real
// contract, which is what this tier is for.
func hostingMockBody(t *testing.T, c *Client, path string) []byte {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, c.BaseURL()+"/"+path, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Skipf("porkbun mock API unreachable, skipping: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Skipf("porkbun mock API read failed, skipping: %v", err)
	}
	if resp.StatusCode >= 400 {
		t.Skipf("porkbun mock API returned %d for %s, skipping", resp.StatusCode, path)
	}
	return body
}

// TestMockHostingPlans is tier 1: it checks the shape Porkbun actually
// serves, not the placeholder values in it. The mock's own example data is
// Porkbun's to edit — price and trialDays are 0 there and features is empty
// — so asserting on those values would make this build fail on a harmless
// example change. What must not change is the set of field names and the
// decode agreeing with whatever the body said.
func TestMockHostingPlans(t *testing.T) {
	c := mockClient(t)
	body := hostingMockBody(t, c, "hosting/plans")

	var raw struct {
		Plans []map[string]json.RawMessage `json:"plans"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decoding /hosting/plans: %v", err)
	}
	if len(raw.Plans) == 0 {
		t.Fatal("expected at least one plan from the mock")
	}
	for _, field := range []string{"product", "plan", "sku", "interval", "price", "priceFormatted", "trialDays", "name", "features"} {
		if _, ok := raw.Plans[0][field]; !ok {
			t.Errorf("field %q is gone from /hosting/plans; the data source exposes it", field)
		}
	}

	var decoded hostingPlansResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding /hosting/plans into the client's own types: %v", err)
	}
	plans := hostingPlansFrom(decoded, ListHostingPlansOptions{})
	if len(plans) != len(raw.Plans) {
		t.Fatalf("decoded %d plans, body carried %d", len(plans), len(raw.Plans))
	}

	// Agreement with the body, whatever the body happens to say.
	for i, got := range plans {
		var want struct {
			Product string           `json:"product"`
			SKU     string           `json:"sku"`
			Price   *json.Number     `json:"price"`
			Feature *json.RawMessage `json:"features"`
		}
		body, err := json.Marshal(raw.Plans[i])
		if err != nil {
			t.Fatalf("re-encoding plan %d: %v", i, err)
		}
		if err := json.Unmarshal(body, &want); err != nil {
			t.Fatalf("decoding plan %d: %v", i, err)
		}
		if got.Product != want.Product || got.SKU != want.SKU {
			t.Errorf("plan %d decoded as product=%q sku=%q, body said %q/%q", i, got.Product, got.SKU, want.Product, want.SKU)
		}
		switch {
		case want.Price == nil && got.Price != nil:
			t.Errorf("plan %d: body sent no price, decoded %d", i, *got.Price)
		case want.Price != nil && got.Price == nil:
			t.Errorf("plan %d: body sent price %s, decoded nil", i, want.Price.String())
		case want.Price != nil:
			if n, err := want.Price.Int64(); err == nil && n != *got.Price {
				t.Errorf("plan %d: price decoded as %d, body said %s", i, *got.Price, want.Price.String())
			}
		}
		if want.Feature != nil && string(*want.Feature) != "null" && got.Features == nil {
			t.Errorf("plan %d: body carried a features object, decoded nil", i)
		}
	}

	// A prefix that cannot match anything filters locally: the endpoint
	// takes no parameters, so there is nothing for the API to reject.
	if filtered := hostingPlansFrom(decoded, ListHostingPlansOptions{SKUPrefix: "NO_SUCH_SKU_PREFIX"}); len(filtered) != 0 {
		t.Errorf("expected no plans for an impossible prefix, got %d", len(filtered))
	}

	// The full client path is still exercised, tolerantly: the one tolerated
	// failure is a well-formed envelope whose status is not SUCCESS, which
	// is the placeholder the mock renders here. The literal placeholder is
	// deliberately not asserted — it is Porkbun's example data to change —
	// and a mock fixed to answer SUCCESS passes outright.
	_, err := c.ListHostingPlans(context.Background(), ListHostingPlansOptions{})
	skipIfUnavailable(t, err)
	if err != nil {
		var apiErr *Error
		if !as(err, &apiErr) || strings.EqualFold(apiErr.Status, "SUCCESS") {
			t.Errorf("ListHostingPlans against the mock: %v", err)
		}
	}
}

// hostingPlansBody is a hand-built response covering what the mock's
// placeholder data cannot: a non-zero price, a null trial, a price sent as a
// numeric string, an absent price, and a feature bag of mixed types.
const hostingPlansBody = `{
  "status": "SUCCESS",
  "plans": [
    {
      "product": "cloudWordpress",
      "plan": "monthly",
      "sku": "CLOUDWORDPRESSM1",
      "interval": "month",
      "price": 1200,
      "priceFormatted": "$12.00",
      "trialDays": null,
      "name": "Cloud for WordPress Monthly",
      "features": {"storage": 10, "managed": true, "cdn": "included", "limits": { "sites" : 1 }}
    },
    {
      "product": "secureStaticHosting",
      "plan": "yearly",
      "sku": "PIXIESECURESTATICY2",
      "interval": "year",
      "price": "3000",
      "priceFormatted": "$30.00",
      "trialDays": 15,
      "name": "Secure Static Hosting Yearly",
      "features": {}
    },
    {
      "product": "secureStaticHosting",
      "plan": "monthly",
      "sku": "PIXIESECURESTATICM2",
      "interval": "month",
      "priceFormatted": "",
      "trialDays": 0,
      "name": "Secure Static Hosting Monthly"
    }
  ]
}`

func hostingPlansServer(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hosting/plans" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("/hosting/plans takes no parameters, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(hostingPlansBody))
	}))
	t.Cleanup(srv.Close)
	return testClient(t, srv.URL)
}

func TestListHostingPlansDecodesQuirks(t *testing.T) {
	t.Parallel()

	plans, err := hostingPlansServer(t).ListHostingPlans(context.Background(), ListHostingPlansOptions{})
	if err != nil {
		t.Fatalf("ListHostingPlans: %v", err)
	}
	if len(plans) != 3 {
		t.Fatalf("got %d plans, want 3", len(plans))
	}

	// Sorted by SKU, so the WordPress plan comes first.
	if plans[0].SKU != "CLOUDWORDPRESSM1" || plans[1].SKU != "PIXIESECURESTATICM2" || plans[2].SKU != "PIXIESECURESTATICY2" {
		t.Fatalf("plans are not sorted by sku: %q %q %q", plans[0].SKU, plans[1].SKU, plans[2].SKU)
	}

	wp := plans[0]
	if wp.Price == nil || *wp.Price != 1200 {
		t.Errorf("price = %v, want 1200 cents", wp.Price)
	}
	if wp.TrialDays != nil {
		t.Errorf("a null trialDays must stay nil, got %d", *wp.TrialDays)
	}
	if wp.Product != "cloudWordpress" || wp.Name != "Cloud for WordPress Monthly" {
		t.Errorf("product/name did not decode: %+v", wp)
	}
	want := map[string]string{"storage": "10", "managed": "true", "cdn": "included", "limits": `{"sites":1}`}
	for k, v := range want {
		if wp.Features[k] != v {
			t.Errorf("features[%q] = %q, want %q", k, wp.Features[k], v)
		}
	}

	monthly := plans[1]
	if monthly.Price != nil {
		t.Errorf("an absent price must stay nil, got %d", *monthly.Price)
	}
	if monthly.TrialDays == nil || *monthly.TrialDays != 0 {
		t.Errorf("trialDays = %v, want an explicit 0", monthly.TrialDays)
	}
	if monthly.Features != nil {
		t.Errorf("an absent features object must stay nil, got %v", monthly.Features)
	}

	yearly := plans[2]
	if yearly.Price == nil || *yearly.Price != 3000 {
		t.Errorf("a numeric-string price did not decode: %v", yearly.Price)
	}
	if yearly.TrialDays == nil || *yearly.TrialDays != 15 {
		t.Errorf("trialDays = %v, want 15", yearly.TrialDays)
	}
	if yearly.Features == nil || len(yearly.Features) != 0 {
		t.Errorf("an empty features object must stay empty and non-nil, got %v", yearly.Features)
	}
}

func TestListHostingPlansFilters(t *testing.T) {
	t.Parallel()

	c := hostingPlansServer(t)

	// Case-insensitive on both filters: the SKUs are upper case and the
	// products are camel case, and neither is a spelling a user will match.
	static, err := c.ListHostingPlans(context.Background(), ListHostingPlansOptions{Product: "SECURESTATICHOSTING"})
	if err != nil {
		t.Fatalf("ListHostingPlans: %v", err)
	}
	if len(static) != 2 {
		t.Fatalf("product filter kept %d plans, want 2", len(static))
	}
	for _, p := range static {
		if !strings.HasPrefix(p.SKU, "PIXIE") {
			t.Errorf("product filter let %q through", p.SKU)
		}
	}

	wp, err := c.ListHostingPlans(context.Background(), ListHostingPlansOptions{SKUPrefix: "cloudwordpress"})
	if err != nil {
		t.Fatalf("ListHostingPlans: %v", err)
	}
	if len(wp) != 1 || wp[0].SKU != "CLOUDWORDPRESSM1" {
		t.Fatalf("sku_prefix filter returned %+v", wp)
	}

	none, err := c.ListHostingPlans(context.Background(), ListHostingPlansOptions{Product: "secureStaticHosting", SKUPrefix: "CLOUDWORDPRESS"})
	if err != nil {
		t.Fatalf("ListHostingPlans: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("filters must be conjunctive, got %+v", none)
	}
}
