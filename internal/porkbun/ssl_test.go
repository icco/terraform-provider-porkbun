package porkbun

import (
	"context"
	"testing"
)

// TestMockRetrieveSSLBundle pins the response shape of /ssl/retrieve. The
// three PEM fields are all-lowercase and unseparated (certificatechain, not
// certificate_chain or certificateChain), which is easy to get wrong and
// fails silently: a mis-tagged field decodes to "" and the data source
// writes an empty certificate to state.
//
// The mock serves the literal placeholder "string" for every string field,
// so only non-emptiness can be asserted here, not PEM structure.
func TestMockRetrieveSSLBundle(t *testing.T) {
	c := mockClient(t)

	bundle, err := c.RetrieveSSLBundle(context.Background(), "example.com")
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("RetrieveSSLBundle: %v", err)
	}

	for name, got := range map[string]string{
		"certificatechain": bundle.CertificateChain,
		"privatekey":       bundle.PrivateKey,
		"publickey":        bundle.PublicKey,
	} {
		if got == "" {
			t.Errorf("%s did not decode: %+v", name, bundle)
		}
	}
}
