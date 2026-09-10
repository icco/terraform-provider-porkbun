package porkbun

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

// ScannedInt is a nullable integer in a scan response.
//
// flexInt maps null, "" and an absent field all to zero, which is the right
// default for a write but a lie on a read: a scanned A record carries no
// priority at all, and a stored 0 claims the nameserver answered with one.
// Named for this surface so it cannot collide with the same idea landing in
// another file of this package.
type ScannedInt struct {
	value int64
	set   bool
}

// A value receiver here would compile and then silently never run.
var _ json.Unmarshaler = (*ScannedInt)(nil)

// Present reports whether the API actually sent a number.
func (s ScannedInt) Present() bool { return s.set }

// Int64 returns the value, which is meaningless unless Present.
func (s ScannedInt) Int64() int64 { return s.value }

func (s *ScannedInt) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "null" || trimmed == `""` || trimmed == "" {
		*s = ScannedInt{}
		return nil
	}
	var f flexInt
	if err := f.UnmarshalJSON(b); err != nil {
		return err
	}
	*s = ScannedInt{value: f.Int64(), set: true}
	return nil
}

// ScannedRecord is one record the domain's live authoritative nameservers
// answered with. It is deliberately not a Record: a scan reads DNS, not the
// Porkbun zone, so there is no Porkbun record id and no notes field.
type ScannedRecord struct {
	// Name is the subdomain only, empty at the apex — the form RecordInput
	// and porkbun_dns_record take, not the fully-qualified form
	// /dns/retrieve hands back. The spec documents this field as accepting
	// either on input, so ScanDNS normalizes whatever comes back.
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	// TTL and Prio are absent on records that have neither. Callers must
	// keep "the nameserver did not say" distinct from a real zero.
	TTL  ScannedInt `json:"ttl"`
	Prio ScannedInt `json:"prio"`
}

// scanName reduces a scanned record's name to the subdomain form.
//
// Porkbun documents `@`, a bare subdomain and a fully-qualified name as
// interchangeable spellings of this field, so a scan is free to answer with
// any of them; SubdomainOf handles the last two and `@` is handled here.
// Without this the apex arrives as three different keys and drift
// comparison against porkbun_dns_record.name silently misses records.
func scanName(name, domain string) string {
	if strings.TrimSpace(name) == "@" {
		return ""
	}
	return SubdomainOf(name, domain)
}

// ScanResult is one /dns/scan response.
type ScanResult struct {
	// Domain is the name the API echoed back.
	Domain string
	// RecordCount is the count the API reported, which is not necessarily
	// len(Records): it is the API's own summary of the same scan.
	RecordCount ScannedInt
	Records     []ScannedRecord
}

// ScanDNS queries a domain's live authoritative nameservers and returns the
// records they answer with. It writes nothing.
//
// This is not /dns/retrieve. Retrieve reads the zone Porkbun stores; a scan
// reads what the delegation actually publishes today, which during an
// inbound transfer is the other registrar's zone and is the only copy of it
// that survives the move.
//
// The scan probes a list of well-known names and consolidates wildcards. It
// cannot enumerate a zone — DNS has no listing operation and AXFR is
// universally refused — so an empty or short result means nothing further
// was found, never that nothing further exists.
//
// Records come back sorted by name, type then content: the API's own
// ordering is not documented as stable, and an unstable order would churn
// every plan that reads this.
func (c *Client) ScanDNS(ctx context.Context, domain string) (*ScanResult, error) {
	var out struct {
		Domain      string          `json:"domain"`
		RecordCount ScannedInt      `json:"recordCount"`
		Records     []ScannedRecord `json:"records"`
	}
	if err := c.get(ctx, "dns/scan/"+escapePath(domain), nil, &out); err != nil {
		return nil, err
	}

	records := make([]ScannedRecord, 0, len(out.Records))
	for _, r := range out.Records {
		r.Name = scanName(r.Name, domain)
		records = append(records, r)
	}
	sort.SliceStable(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.Content < b.Content
	})

	return &ScanResult{Domain: out.Domain, RecordCount: out.RecordCount, Records: records}, nil
}
