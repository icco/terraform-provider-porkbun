package porkbun

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestMockRetrieveZone checks the decode against whatever /mock is serving
// today rather than against the placeholder values it happens to contain.
// Porkbun edits its own example data; an assertion like `cloudflare ==
// "enabled"` would turn that into a red build here.
func TestMockRetrieveZone(t *testing.T) {
	c := mockClient(t)
	ctx := context.Background()

	var raw map[string]json.RawMessage
	err := c.get(ctx, "dns/retrieve/example.com", nil, &raw)
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("raw dns/retrieve: %v", err)
	}

	// The zone-level flag is the whole reason RetrieveZone exists. If Porkbun
	// renames or drops it, cloudflare_enabled silently goes null forever and
	// nothing else notices.
	rawCloudflare, ok := raw["cloudflare"]
	if !ok {
		t.Error("dns/retrieve no longer carries a top-level `cloudflare` field")
	}
	if _, ok := raw["records"]; !ok {
		t.Fatal("dns/retrieve no longer carries a `records` array")
	}

	zone, err := c.RetrieveZone(ctx, "example.com")
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("RetrieveZone: %v", err)
	}

	// Agree with whatever the raw body said, rather than with a fixed value.
	var wantCloudflare string
	if ok {
		if err := json.Unmarshal(rawCloudflare, &wantCloudflare); err != nil {
			t.Errorf("cloudflare is no longer a JSON string: %s", rawCloudflare)
		}
	}
	if zone.Cloudflare != wantCloudflare {
		t.Errorf("Cloudflare = %q, raw body said %q", zone.Cloudflare, wantCloudflare)
	}
	enabled, reported := zone.ProxiedByCloudflare()
	if wantEnabled := strings.EqualFold(wantCloudflare, "enabled"); enabled != wantEnabled {
		t.Errorf("ProxiedByCloudflare() = %v, want %v for %q", enabled, wantEnabled, wantCloudflare)
	}
	if wantReported := wantCloudflare != ""; reported != wantReported {
		t.Errorf("ProxiedByCloudflare() reported = %v, want %v", reported, wantReported)
	}

	if len(zone.Records) == 0 {
		t.Fatal("expected at least one record from the mock")
	}
	rec := zone.Records[0]
	if rec.Type == "" || rec.Content == "" || rec.Name == "" {
		t.Errorf("record did not decode: %+v", rec)
	}
	// ttl is a string on read and an integer on write; a decode regression
	// there gives every record a permanent diff.
	if rec.TTLSet && rec.TTL.Int64() == 0 {
		t.Errorf("ttl was present but decoded to 0: %+v", rec)
	}

	// The presence flags must track the raw record, not the Go zero value.
	var rawRecords []map[string]json.RawMessage
	if err := json.Unmarshal(raw["records"], &rawRecords); err != nil {
		t.Fatalf("decoding raw records: %v", err)
	}
	if len(rawRecords) != len(zone.Records) {
		t.Fatalf("decoded %d records from a body carrying %d", len(zone.Records), len(rawRecords))
	}
	for i, rawRec := range rawRecords {
		got := zone.Records[i]
		for _, field := range []struct {
			name string
			set  bool
		}{
			{"ttl", got.TTLSet},
			{"prio", got.PrioSet},
			{"notes", got.NotesSet},
		} {
			if want := jsonValuePresent(rawRec[field.name]); field.set != want {
				t.Errorf("record %d: %sSet = %v, raw value was %s", i, field.name, field.set, rawRec[field.name])
			}
		}
	}
}

// TestMockRetrieveZoneRecord covers the by-ID form, which shares the response
// schema but is a different route.
func TestMockRetrieveZoneRecord(t *testing.T) {
	c := mockClient(t)

	zone, err := c.RetrieveZoneRecord(context.Background(), "example.com", "123456789")
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("RetrieveZoneRecord: %v", err)
	}
	if len(zone.Records) == 0 {
		t.Fatal("expected the mock to return the requested record")
	}
	if zone.Records[0].Type == "" {
		t.Errorf("record did not decode: %+v", zone.Records[0])
	}
}

// TestZoneRecordNullFieldsDecode pins the null handling on hand-written
// bodies, so the case the mock does not happen to serve is still covered:
// the mock sends a non-null prio and notes, and a presence flag that is
// always true proves nothing.
func TestZoneRecordNullFieldsDecode(t *testing.T) {
	t.Parallel()

	body := `{
	  "status": "SUCCESS",
	  "cloudflare": "enabled",
	  "records": [
	    {"id":"1","name":"example.com","type":"A","content":"1.2.3.4","ttl":"600","prio":null,"notes":null},
	    {"id":"2","name":"example.com","type":"MX","content":"mx.example.com","ttl":"300","prio":"10","notes":"primary"},
	    {"id":"3","name":"www.example.com","type":"CNAME","content":"example.com","ttl":"600","prio":"","notes":""}
	  ]
	}`

	var zone Zone
	if err := json.Unmarshal([]byte(body), &zone); err != nil {
		t.Fatalf("decoding zone: %v", err)
	}

	if enabled, reported := zone.ProxiedByCloudflare(); !enabled || !reported {
		t.Errorf("ProxiedByCloudflare() = %v, %v for %q", enabled, reported, zone.Cloudflare)
	}

	if got := zone.Records[0]; got.PrioSet || got.NotesSet || !got.TTLSet {
		t.Errorf("null prio/notes must not read as set: %+v", got)
	}
	// The one that must be non-zero: a true flag with a real value behind it.
	if got := zone.Records[1]; !got.PrioSet || got.Prio.Int64() != 10 {
		t.Errorf("prio = %d (set %v), want 10 set", got.Prio.Int64(), got.PrioSet)
	}
	if got := zone.Records[1]; !got.NotesSet || got.Notes != "primary" {
		t.Errorf("notes = %q (set %v), want \"primary\" set", got.Notes, got.NotesSet)
	}
	// An empty string is not a priority either.
	if got := zone.Records[2]; got.PrioSet || got.NotesSet {
		t.Errorf("empty-string prio/notes must not read as set: %+v", got)
	}

	var missing Zone
	if err := json.Unmarshal([]byte(`{"status":"SUCCESS","records":[{"id":"9","name":"example.com","type":"A","content":"1.2.3.4"}]}`), &missing); err != nil {
		t.Fatalf("decoding zone without cloudflare: %v", err)
	}
	if _, reported := missing.ProxiedByCloudflare(); reported {
		t.Error("an absent cloudflare field must not be reported as a value")
	}
	if got := missing.Records[0]; got.TTLSet || got.PrioSet || got.NotesSet {
		t.Errorf("absent fields must not read as set: %+v", got)
	}
}
