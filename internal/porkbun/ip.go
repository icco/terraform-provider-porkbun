package porkbun

import "context"

// IPInfo is the shared response of /ip and /ping: the caller's public IP as
// Porkbun sees it, plus — on /ping only — whether the credentials sent
// alongside it were accepted.
type IPInfo struct {
	YourIP string `json:"yourIp"`
	// XForwardedFor is the raw header value, so it can be a comma-separated
	// chain rather than a single address when a proxy is in the path.
	XForwardedFor string `json:"xForwardedFor"`
	// CredentialsValid is nil when Porkbun said nothing about the key: /ip
	// never sends the field, and /ping omits it when no credentials were
	// supplied. Neither case means "rejected" — a bad key is an error
	// response, not credentialsValid:false — so this must stay a pointer.
	CredentialsValid *bool `json:"credentialsValid"`
}

// CallerIP calls /ip, which reports the caller's public IP and ignores
// credentials entirely: the live API answers SUCCESS even for a bogus key.
// That makes it the only way to learn the egress address when the key is
// being refused by its own IP allowlist.
func (c *Client) CallerIP(ctx context.Context) (*IPInfo, error) {
	var out IPInfo
	if err := c.get(ctx, "ip", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PingInfo calls /ping, which reports the same IP and additionally validates
// whatever credentials the client sends: a good key comes back with
// credentialsValid true, a bad one as an INVALID_API_KEYS error.
//
// It sits beside Ping (client.go) rather than replacing it. Ping is this
// package's reachability probe and returns only the IP string, discarding
// the flag this needs.
func (c *Client) PingInfo(ctx context.Context) (*IPInfo, error) {
	var out IPInfo
	if err := c.get(ctx, "ping", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
