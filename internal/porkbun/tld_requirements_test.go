package porkbun

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// Tier 1: prove the decode agrees with the body the live mock actually
// serves. The mock's values are Porkbun's editable example data, so nothing
// here asserts a value literally — it asserts that every field this client
// reads is still spelled the way it expects, and that what came out of the
// struct matches what was in the body.
func TestMockTLDRegistrationRequirements(t *testing.T) {
	c := mockClient(t)
	ctx := context.Background()

	var raw map[string]json.RawMessage
	err := c.get(ctx, "domain/getRegistrationRequirements/us", nil, &raw)
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("raw getRegistrationRequirements: %v", err)
	}

	// A rename upstream turns every attribute of the data source silently
	// null, which no value assertion would catch.
	for _, field := range []string{
		"tld", "apiRegisterable", "registrationDurationYears", "maxRegistrationYears",
		"whoisPrivacySupported", "requiresValidatedAddress", "registrantOnly",
		"requestSchema", "registryRequirements",
	} {
		if _, ok := raw[field]; !ok {
			t.Errorf("response no longer carries %q; got fields %v", field, sortedKeys(raw))
		}
	}

	got, err := c.GetTLDRegistrationRequirements(ctx, "us")
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("GetTLDRegistrationRequirements: %v", err)
	}

	if want := unquote(raw["tld"]); string(got.TLD) != want {
		t.Errorf("TLD = %q, body said %q", got.TLD, want)
	}

	// Every flag must agree with the literal in the body, whichever way it
	// reads: a decoder that never looked at the field also returns false.
	for _, tc := range []struct {
		field string
		got   bool
	}{
		{"apiRegisterable", got.APIRegisterable.Bool()},
		{"whoisPrivacySupported", got.WhoisPrivacySupported.Bool()},
		{"requiresValidatedAddress", got.RequiresValidatedAddress.Bool()},
		{"registrantOnly", got.RegistrantOnly.Bool()},
	} {
		want := unquote(raw[tc.field])
		if truthy := want == "true" || want == "1" || want == "yes"; truthy != tc.got {
			t.Errorf("%s decoded as %v, body said %q", tc.field, tc.got, want)
		}
	}

	// Both schemas are JSON documents in the mock, so both must survive as
	// something jsondecode() can read.
	for name, doc := range map[string]string{
		"requestSchema":        got.RequestSchemaJSON(),
		"registryRequirements": got.RegistryRequirementsJSON(),
	} {
		if strings.TrimSpace(string(raw[name])) == "null" {
			continue
		}
		if doc == "" {
			t.Errorf("%s did not decode: %s", name, truncate(string(raw[name]), 120))
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(doc), &parsed); err != nil {
			t.Errorf("%s is not a JSON object after compaction: %v", name, err)
		}
	}

	// maxRegistrationYears is nullable, and null must not arrive as a real 0.
	if strings.TrimSpace(string(raw["maxRegistrationYears"])) == "null" && got.MaxRegistrationYears != nil {
		t.Errorf("null maxRegistrationYears decoded as %d", got.MaxRegistrationYears.Int64())
	}
	if got.RegistrationDurationYears.Int64() <= 0 {
		t.Errorf("registrationDurationYears did not decode: body said %s", raw["registrationDurationYears"])
	}
}

// TestTLDBoolDecoding covers the spellings the mock does not serve. The spec
// types these as booleans, but Porkbun sends booleans as 0/1 and "yes"/"no"
// on other endpoints, and a flag that silently reads false would make a
// TLD look unregisterable.
func TestTLDBoolDecoding(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		`true`: true, `false`: false,
		`1`: true, `0`: false,
		`"yes"`: true, `"no"`: false,
		`"true"`: true, `"1"`: true,
		`null`: false,
	}
	for in, want := range cases {
		var b tldBool
		if err := json.Unmarshal([]byte(in), &b); err != nil {
			t.Errorf("unmarshal %s: %v", in, err)
			continue
		}
		if b.Bool() != want {
			t.Errorf("%s decoded as %v, want %v", in, b.Bool(), want)
		}
	}

	var b tldBool
	if err := json.Unmarshal([]byte(`"maybe"`), &b); err == nil {
		t.Error("a non-boolean string should fail rather than read as false")
	}
}

// A null maxRegistrationYears means "no stated maximum" and a 0 means the
// opposite, so the pointer must survive the difference.
func TestTLDRequirementsNullVersusZero(t *testing.T) {
	t.Parallel()

	var withNull TLDRegistrationRequirements
	if err := json.Unmarshal([]byte(`{"maxRegistrationYears":null,"registryRequirements":null}`), &withNull); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if withNull.MaxRegistrationYears != nil {
		t.Errorf("null decoded to %d, want nil", withNull.MaxRegistrationYears.Int64())
	}
	if got := withNull.RegistryRequirementsJSON(); got != "" {
		t.Errorf("null registryRequirements decoded to %q", got)
	}

	var withZero TLDRegistrationRequirements
	if err := json.Unmarshal([]byte(`{"maxRegistrationYears":0}`), &withZero); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if withZero.MaxRegistrationYears == nil || withZero.MaxRegistrationYears.Int64() != 0 {
		t.Errorf("a stated 0 must not be indistinguishable from null: %v", withZero.MaxRegistrationYears)
	}

	var withTen TLDRegistrationRequirements
	if err := json.Unmarshal([]byte(`{"maxRegistrationYears":"10","registryRequirements":{"type":"object"}}`), &withTen); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if withTen.MaxRegistrationYears == nil || withTen.MaxRegistrationYears.Int64() != 10 {
		t.Errorf("a stringified 10 did not decode: %v", withTen.MaxRegistrationYears)
	}
	if got := withTen.RegistryRequirementsJSON(); got != `{"type":"object"}` {
		t.Errorf("registryRequirements = %q", got)
	}
}

func sortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func unquote(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str
	}
	return s
}
