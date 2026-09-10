package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeAPI is a small stateful stand-in for the Porkbun API.
//
// Porkbun's own /mock endpoint cannot be used for resource lifecycle tests:
// it is stateless, so a write is never reflected in the following read and
// every apply fails with "Provider produced inconsistent result after apply".
// This fake stores what it is told and — deliberately — hands it back in a
// hostile spelling: reversed order, trailing dots, mixed case. A provider
// that models nameservers as an ordered list, or that skips normalization,
// fails against it exactly the way it would fail against 27 real domains.
type fakeAPI struct {
	mu sync.Mutex

	// nameservers maps domain to the normalized set last written.
	nameservers map[string][]string
	// records maps domain to record id to record.
	records map[string]map[string]*fakeRecord
	nextID  int

	// deleteNsCalls counts attempts to unset a delegation. Nothing should
	// ever increment it: the provider's Delete is a no-op by design.
	deleteNsCalls int
}

type fakeRecord struct {
	ID      string
	Name    string
	Type    string
	Content string
	TTL     int64
	Prio    int64
	Notes   string
}

func newFakeAPI(t *testing.T) (*fakeAPI, string) {
	t.Helper()
	f := &fakeAPI{
		nameservers: map[string][]string{},
		records:     map[string]map[string]*fakeRecord{},
		nextID:      100000,
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

// seedDomain registers a domain so getNs answers for it.
func (f *fakeAPI) seedDomain(domain string, ns ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nameservers[domain] = ns
}

func (f *fakeAPI) currentNameservers(domain string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.nameservers[domain]...)
	sort.Strings(out)
	return out
}

// churnNameservers re-stores the same set in a different order, the way a
// registry is free to at any time.
func (f *fakeAPI) churnNameservers(domain string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ns := f.nameservers[domain]
	for i, j := 0, len(ns)-1; i < j; i, j = i+1, j-1 {
		ns[i], ns[j] = ns[j], ns[i]
	}
	if len(ns) > 0 {
		ns[0] = strings.ToUpper(ns[0])
	}
	f.nameservers[domain] = ns
}

func (f *fakeAPI) forgetDomain(domain string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.nameservers, domain)
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-API-Key") == "" || r.Header.Get("X-Secret-API-Key") == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_API_KEY", "Missing API credentials.")
		return
	}

	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case path == "ping":
		writeJSON(w, map[string]any{"status": "SUCCESS", "yourIp": "203.0.113.7"})

	case len(parts) == 3 && parts[0] == "domain" && parts[1] == "getNs":
		f.getNs(w, parts[2])

	case len(parts) == 3 && parts[0] == "domain" && parts[1] == "updateNs":
		f.updateNs(w, r, parts[2])

	case len(parts) == 3 && parts[0] == "domain" && parts[1] == "get":
		f.getDomain(w, parts[2])

	case path == "domain/listAll":
		f.listAll(w, r)

	case len(parts) == 3 && parts[0] == "dns" && parts[1] == "create":
		f.createRecord(w, r, parts[2])

	case len(parts) == 4 && parts[0] == "dns" && parts[1] == "edit":
		f.editRecord(w, r, parts[2], parts[3])

	case len(parts) == 4 && parts[0] == "dns" && parts[1] == "delete":
		f.deleteRecord(w, parts[2], parts[3])

	case len(parts) == 4 && parts[0] == "dns" && parts[1] == "retrieve":
		f.retrieveRecord(w, parts[2], parts[3])

	case len(parts) == 3 && parts[0] == "dns" && parts[1] == "retrieve":
		f.retrieveRecords(w, parts[2])

	default:
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "No such endpoint: "+path)
	}
}

func (f *fakeAPI) getNs(w http.ResponseWriter, domain string) {
	ns, ok := f.nameservers[domain]
	if !ok {
		writeErr(w, http.StatusBadRequest, "DOMAIN_NOT_FOUND", "Domain is not in the account.")
		return
	}
	writeJSON(w, map[string]any{"status": "SUCCESS", "ns": scramble(ns)})
}

// scramble mangles a nameserver set the way a real registry might: different
// order, trailing dots, and inconsistent case. The provider must treat the
// result as equal to what it wrote.
func scramble(ns []string) []string {
	out := make([]string, 0, len(ns))
	for i := len(ns) - 1; i >= 0; i-- {
		v := ns[i] + "."
		if i%2 == 0 {
			v = strings.ToUpper(v)
		}
		out = append(out, v)
	}
	return out
}

