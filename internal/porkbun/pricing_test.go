package porkbun

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The live catalog sends "coupons": [] for every TLD and the /mock endpoint
// sends a populated object, so neither source alone exercises both branches
// of CouponSet. This does.
func TestPricingDecode(t *testing.T) {
	t.Parallel()

	const body = `{
	  "status": "SUCCESS",
	  "pricing": {
	    "com": {"registration": "11.08", "renewal": "11.08", "transfer": "11.08", "coupons": []},
	    "security": {"registration": "2,060.25", "renewal": "2,060.25", "transfer": "2,060.25", "coupons": []},
	    "uk": {"registration": "4.32", "renewal": "5.66", "transfer": "0.00", "coupons": {}},
	    "0z": {"registration": "42.52", "renewal": "16.77", "transfer": "16.77", "coupons": [], "specialType": "handshake"},
	    "quest": {
	      "registration": "1.10", "renewal": "22.62", "transfer": "22.62", "specialType": null,
	      "coupons": {"registration": {"code": "AWESOMENESS", "max_per_user": "1", "first_year_only": "yes", "type": "amount", "amount": 1.5}}
	    },
	    "io": {
	      "registration": "43.94", "renewal": "43.94", "transfer": "43.94",
	      "coupons": {"registration": {"code": "SPARSE", "amount": null}}
	    }
	  }
	}`

	var out struct {
		Pricing map[string]TLDPricing `json:"pricing"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decoding pricing: %v", err)
	}

	// A grouped price must survive verbatim: it is the value that would be
	// mangled by any numeric decoding.
	if got := string(out.Pricing["security"].Registration); got != "2,060.25" {
		t.Errorf("security registration = %q, want the string Porkbun sent", got)
	}
	if got := string(out.Pricing["uk"].Transfer); got != "0.00" {
		t.Errorf("uk transfer = %q", got)
	}
	if got := string(out.Pricing["0z"].SpecialType); got != "handshake" {
		t.Errorf("0z specialType = %q", got)
	}
	if got := string(out.Pricing["quest"].SpecialType); got != "" {
		t.Errorf("null specialType = %q, want empty", got)
	}
	if n := len(out.Pricing["com"].Coupons); n != 0 {
		t.Errorf(`"coupons": [] decoded to %d coupons`, n)
	}
	if n := len(out.Pricing["uk"].Coupons); n != 0 {
		t.Errorf(`"coupons": {} decoded to %d coupons`, n)
	}

	c, ok := out.Pricing["quest"].Coupons["registration"]
	if !ok {
		t.Fatalf("quest coupons did not decode: %+v", out.Pricing["quest"].Coupons)
	}
	if string(c.Code) != "AWESOMENESS" {
		t.Errorf("coupon code = %q", c.Code)
	}
	// max_per_user is typed as an integer in the spec and arrives as a
	// string here; amount is the reverse.
	if c.MaxPerUser.Int64() != 1 {
		t.Errorf("coupon max_per_user = %d", c.MaxPerUser.Int64())
	}
	if string(c.Amount) != "1.5" {
		t.Errorf("coupon amount = %q", c.Amount)
	}
	if string(c.FirstYearOnly) != "yes" {
		t.Errorf("coupon first_year_only = %q", c.FirstYearOnly)
	}

	// No live coupon has ever been observed — only /mock's, which fills in
	// every field — so a coupon with fields missing must still decode. It
	// reads as absent, which the data source renders as first_year_only =
	// false and max_per_user = 0.
	sparse, ok := out.Pricing["io"].Coupons["registration"]
	if !ok {
		t.Fatalf("sparse coupon did not decode: %+v", out.Pricing["io"].Coupons)
	}
	if string(sparse.FirstYearOnly) != "" || sparse.MaxPerUser.Int64() != 0 || string(sparse.Amount) != "" {
		t.Errorf("missing coupon fields did not decode to zero values: %+v", sparse)
	}
}

// A populated array has never been observed, but keying it by index beats
// dropping a live discount on the floor.
func TestCouponSetPopulatedArray(t *testing.T) {
	t.Parallel()

	var cs CouponSet
	if err := json.Unmarshal([]byte(`[{"code":"ONE"},{"code":"TWO"}]`), &cs); err != nil {
		t.Fatalf("decoding coupon array: %v", err)
	}
	if len(cs) != 2 || string(cs["1"].Code) != "TWO" {
		t.Errorf("coupon array decoded to %+v", cs)
	}
}

func TestNormalizeTLD(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"com":       "com",
		".COM":      "com",
		"  .Co.Uk ": "co.uk",
		"":          "",
	} {
		if got := NormalizeTLD(in); got != want {
			t.Errorf("NormalizeTLD(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMockPricing(t *testing.T) {
	c := mockClient(t)

	pricing, err := c.GetPricing(context.Background(), nil)
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("GetPricing: %v", err)
	}
	if len(pricing) == 0 {
		t.Fatal("expected at least one TLD from the mock")
	}

	var tld string
	for k := range pricing {
		tld = k
		break
	}
	p := pricing[tld]
	if p.Registration == "" || p.Renewal == "" || p.Transfer == "" {
		t.Errorf("prices did not decode for %q: %+v", tld, p)
	}
	// The mock is the only source that sends coupons as a populated object.
	for product, coupon := range p.Coupons {
		if coupon.Code == "" {
			t.Errorf("coupon %q decoded without a code: %+v", product, coupon)
		}
	}

	// The filter is applied locally, so an upper-cased dotted spelling of a
	// key the catalog already returned must still match it.
	filtered, err := c.GetPricing(context.Background(), []string{"." + strings.ToUpper(tld), "definitelynotatld"})
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("GetPricing filtered: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("filtered pricing = %v, want just %q", filtered, tld)
	}
	if _, ok := filtered[tld]; !ok {
		t.Errorf("filtered pricing lost %q: %v", tld, filtered)
	}
}
