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

// apiErrorDiagnostic renders a Porkbun error with its machine-readable code
// front and centre. DOMAIN_NOT_ALLOWED and IP_NOT_ALLOWED are key-scoping
// problems, not configuration problems, and reading them as configuration
// problems wastes a lot of time on a wide rollout.
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
	case "INVALID_API_KEY", "UNAUTHORIZED":
		detail += "\n\nCheck api_key/secret_key (PORKBUN_API_KEY / PORKBUN_SECRET_KEY). API access must also be " +
			"enabled for the account at https://porkbun.com/account/api."
	}
	return diag.NewErrorDiagnostic(summary, detail)
}
