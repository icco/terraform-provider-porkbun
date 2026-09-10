package porkbun

import (
	"context"
	"testing"
)

func TestMockCheckDomain(t *testing.T) {
	c := mockClient(t)

	avail, err := c.CheckDomain(context.Background(), "example.com")
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("CheckDomain: %v", err)
	}

	if avail.Domain != "example.com" {
		t.Errorf("Domain = %q, want the name that was checked", avail.Domain)
	}
	// The textual booleans are checked against what the mock actually said
	// on this call, not against the answer it happens to give today.
	// Hard-coding "yes" would hand Porkbun the ability to fail this build
	// by editing its own placeholder data — and a mock that flipped to
	// "no" would fail here while the decoder was working perfectly.
	var raw struct {
		Response struct {
			Avail          *string `json:"avail"`
			Premium        *string `json:"premium"`
			FirstYearPromo *string `json:"firstYearPromo"`
		} `json:"response"`
	}
	if err := c.post(context.Background(), "domain/checkDomain/"+escapePath("example.com"),
		map[string]any{}, &raw); err != nil {
		skipIfUnavailable(t, err)
		t.Fatalf("raw checkDomain: %v", err)
	}
	for _, tc := range []struct {
		name string
		raw  *string
		got  bool
	}{
		{"avail", raw.Response.Avail, avail.Available},
		{"premium", raw.Response.Premium, avail.Premium},
		{"firstYearPromo", raw.Response.FirstYearPromo, avail.FirstYearPromo},
	} {
		if tc.raw == nil {
			t.Errorf("%s is absent from the response; it may have been renamed", tc.name)
			continue
		}
		if want := *tc.raw == "yes"; tc.got != want {
			t.Errorf("%s: API said %q, decoded to %v", tc.name, *tc.raw, tc.got)
		}
	}
	if avail.Registration.Price == "" || avail.Registration.RegularPrice == "" {
		t.Errorf("registration pricing did not decode: %+v", avail.Registration)
	}
	if avail.MinDuration < 1 {
		t.Errorf("MinDuration = %d, want the registry minimum in years", avail.MinDuration)
	}
	// The mock fills the nested price strings with the literal placeholder
	// "string", so these can only be checked for presence — asserting a
	// parseable amount here would fail against the mock forever.
	if avail.Renewal.Price == "" || avail.Transfer.Price == "" {
		t.Errorf("additional pricing did not decode: renewal=%+v transfer=%+v", avail.Renewal, avail.Transfer)
	}
}
