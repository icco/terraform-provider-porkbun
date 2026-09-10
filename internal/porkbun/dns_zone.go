package porkbun

import (
	"context"
	"encoding/json"
	"strings"
)

// cloudflareEnabled is the one value of the zone-level `cloudflare` field
// that means the proxy is on. The spec enumerates "enabled" and "disabled".
const cloudflareEnabled = "enabled"

// Zone is a whole /dns/retrieve response: the editable records plus the
// zone-level fields the record-only helpers in dns.go discard.
//
// RetrieveRecords stays the right call when only the records matter, and the
// resource uses it. This exists because `cloudflare` is a sibling of
// `records` rather than a field on one, and it decides whether any of those
// records are load-bearing: after a domain moves to Cloudflare, Porkbun keeps
// serving its own copy of the zone from /dns/retrieve while Cloudflare is
// what the internet actually queries.
type Zone struct {
	// Cloudflare is "enabled" or "disabled", or "" when the response carried
	// no such field. Empty is not "disabled" — it means Porkbun did not say.
	Cloudflare string       `json:"cloudflare"`
	Records    []ZoneRecord `json:"records"`
}

// ProxiedByCloudflare reports whether the Cloudflare proxy is on, and whether
// Porkbun reported the field at all. An unreported flag must not be rendered
// as false: "we don't know" and "no" send a reader to different places.
func (z *Zone) ProxiedByCloudflare() (enabled, reported bool) {
	if strings.TrimSpace(z.Cloudflare) == "" {
		return false, false
	}
	return strings.EqualFold(strings.TrimSpace(z.Cloudflare), cloudflareEnabled), true
}

// ZoneRecord is a Record plus which of its nullable fields Porkbun actually
// sent. `prio` and `notes` are documented nullable and flexInt/string decode
// null to the zero value, which is correct for the resource — it writes a
// prio of 0 and gets null back — but wrong for a read-only surface, where
// reporting a null prio as 0 asserts a priority the API never gave.
type ZoneRecord struct {
	Record

	TTLSet   bool
	PrioSet  bool
	NotesSet bool
}

func (z *ZoneRecord) UnmarshalJSON(b []byte) error {
	// Into the embedded Record, not into z: Record has no UnmarshalJSON of
	// its own, so this decodes the fields normally and cannot recurse.
	if err := json.Unmarshal(b, &z.Record); err != nil {
		return err
	}

	var probe struct {
		TTL   json.RawMessage `json:"ttl"`
		Prio  json.RawMessage `json:"prio"`
		Notes json.RawMessage `json:"notes"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	z.TTLSet = jsonValuePresent(probe.TTL)
	z.PrioSet = jsonValuePresent(probe.Prio)
	z.NotesSet = jsonValuePresent(probe.Notes)
	return nil
}

// jsonValuePresent reports whether a field arrived carrying a value. Absent,
// null and "" all count as unset: Porkbun uses null for an unset prio but has
// been seen to use the empty string too, and neither is a priority.
func jsonValuePresent(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != `""`
}

// RetrieveZone reads every editable DNS record in the Porkbun-hosted zone,
// together with the zone-level cloudflare flag.
//
// The answer is not the whole zone: Porkbun excludes SOA records and its own
// default NS records from /dns/retrieve, so a caller must not treat this as
// an exhaustive picture of what resolves.
func (c *Client) RetrieveZone(ctx context.Context, domain string) (*Zone, error) {
	var zone Zone
	if err := c.get(ctx, "dns/retrieve/"+escapePath(domain), nil, &zone); err != nil {
		return nil, err
	}
	return &zone, nil
}

// RetrieveZoneRecord fetches one record by id, with the same zone-level
// fields. Porkbun answers with a records array even for a single id, and an
// empty array rather than an error is how it says the record is gone, so
// callers must check len(Records) as well as err.
func (c *Client) RetrieveZoneRecord(ctx context.Context, domain, id string) (*Zone, error) {
	var zone Zone
	if err := c.get(ctx, "dns/retrieve/"+escapePath(domain)+"/"+escapePath(id), nil, &zone); err != nil {
		return nil, err
	}
	return &zone, nil
}
