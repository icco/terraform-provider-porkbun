package porkbun

import (
	"context"
	"strings"
)

// APISettings is the account's API spend controls plus the current calendar
// month's API spend, from /account/apiSettings. Every amount is in cents.
//
// The limits are pointers because null is not zero here. Porkbun sends null
// (or omits the field) for "no limit" / "disabled", and a limit of 0 cents
// would mean the opposite: no API spending permitted at all. Collapsing the
// two would report an account with a hard stop as unlimited.
type APISettings struct {
	// MonthlySpendLimit caps API spend per calendar month. Nil means no cap.
	MonthlySpendLimit *int64
	// LowBalanceAlert is the balance below which Porkbun emails. Nil means
	// the alert is off.
	LowBalanceAlert *int64
	// AutoTopup reports whether automatic balance top-up is on.
	AutoTopup bool
	// TopupThreshold is the balance that triggers an auto top-up. Nil means
	// no threshold is configured.
	TopupThreshold *int64
	// TopupAmount is what an auto top-up adds. Nil means unset.
	TopupAmount *int64
	// MonthlySpend is API spend so far this calendar month.
	MonthlySpend int64
}

type apiSettingsResponse struct {
	Settings struct {
		MonthlySpendLimit *flexInt `json:"monthlySpendLimit"`
		LowBalanceAlert   *flexInt `json:"lowBalanceAlert"`
		// The spec types autoTopup as a boolean and the mock sends one, but
		// every other boolean-ish field on this API arrives as a 0/1
		// integer, and flexInt cannot decode `true`. flexString keeps
		// whichever literal turns up; apiSettingsBool interprets it.
		AutoTopup      flexString `json:"autoTopup"`
		TopupThreshold *flexInt   `json:"topupThreshold"`
		TopupAmount    *flexInt   `json:"topupAmount"`
	} `json:"settings"`
	MonthlySpend flexInt `json:"monthlySpend"`
}

// GetAPISettings reads the account's API spend controls. These are
// account-wide, not per key, and Porkbun enforces them on registrations,
// renewals and transfers.
func (c *Client) GetAPISettings(ctx context.Context) (*APISettings, error) {
	var out apiSettingsResponse
	if err := c.get(ctx, "account/apiSettings", nil, &out); err != nil {
		return nil, err
	}
	return &APISettings{
		MonthlySpendLimit: apiSettingsAmount(out.Settings.MonthlySpendLimit),
		LowBalanceAlert:   apiSettingsAmount(out.Settings.LowBalanceAlert),
		AutoTopup:         apiSettingsBool(out.Settings.AutoTopup),
		TopupThreshold:    apiSettingsAmount(out.Settings.TopupThreshold),
		TopupAmount:       apiSettingsAmount(out.Settings.TopupAmount),
		MonthlySpend:      out.MonthlySpend.Int64(),
	}, nil
}

// apiSettingsAmount converts a decoded nullable amount, preserving the
// null/zero distinction the pointer exists for. A *flexInt is nil for both
// an explicit null and an absent field: encoding/json nils a settable
// pointer on null without consulting the element's UnmarshalJSON.
func apiSettingsAmount(v *flexInt) *int64 {
	if v == nil {
		return nil
	}
	n := v.Int64()
	return &n
}

// apiSettingsBool reads a flag Porkbun may send as a JSON boolean, a 0/1
// integer, or any of the string spellings it accepts on input ("on",
// "yes", "true", "1").
func apiSettingsBool(v flexString) bool {
	switch strings.ToLower(strings.TrimSpace(string(v))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
