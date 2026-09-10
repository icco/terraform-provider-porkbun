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
	// The mock answers avail "yes". A decode regression on the textual
	// booleans shows up here as false, which reads as "not for sale".
	if !avail.Available {
		t.Error("Available = false, but the mock answers avail \"yes\"")
	}
	if !avail.Premium || !avail.FirstYearPromo {
		t.Errorf("the mock answers yes to premium and firstYearPromo, got %+v", avail)
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
