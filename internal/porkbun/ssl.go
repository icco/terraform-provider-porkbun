package porkbun

import "context"

// SSLBundle is the free Let's Encrypt certificate Porkbun issues for a
// domain hosted on its nameservers.
type SSLBundle struct {
	// CertificateChain is the leaf certificate followed by the intermediates.
	CertificateChain string
	// PrivateKey is secret. Porkbun returns it in the clear, so whatever
	// holds this value holds the certificate.
	PrivateKey string
	PublicKey  string
}

// sslBundleResponse decodes the wire form. The field names are all-lowercase
// and unseparated, and every one is flexString: the spec types them as
// strings, but they exist only once the certificate reaches HAVECERT, so a
// bundle caught mid-issuance can carry a null where a PEM belongs. That
// decodes to "" instead of failing the whole read.
type sslBundleResponse struct {
	CertificateChain flexString `json:"certificatechain"`
	PrivateKey       flexString `json:"privatekey"`
	PublicKey        flexString `json:"publickey"`
}

// RetrieveSSLBundle reads the SSL certificate bundle for a domain.
//
// A domain whose certificate is not yet issued either errors or decodes to
// empty PEMs; the spec documents no SSL-specific error code, so "no
// certificate yet" is not distinguishable from "no such domain".
func (c *Client) RetrieveSSLBundle(ctx context.Context, domain string) (*SSLBundle, error) {
	var out sslBundleResponse
	if err := c.get(ctx, "ssl/retrieve/"+escapePath(domain), nil, &out); err != nil {
		return nil, err
	}
	return &SSLBundle{
		CertificateChain: string(out.CertificateChain),
		PrivateKey:       string(out.PrivateKey),
		PublicKey:        string(out.PublicKey),
	}, nil
}
