package porkbun

import (
	"context"
	"encoding/json"
	"testing"
)

// rawBody fetches an endpoint without decoding it into a typed struct, so a
// test can compare what the API actually sent against what the struct made
// of it.
func rawBody(t *testing.T, c *Client, path string) map[string]json.RawMessage {
	t.Helper()
	var raw json.RawMessage
	err := c.get(context.Background(), path, nil, &raw)
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("GET %s did not return a JSON object: %v", path, err)
	}
	return fields
}

// TestMockCallerIP pins /ip. The mock's placeholder address is Porkbun's to
// change, so only the field's presence is asserted, not its value.
func TestMockCallerIP(t *testing.T) {
	c := mockClient(t)

	fields := rawBody(t, c, "ip")
	if _, ok := fields["yourIp"]; !ok {
		t.Error("/ip no longer carries yourIp; the field was renamed")
	}

	info, err := c.CallerIP(context.Background())
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("CallerIP: %v", err)
	}
	if info.YourIP == "" {
		t.Errorf("yourIp did not decode: %+v", info)
	}
	assertCredentialsFlagAgrees(t, fields, info)
}

// TestMockPingInfo pins /ping, whose only difference from /ip is the
// credentialsValid flag.
func TestMockPingInfo(t *testing.T) {
	c := mockClient(t)

	fields := rawBody(t, c, "ping")
	if _, ok := fields["yourIp"]; !ok {
		t.Error("/ping no longer carries yourIp; the field was renamed")
	}

	info, err := c.PingInfo(context.Background())
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("PingInfo: %v", err)
	}
	if info.YourIP == "" {
		t.Errorf("yourIp did not decode: %+v", info)
	}
	assertCredentialsFlagAgrees(t, fields, info)
}

// assertCredentialsFlagAgrees checks the decode against whatever the body
// actually said. Asserting the mock's own placeholder true would break the
// build the day Porkbun edits its example data; asserting only that a nil
// flag stays nil would pass for a struct tag that no longer matches
// anything. Agreement catches the rename in either direction.
func assertCredentialsFlagAgrees(t *testing.T, fields map[string]json.RawMessage, info *IPInfo) {
	t.Helper()

	raw, sent := fields["credentialsValid"]
	if sent != (info.CredentialsValid != nil) {
		t.Fatalf("credentialsValid in body = %v, but decoded non-nil = %v", sent, info.CredentialsValid != nil)
	}
	if !sent {
		return
	}
	var want bool
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("credentialsValid is no longer a JSON boolean: %s", raw)
	}
	if *info.CredentialsValid != want {
		t.Errorf("credentialsValid decoded as %v, body said %v", *info.CredentialsValid, want)
	}
}
