package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

// clientFromResourceConfigure pulls the shared API client out of a resource
// Configure request.
func clientFromResourceConfigure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) *porkbun.Client {
	if req.ProviderData == nil {
		return nil
	}
	client, ok := req.ProviderData.(*porkbun.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *porkbun.Client, got %T. Please report this to the provider developers.", req.ProviderData),
		)
		return nil
	}
	return client
}

// clientFromDataSourceConfigure is the data source equivalent.
func clientFromDataSourceConfigure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *porkbun.Client {
	if req.ProviderData == nil {
		return nil
	}
	client, ok := req.ProviderData.(*porkbun.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *porkbun.Client, got %T. Please report this to the provider developers.", req.ProviderData),
		)
		return nil
	}
	return client
}

// apiErrorDiagnostic appends the remediation a Porkbun error code calls for.
//
// Only codes the v3 spec documents are branched on: an invented but
// plausible name (INVALID_API_KEY, UNAUTHORIZED) renders no remediation at
// all while making the case look covered.
func apiErrorDiagnostic(summary string, err error) diag.Diagnostic {
	detail := err.Error()
	switch porkbun.ErrorCode(err) {
	case "DOMAIN_NOT_ALLOWED":
		detail += "\n\nThis API key is not permitted to operate on this domain. Per-domain API access is opt-in: " +
			"enable it for the domain at https://porkbun.com/account/domainsSpeedy, or turn on \"Opt In All Domains\" " +
			"under API Access at https://porkbun.com/account/api."
	case "IP_NOT_ALLOWED":
		detail += "\n\nThis API key has an IP allowlist that does not include the address Terraform is calling from. " +
			"Runner IPs are not stable; prefer scoping the key by domain rather than by IP."
	case "INVALID_API_KEYS_001", "INVALID_API_KEYS_002", "API_KEY_REQUIRED", "MISSING_SECRETAPIKEY",
		"INVALID_TOKEN", "INVALID_USER":
		detail += "\n\nCheck api_key/secret_key (PORKBUN_API_KEY / PORKBUN_SECRET_KEY). The secret is the " +
			"`secretapikey` value, not the key itself. API access must also be enabled for the account at " +
			"https://porkbun.com/account/api."
	case "INVALID_DOMAIN":
		detail += "\n\nPorkbun uses INVALID_DOMAIN for both a malformed domain and one that is not in this " +
			"account, so check the spelling of `domain` first. List the domains this key can see with the " +
			"`porkbun_domains` data source (/domain/listAll)."
	case "DOMAIN_NOT_FOUND":
		detail += "\n\nThis domain is not in the authenticated Porkbun account. List the domains this key can " +
			"see with the `porkbun_domains` data source (/domain/listAll)."
	case "RATE_LIMIT_EXCEEDED":
		detail += "\n\nPorkbun rate-limited the request and the provider's retry budget was exhausted. Wait the " +
			"seconds given in the Retry-After header, then apply again; -parallelism=1 helps on a wide rollout."
	}
	return diag.NewErrorDiagnostic(summary, detail)
}
