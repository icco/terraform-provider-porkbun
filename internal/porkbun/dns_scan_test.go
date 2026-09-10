package porkbun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestScannedIntKeepsAbsentDistinctFromZero is the guarantee the live mock
// cannot give, because Porkbun is free to edit its own example data. A
// scanned A record has no priority; decoding that to 0 would put a value in
// state that no nameserver ever answered with.
func TestScannedIntKeepsAbsentDistinctFromZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		want    ScannedInt
	}{
		{"null", `{"prio":null}`, ScannedInt{}},
		{"absent", `{}`, ScannedInt{}},
		{"empty string", `{"prio":""}`, ScannedInt{}},
		{"real zero", `{"prio":0}`, ScannedInt{value: 0, set: true}},
		{"integer", `{"prio":10}`, ScannedInt{value: 10, set: true}},
		{"decimal string", `{"prio":"20"}`, ScannedInt{value: 20, set: true}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got struct {
				Prio ScannedInt `json:"prio"`
			}
			if err := json.Unmarshal([]byte(tc.payload), &got); err != nil {
				t.Fatalf("Unmarshal(%s): %v", tc.payload, err)
			}
			if got.Prio != tc.want {
				t.Errorf("prio = %+v, want %+v", got.Prio, tc.want)
			}
		})
	}
}

// TestScanDNSNormalizesAndSorts covers the two transformations ScanDNS makes
// on top of decoding, against the awkward shapes the spec allows: a
// fully-qualified name where the documented form is the subdomain, an apex
// spelled as the domain itself, and a ttl carried as a string.
func TestScanDNSNormalizesAndSorts(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"SUCCESS","domain":"example.com","recordCount":3,"records":[
			{"name":"www.example.com","type":"A","content":"203.0.113.10","ttl":"600","prio":null},
			{"name":"example.com","type":"MX","content":"mail.example.net","ttl":3600,"prio":10},
			{"name":"@","type":"A","content":"203.0.113.9","ttl":600}
		]}`))
	}))
	defer srv.Close()

	res, err := testClient(t, srv.URL).ScanDNS(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("ScanDNS: %v", err)
	}

	if !res.RecordCount.Present() || res.RecordCount.Int64() != 3 {
		t.Errorf("recordCount = %+v, want 3", res.RecordCount)
	}
	if len(res.Records) != 3 {
		t.Fatalf("got %d records, want 3", len(res.Records))
	}

	// Sorted by name, then type: the two apex records first, "www" last.
	wantNames := []string{"", "", "www"}
	wantTypes := []string{"A", "MX", "A"}
	for i, r := range res.Records {
		if r.Name != wantNames[i] || r.Type != wantTypes[i] {
			t.Errorf("record %d = %s/%s, want %s/%s", i, r.Name, r.Type, wantNames[i], wantTypes[i])
		}
	}

	// "@" is the apex, and an unset ttl on that record would be a decode
	// failure rather than a real absence.
	if apex := res.Records[0]; !apex.TTL.Present() || apex.TTL.Int64() != 600 || apex.Prio.Present() {
		t.Errorf("apex A record decoded as %+v; want ttl 600 and no prio", apex)
	}
	// The non-zero case: an MX priority must survive, and its ttl arrived
	// as a bare integer while the A record's arrived as a string.
	if mx := res.Records[1]; !mx.Prio.Present() || mx.Prio.Int64() != 10 || mx.TTL.Int64() != 3600 {
		t.Errorf("MX record decoded as %+v; want prio 10 and ttl 3600", mx)
	}
	if www := res.Records[2]; !www.TTL.Present() || www.TTL.Int64() != 600 || www.Prio.Present() {
		t.Errorf("www record decoded as %+v; want ttl 600 from the string form and no prio", www)
	}
}

// TestMockDNSScan is the tier-1 check against the live mock: it catches the
// API renaming a field out from under the client. It asserts what the raw
// body says rather than the placeholder values themselves, which Porkbun
// can edit at any time.
func TestMockDNSScan(t *testing.T) {
	c := mockClient(t)
	ctx := context.Background()

	var raw map[string]json.RawMessage
	err := c.get(ctx, "dns/scan/example.com", nil, &raw)
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("GET dns/scan: %v", err)
	}
	for _, key := range []string{"domain", "recordCount", "records"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("the scan response no longer carries %q: %v", key, keysOf(raw))
		}
	}

	var rawRecords []map[string]json.RawMessage
	if body, ok := raw["records"]; ok {
		if uerr := json.Unmarshal(body, &rawRecords); uerr != nil {
			t.Fatalf("records is not an array: %v", uerr)
		}
	}
	if len(rawRecords) == 0 {
		t.Fatal("expected the mock to return at least one scanned record")
	}
	// Fatal, not Error: a renamed field would otherwise fall through to a
	// decode of nil and report "unexpected end of JSON input" instead of
	// naming the field that moved.
	for _, key := range []string{"name", "type", "content", "ttl", "prio"} {
		if _, ok := rawRecords[0][key]; !ok {
			t.Fatalf("scanned records no longer carry %q: %v", key, keysOf(rawRecords[0]))
		}
	}

	res, err := c.ScanDNS(ctx, "example.com")
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("ScanDNS: %v", err)
	}
	if len(res.Records) != len(rawRecords) {
		t.Fatalf("decoded %d records from a body carrying %d", len(res.Records), len(rawRecords))
	}

	// Agreement with whatever the body actually said, so this survives
	// Porkbun editing its example data. Membership rather than position:
	// ScanDNS sorts, so sorted[0] is raw[0] only while the mock happens to
	// return a single record.
	var want ScannedRecord
	if uerr := json.Unmarshal(mustMarshal(t, rawRecords[0]), &want); uerr != nil {
		t.Fatalf("the raw record does not decode into ScannedRecord: %v", uerr)
	}
	want.Name = scanName(want.Name, "example.com")

	found := false
	for _, got := range res.Records {
		if got == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no decoded record matches the raw body's first one.\n raw %+v\ngot %+v", want, res.Records)
	}

	// The mock's example record carries a priority; if it ever stops doing
	// so this stays a decode-agreement test but exercises only the null
	// path, so say why the coverage narrowed rather than passing silently.
	if !want.Prio.Present() {
		t.Log("the mock's example record no longer carries a prio; the non-null path went untested")
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("re-encoding the raw record: %v", err)
	}
	return b
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
