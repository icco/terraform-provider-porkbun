package porkbun

import (
	"context"
	"encoding/json"
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
		MonthlySpendLimit optionalInt `json:"monthlySpendLimit"`
		LowBalanceAlert   optionalInt `json:"lowBalanceAlert"`
		// The spec types autoTopup as a boolean and the mock sends one, but
		// every other boolean-ish field on this API arrives as a 0/1
		// integer, and flexInt cannot decode `true`. flexString keeps
		// whichever literal turns up; apiSettingsBool interprets it.
		AutoTopup      flexString  `json:"autoTopup"`
		TopupThreshold optionalInt `json:"topupThreshold"`
		TopupAmount    optionalInt `json:"topupAmount"`
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

// optionalInt decodes a nullable amount while keeping "no value" distinct
// from zero. The distinction is load-bearing and inverted: on a spend
// limit, null means *no limit* and 0 means *no spending permitted*, so
// collapsing one into the other reports an account that can spend nothing
// as unlimited, or the reverse.
//
// A *flexInt is not enough. It separates null and absent from a number,
// because encoding/json nils a settable pointer on null without consulting
// the element's UnmarshalJSON — but an empty string still routes through
// flexInt, which maps "" to 0, allocating a pointer to a zero that the API
// never sent.
type optionalInt struct {
	Valid bool
	Value int64
}

func (o *optionalInt) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	// A quoted empty or whitespace-only string is "unset", not zero.
	if trimmed[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		if strings.TrimSpace(str) == "" {
			return nil
		}
	}
	var f flexInt
	if err := f.UnmarshalJSON(b); err != nil {
		return err
	}
	o.Valid, o.Value = true, f.Int64()
	return nil
}

// apiSettingsAmount converts a decoded nullable amount, preserving the
// null/zero distinction.
func apiSettingsAmount(v optionalInt) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Value
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
