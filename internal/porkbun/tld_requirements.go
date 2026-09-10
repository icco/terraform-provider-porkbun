package porkbun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// tldBool decodes a Porkbun flag that the spec types as a boolean but which
// the API is free to send as 0/1 or "yes"/"no", as it does elsewhere. Null
// and absent decode to false, which is what every flag here means when
// unstated.
//
// It is named for this file rather than generically because the shared
// types.go is off limits to a single-endpoint change.
type tldBool bool

func (b *tldBool) UnmarshalJSON(raw []byte) error {
	s := strings.ToLower(strings.Trim(strings.TrimSpace(string(raw)), `"`))
	switch s {
	case "", "null", "0", "false", "no", "off":
		*b = false
		return nil
	case "1", "true", "yes", "on":
		*b = true
		return nil
	}
	// Any other number is truthy; anything else is a shape change worth
	// failing on rather than silently reading as false.
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return fmt.Errorf("porkbun: %s is not a boolean", truncate(s, 64))
	}
	f, err := n.Float64()
	if err != nil {
		return err
	}
	*b = f != 0
	return nil
}

// Bool returns the decoded flag.
func (b tldBool) Bool() bool { return bool(b) }

// TLDRegistrationRequirements is the response of
// /domain/getRegistrationRequirements/{tld}: what a TLD demands of a
// registrant, and whether the API can register it at all.
type TLDRegistrationRequirements struct {
	TLD flexString `json:"tld"`
	// APIRegisterable is false for TLDs whose registry eligibility data the
	// API cannot submit; those are website-only registrations.
	APIRegisterable tldBool `json:"apiRegisterable"`
	// NotAPIRegisterableReason is sent only when APIRegisterable is false.
	NotAPIRegisterableReason flexString `json:"notApiRegisterableReason"`
	// RegistrationDurationYears is the fixed term the API registers for.
	RegistrationDurationYears flexInt `json:"registrationDurationYears"`
	// MaxRegistrationYears is nil when Porkbun sent null or omitted the
	// field, which the spec defines as "unspecified". A pointer keeps that
	// distinct from a real 0, which would mean the opposite.
	MaxRegistrationYears     *flexInt `json:"maxRegistrationYears"`
	WhoisPrivacySupported    tldBool  `json:"whoisPrivacySupported"`
	RequiresValidatedAddress tldBool  `json:"requiresValidatedAddress"`
	RegistrantOnly           tldBool  `json:"registrantOnly"`
	// RequestSchema and RegistryRequirements are arbitrary JSON Schema
	// documents, kept raw. Modelling them field by field would pin this
	// client to one version of a schema whose whole purpose is to vary per
	// TLD and change with registry policy.
	RequestSchema        json.RawMessage `json:"requestSchema"`
	RegistryRequirements json.RawMessage `json:"registryRequirements"`
}

// RequestSchemaJSON returns the /domain/create request schema as compact
// JSON, or "" when Porkbun sent nothing.
func (r *TLDRegistrationRequirements) RequestSchemaJSON() string {
	return tldCompactJSON(r.RequestSchema)
}

// RegistryRequirementsJSON returns the registry eligibility schema as
// compact JSON, or "" when the TLD has no structured extra requirements.
func (r *TLDRegistrationRequirements) RegistryRequirementsJSON() string {
	return tldCompactJSON(r.RegistryRequirements)
}

// tldCompactJSON normalises a raw JSON document to one line. An absent field
// and a JSON null both come back as "", so callers can tell "no schema" from
// a document; whitespace is stripped so a reformat upstream is not churn.
func tldCompactJSON(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		// Unreachable for anything that decoded, but returning the original
		// beats returning "" and claiming the API sent no schema.
		return s
	}
	return buf.String()
}

// GetTLDRegistrationRequirements reads the registration requirements for one
// TLD. It needs no domain, so it works before anything is registered.
//
// The path segment is the bare TLD (`us`, `co.uk`); a leading dot is trimmed
// here so a caller that wrote ".us" gets an answer rather than an
// unknown-TLD error from a path Porkbun never documented accepting.
func (c *Client) GetTLDRegistrationRequirements(ctx context.Context, tld string) (*TLDRegistrationRequirements, error) {
	tld = strings.TrimPrefix(strings.TrimSpace(tld), ".")

	var out TLDRegistrationRequirements
	if err := c.get(ctx, "domain/getRegistrationRequirements/"+escapePath(tld), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
