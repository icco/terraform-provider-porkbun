# Terraform Provider for Porkbun

Manage [Porkbun](https://porkbun.com) domains from Terraform: the nameservers a domain is delegated to at the registry, and DNS records for zones still hosted at Porkbun.

`porkbun_domain_nameservers` is the reason this provider exists. If your domains are registered at Porkbun but their zones live somewhere else — Cloud DNS, Route 53, anywhere — the delegation is otherwise a manual edit in the Porkbun web UI on every change.

## Usage

```hcl
terraform {
  required_providers {
    porkbun = {
      source  = "icco/porkbun"
      version = "~> 1.0"
    }
  }
}

provider "porkbun" {
  # or PORKBUN_API_KEY / PORKBUN_SECRET_KEY
  api_key    = var.porkbun_api_key
  secret_key = var.porkbun_secret_key
}

resource "porkbun_domain_nameservers" "example" {
  domain      = "example.com"
  nameservers = google_dns_managed_zone.example.name_servers
}
```

Nameservers are a set, compared ignoring case, trailing dots and ordering, so another provider's `name_servers` output can be passed straight through without a permanent diff.

Full reference: [`docs/`](./docs).

## Before your first apply

1. **Enable API access** for the account at [porkbun.com/account/api](https://porkbun.com/account/api), then opt in per domain — or globally, with "Opt In All Domains". A key cannot touch a domain that has not opted in, however well scoped it is. `data.porkbun_domains` filtered on `api_access = true` lists the domains a key can actually use.
2. **Check DNSSEC.** If a DS record exists at the registry and the new nameservers do not serve the matching signed zone, every validating resolver returns SERVFAIL the moment the delegation changes. This is the only failure mode here that takes a domain completely dark rather than merely stale. Clear the DS record before or with the switch.
3. **Do not put an IP allowlist on the API key.** CI runner addresses are not stable and you will get `IP_NOT_ALLOWED`. Scope the key by domain instead.
4. **Try one low-stakes domain first**, confirm with `dig NS`, then go wide.

## Two things that will surprise you

**`terraform destroy` does not restore Porkbun's nameservers.** A registered domain always has nameservers and Porkbun publishes no endpoint that unsets them. Destroying `porkbun_domain_nameservers` removes it from state, makes no API call, and emits a warning; a later apply adopts the existing delegation rather than resetting it.

**`porkbun_dns_record` does nothing for a domain delegated elsewhere.** Porkbun keeps its copy of the zone and still answers `SUCCESS` to writes against it, but no resolver asks Porkbun for that zone. The apply goes green and nothing changes. `data.porkbun_domain.<name>.not_local` tells you when you are in that state.

## Development

```sh
make build     # go build ./...
make lint      # gofmt, go vet, golangci-lint
make test      # unit tests + decode tests against Porkbun's /mock endpoint
make testacc   # full lifecycle tests against an in-process fake API
make docs      # regenerate docs/ with tfplugindocs
```

No Porkbun credentials are needed for any of it. `make testacc` drives a real Terraform CLI, which the test harness downloads if one is not on `PATH`.

To try the provider against real infrastructure before it is released, use a [development override](https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides-for-provider-developers):

```hcl
# ~/.terraformrc
provider_installation {
  dev_overrides {
    "icco/porkbun" = "/Users/you/go/bin"
  }
  direct {}
}
```

`go install .` puts the binary there. With an override in place `terraform plan` and `apply` work; `terraform init` is skipped.

## License

MPL-2.0, inherited from [`cullenmcdermott/terraform-provider-porkbun`](https://github.com/cullenmcdermott/terraform-provider-porkbun), which this is a fork of; [CHANGELOG.md](./CHANGELOG.md) records what changed.