func (f *fakeAPI) updateNs(w http.ResponseWriter, r *http.Request, domain string) {
	var body struct {
		NS []string `json:"ns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if len(body.NS) == 0 {
		f.deleteNsCalls++
		writeErr(w, http.StatusBadRequest, "INVALID_REQUEST", "Refusing an empty nameserver list.")
		return
	}
	normalized := make([]string, 0, len(body.NS))
	for _, n := range body.NS {
		normalized = append(normalized, strings.ToLower(strings.TrimSuffix(n, ".")))
	}
	sort.Strings(normalized)
	f.nameservers[domain] = normalized
	// updateNs answers with a bare success: it never echoes what was applied.
	writeJSON(w, map[string]any{"status": "SUCCESS"})
}

func (f *fakeAPI) domainObject(domain string) map[string]any {
	return map[string]any{
		"domain":       domain,
		"status":       "ACTIVE",
		"tld":          domain[strings.LastIndex(domain, ".")+1:],
		"createDate":   "2021-01-15 10:00:00",
		"expireDate":   "2027-01-15 10:00:00",
		"securityLock": 1,
		"whoisPrivacy": 1,
		"autoRenew":    1,
		"apiAccess":    1,
		"notLocal":     1,
	}
}

func (f *fakeAPI) getDomain(w http.ResponseWriter, domain string) {
	if _, ok := f.nameservers[domain]; !ok {
		writeErr(w, http.StatusNotFound, "DOMAIN_NOT_FOUND", "Domain is not in the account.")
		return
	}
	writeJSON(w, map[string]any{"status": "SUCCESS", "domain": f.domainObject(domain)})
}

func (f *fakeAPI) listAll(w http.ResponseWriter, r *http.Request) {
	names := make([]string, 0, len(f.nameservers))
	for d := range f.nameservers {
		names = append(names, d)
	}
	sort.Strings(names)

	if want := r.URL.Query().Get("nameContains"); want != "" {
		filtered := names[:0:0]
		for _, n := range names {
			if strings.Contains(n, want) {
				filtered = append(filtered, n)
			}
		}
		names = filtered
	}

	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		out = append(out, f.domainObject(n))
	}
	writeJSON(w, map[string]any{"status": "SUCCESS", "count": len(out), "domains": out})
}

func (f *fakeAPI) decodeRecord(w http.ResponseWriter, r *http.Request) (*fakeRecord, bool) {
	var body struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Content string `json:"content"`
		TTL     int64  `json:"ttl"`
		Prio    int64  `json:"prio"`
		Notes   string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return nil, false
	}
	return &fakeRecord{
		Name: body.Name, Type: body.Type, Content: body.Content,
		TTL: body.TTL, Prio: body.Prio, Notes: body.Notes,
	}, true
}

func (f *fakeAPI) createRecord(w http.ResponseWriter, r *http.Request, domain string) {
	rec, ok := f.decodeRecord(w, r)
	if !ok {
		return
	}
	f.nextID++
	rec.ID = strconv.Itoa(f.nextID)
	if f.records[domain] == nil {
		f.records[domain] = map[string]*fakeRecord{}
	}
	f.records[domain][rec.ID] = rec
	writeJSON(w, map[string]any{"status": "SUCCESS", "id": rec.ID})
}

func (f *fakeAPI) editRecord(w http.ResponseWriter, r *http.Request, domain, id string) {
	existing, ok := f.records[domain][id]
	if !ok {
		writeErr(w, http.StatusBadRequest, "RECORD_NOT_FOUND", "No such record.")
		return
	}
	rec, ok := f.decodeRecord(w, r)
	if !ok {
		return
	}
	rec.ID = existing.ID
	f.records[domain][id] = rec
	writeJSON(w, map[string]any{"status": "SUCCESS"})
}

func (f *fakeAPI) deleteRecord(w http.ResponseWriter, domain, id string) {
	if _, ok := f.records[domain][id]; !ok {
		writeErr(w, http.StatusBadRequest, "RECORD_NOT_FOUND", "No such record.")
		return
	}
	delete(f.records[domain], id)
	writeJSON(w, map[string]any{"status": "SUCCESS"})
}

// recordJSON renders a record the way the real API does: fully-qualified
// name, ttl as a string, prio as a nullable string.
func recordJSON(domain string, rec *fakeRecord) map[string]any {
	name := domain
	if rec.Name != "" {
		name = rec.Name + "." + domain
	}
	out := map[string]any{
		"id":      rec.ID,
		"name":    name,
		"type":    rec.Type,
		"content": rec.Content,
		"ttl":     strconv.FormatInt(rec.TTL, 10),
		"notes":   rec.Notes,
	}
	if rec.Prio == 0 {
		out["prio"] = nil
	} else {
		out["prio"] = strconv.FormatInt(rec.Prio, 10)
	}
	return out
}

func (f *fakeAPI) retrieveRecord(w http.ResponseWriter, domain, id string) {
	rec, ok := f.records[domain][id]
	if !ok {
		// A missing record is an empty array, not an error.
		writeJSON(w, map[string]any{"status": "SUCCESS", "cloudflare": "disabled", "records": []any{}})
		return
	}
	writeJSON(w, map[string]any{
		"status": "SUCCESS", "cloudflare": "disabled",
		"records": []any{recordJSON(domain, rec)},
	})
}

func (f *fakeAPI) retrieveRecords(w http.ResponseWriter, domain string) {
	ids := make([]string, 0, len(f.records[domain]))
	for id := range f.records[domain] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, recordJSON(domain, f.records[domain][id]))
	}
	writeJSON(w, map[string]any{"status": "SUCCESS", "cloudflare": "disabled", "records": out})
}

func writeJSON(w http.ResponseWriter, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ERROR",
		"message": message,
		"code":    code,
		"next_action": map[string]any{
			"type": "fix_request",
			"hint": fmt.Sprintf("%s (%s)", message, code),
		},
	})
}
