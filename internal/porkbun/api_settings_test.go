package porkbun

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMockAPISettings is the tier-1 decode test: it runs against Porkbun's
// live /mock endpoint, so it is the only thing here that notices if
// /account/apiSettings changes shape.
func TestMockAPISettings(t *testing.T) {
	c := mockClient(t)

	got, err := c.GetAPISettings(context.Background())
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("GetAPISettings: %v", err)
	}
	// The mock serves autoTopup as a JSON boolean, which is what makes
	// flexInt unusable for it; a decode regression surfaces as an error
	// above rather than a wrong value, so assert only what the placeholder
	// data can promise.
	if got.MonthlySpend < 0 {
		t.Errorf("monthlySpend did not decode: %+v", got)
	}
	for name, v := range map[string]*int64{
		"monthlySpendLimit": got.MonthlySpendLimit,
		"lowBalanceAlert":   got.LowBalanceAlert,
		"topupThreshold":    got.TopupThreshold,
		"topupAmount":       got.TopupAmount,
	} {
		if v == nil {
			t.Errorf("%s came back nil; the mock sends a number for every limit", name)
		}
	}
}

func TestGetAPISettingsUsesGET(t *testing.T) {
	t.Parallel()

	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"status":"SUCCESS","settings":{"monthlySpendLimit":50000,` +
			`"lowBalanceAlert":1000,"autoTopup":true,"topupThreshold":500,"topupAmount":10000},` +
			`"monthlySpend":1234}`))
	}))
	defer srv.Close()

	got, err := testClient(t, srv.URL).GetAPISettings(context.Background())
	if err != nil {
		t.Fatalf("GetAPISettings: %v", err)
	}
	if method != http.MethodGet {
		t.Errorf("reads should be GET, got %s", method)
	}
	if path != "/account/apiSettings" {
		t.Errorf("path = %s", path)
	}
	if got.MonthlySpendLimit == nil || *got.MonthlySpendLimit != 50000 {
		t.Errorf("MonthlySpendLimit = %v", got.MonthlySpendLimit)
	}
	if !got.AutoTopup {
		t.Error("autoTopup: true should decode as true")
	}
	if got.MonthlySpend != 1234 {
		t.Errorf("MonthlySpend = %d", got.MonthlySpend)
	}
}

// TestGetAPISettingsNullsAreNotZero pins the distinction the pointers exist
// for: null means "no limit", 0 means "no spending allowed". A decoder that
// folded null into 0 would report a locked-down account as unlimited.
func TestGetAPISettingsNullsAreNotZero(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"SUCCESS","settings":{"monthlySpendLimit":null,` +
			`"lowBalanceAlert":null,"autoTopup":0,"topupAmount":0},"monthlySpend":"0"}`))
	}))
	defer srv.Close()

	got, err := testClient(t, srv.URL).GetAPISettings(context.Background())
	if err != nil {
		t.Fatalf("GetAPISettings: %v", err)
	}
	if got.MonthlySpendLimit != nil {
		t.Errorf("explicit null must stay nil, got %d", *got.MonthlySpendLimit)
	}
	if got.LowBalanceAlert != nil {
		t.Errorf("explicit null must stay nil, got %d", *got.LowBalanceAlert)
	}
	// topupThreshold is absent from the body, not null; both mean "unset".
	if got.TopupThreshold != nil {
		t.Errorf("an absent field must stay nil, got %d", *got.TopupThreshold)
	}
	if got.TopupAmount == nil || *got.TopupAmount != 0 {
		t.Errorf("a real 0 must survive as 0, got %v", got.TopupAmount)
	}
	if got.AutoTopup {
		t.Error("autoTopup: 0 should decode as false")
	}
}

// TestGetAPISettingsIntegerBooleanForm covers the 0/1 spelling. The spec
// types autoTopup as a boolean, but the rest of this API sends flags as
// integers, and Porkbun accepts the string forms on input.
func TestGetAPISettingsIntegerBooleanForm(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`1`, `"1"`, `true`, `"true"`, `"yes"`, `"on"`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"SUCCESS","settings":{"autoTopup":` + body + `},"monthlySpend":0}`))
		}))
		got, err := testClient(t, srv.URL).GetAPISettings(context.Background())
		srv.Close()
		if err != nil {
			t.Fatalf("GetAPISettings with autoTopup %s: %v", body, err)
		}
		if !got.AutoTopup {
			t.Errorf("autoTopup %s decoded as false", body)
		}
	}

	for _, body := range []string{`0`, `"0"`, `false`, `"false"`, `null`, `""`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"SUCCESS","settings":{"autoTopup":` + body + `},"monthlySpend":0}`))
		}))
		got, err := testClient(t, srv.URL).GetAPISettings(context.Background())
		srv.Close()
		if err != nil {
			t.Fatalf("GetAPISettings with autoTopup %s: %v", body, err)
		}
		if got.AutoTopup {
			t.Errorf("autoTopup %s decoded as true", body)
		}
	}
}
