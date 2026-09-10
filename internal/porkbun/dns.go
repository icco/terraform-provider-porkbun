package porkbun

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

// RecordTypes is the set of DNS record types Porkbun accepts, taken from the
// published OpenAPI enum.
var RecordTypes = []string{
	"A", "AAAA", "MX", "CNAME", "ALIAS", "TXT", "NS", "SRV", "TLSA", "CAA", "SSHFP", "HTTPS", "SVCB",
}

// Record is a DNS record as Porkbun returns it from /dns/retrieve.
//
// The API is internally inconsistent about types: writes take ttl and prio as
// integers, reads hand them back as strings (and prio may be null). flexInt
// absorbs the difference so the provider can expose plain numbers and avoid a
// permanent diff on every record.
type Record struct {
	ID string `json:"id"`
	// Name is fully qualified on read ("www.example.com"), but writes take the
	// subdomain only ("www"). Use SubdomainOf to convert.
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	Content string  `json:"content"`
	TTL     flexInt `json:"ttl"`
	Prio    flexInt `json:"prio"`
	Notes   string  `json:"notes"`
}

// RecordInput is the payload for creating or editing a record.
type RecordInput struct {
	// Name is the subdomain only, empty for the zone apex.
	Name    string
	Type    string
	Content string
	TTL     int64
	Prio    int64
	Notes   string
}

func (r RecordInput) body() map[string]any {
	body := map[string]any{
		"name":    r.Name,
		"type":    r.Type,
		"content": r.Content,
		"prio":    r.Prio,
		"notes":   r.Notes,
	}
	if r.TTL > 0 {
		body["ttl"] = r.TTL
	}
	return body
}

// CreateRecord creates a DNS record and returns its Porkbun id.
//
// On a duplicate, Porkbun answers with code DUPLICATE_RECORD and puts the id
// of the record that already exists in existingId; that id is returned
// alongside the error so a diagnostic can name it.
func (c *Client) CreateRecord(ctx context.Context, domain string, in RecordInput) (id string, existingID string, err error) {
	var out struct {
		ID flexString `json:"id"`
	}
	if err := c.post(ctx, "dns/create/"+escapePath(domain), in.body(), &out); err != nil {
		return "", ExistingRecordID(err), err
	}
	return string(out.ID), "", nil
}

// ExistingRecordID pulls the existingId out of a DUPLICATE_RECORD error, or
// returns "" when the error is anything else.
func ExistingRecordID(err error) string {
	var apiErr *Error
	if !as(err, &apiErr) || apiErr.Code != "DUPLICATE_RECORD" || len(apiErr.Raw) == 0 {
		return ""
	}
	var dup struct {
		ExistingID flexString `json:"existingId"`
	}
	if json.Unmarshal(apiErr.Raw, &dup) != nil {
		return ""
	}
	return string(dup.ExistingID)
}

// EditRecord replaces the contents of an existing record.
func (c *Client) EditRecord(ctx context.Context, domain, id string, in RecordInput) error {
	return c.post(ctx, "dns/edit/"+escapePath(domain)+"/"+escapePath(id), in.body(), nil)
}

// DeleteRecord removes a record.
func (c *Client) DeleteRecord(ctx context.Context, domain, id string) error {
	return c.post(ctx, "dns/delete/"+escapePath(domain)+"/"+escapePath(id), map[string]any{}, nil)
}

// RetrieveRecords lists every record in the Porkbun-hosted zone.
func (c *Client) RetrieveRecords(ctx context.Context, domain string) ([]Record, error) {
	var out struct {
		Records []Record `json:"records"`
	}
	if err := c.get(ctx, "dns/retrieve/"+escapePath(domain), nil, &out); err != nil {
		return nil, err
	}
	return out.Records, nil
}

// RetrieveRecord fetches one record by id. Porkbun answers with a records
// array even for a single id, and an empty array means the record is gone —
// not an error. The second return value reports whether it was found.
func (c *Client) RetrieveRecord(ctx context.Context, domain, id string) (*Record, bool, error) {
	var out struct {
		Records []Record `json:"records"`
	}
	err := c.get(ctx, "dns/retrieve/"+escapePath(domain)+"/"+escapePath(id), nil, &out)
	if err != nil {
		if IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if len(out.Records) == 0 {
		return nil, false, nil
	}
	rec := out.Records[0]
	return &rec, true, nil
}

// SubdomainOf converts a fully-qualified record name as returned by
// /dns/retrieve into the subdomain form that /dns/create and /dns/edit
// expect: "www.example.com" with domain "example.com" becomes "www", and the
// apex becomes "".
func SubdomainOf(fqdn, domain string) string {
	f := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(fqdn), "."))
	d := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if f == d || f == "" {
		return ""
	}
	if strings.HasSuffix(f, "."+d) {
		return strings.TrimSuffix(f, "."+d)
	}
	return f
}

// ParseRecordID validates a Porkbun record id, which is a decimal integer
// carried as a string.
func ParseRecordID(id string) (string, error) {
	trimmed := strings.TrimSpace(id)
	if _, err := strconv.ParseInt(trimmed, 10, 64); err != nil {
		return "", err
	}
	return trimmed, nil
}
