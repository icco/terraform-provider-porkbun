# Terraform Provider for Porkbun

Manage [Porkbun](https://porkbun.com) domains from Terraform: registry
nameserver delegation, and DNS records for zones still hosted at Porkbun.

This is a fork of an upstream provider that was archived in November 2024 —
see [CHANGELOG.md](./CHANGELOG.md) for the attribution and for everything
that changed. The Go code has been rewritten; the API client is hand-written
against [Porkbun's OpenAPI spec](https://porkbun.com/api/json/v3/spec).

## Why it exists

To stop changing nameservers by hand. If your domains are registered at
Porkbun but their zones live somewhere else — Cloud DNS, Route 53, anywhere —
the delegation is the one piece that has always had to be clicked through the
Porkbun web UI. `porkbun_domain_nameservers` is that click, in Terraform.

```hcl
resource "porkbun_domain_nameservers" "delegation" {
  for_each = local.zones

  domain      = each.key
  nameservers = each.value.name_servers
}
```

Ordering, case and trailing dots are all ignored, so another provider's
`name_servers` output can be passed straight through. An NS RRset is
unordered (RFC 1034/2181) and registries return it in whatever order they
like; a provider that modelled this as a list would show a diff on every
plan, forever.

## Usage

```hcl
terraform {
  required_providers {
    porkbun = {
      source  = "icco/porkbun"
      version = "~> 0.4"
    }
  }
}

provider "porkbun" {
  # or PORKBUN_API_KEY / PORKBUN_SECRET_KEY
  api_key    = var.porkbun_api_key
  secret_key = var.porkbun_secret_key
}
```

Full reference: [`docs/`](./docs).

## Before your first apply

1. **Enable API access.** Turn it on for the account at
   [porkbun.com/account/api](https://porkbun.com/account/api), then opt in
   per domain — or globally, with "Opt In All Domains". A key cannot touch a
   domain that has not opted in, however well scoped it is. Verify with
   `data.porkbun_domains` filtered on `api_access = true` before applying
   widely.
2. **Check DNSSEC.** This is the only failure mode that takes a domain
   completely dark rather than merely stale. If a DS record exists at the
   registry and the new nameservers do not serve the matching signed zone,
   every validating resolver returns SERVFAIL the moment the delegation
   changes. Audit with `/dns/getDnssecRecords` and clear the DS record before
   or with the switch.
3. **Do not put an IP allowlist on the API key.** CI runner IPs are not
   stable and you will get `IP_NOT_ALLOWED`. Scope the key by domain instead.
4. **Try one low-stakes domain first**, confirm with `dig NS`, then go wide.

## Two things that will surprise you

**`terraform destroy` does not restore Porkbun's nameservers.** A registered
domain always has nameservers; Porkbun publishes no endpoint that unsets them
and no citable list of its own defaults. Destroying
`porkbun_domain_nameservers` therefore removes it from state, makes no API
call, and emits a warning. A later apply adopts the existing delegation
rather than resetting it.

**`porkbun_dns_record` does nothing for a domain delegated elsewhere.**
Porkbun keeps its copy of the zone and still answers `SUCCESS` to writes
against it, but no resolver asks Porkbun for that zone. The apply goes green
and nothing changes. `data.porkbun_domain.<name>.not_local` tells you when
you are in that state.

## Development

```sh
make build     # go build ./...
make lint      # gofmt, go vet, golangci-lint
make test      # unit tests + decode tests against Porkbun's /mock endpoint
make testacc   # full lifecycle tests against an in-process fake API
make docs      # regenerate docs/ with tfplugindocs
```

No Porkbun credentials are needed for any of it. `make testacc` needs a
Terraform CLI, which the test harness downloads if it is not on `PATH`.

To try the provider against real infrastructure before it is released, use a
[development override](https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides-for-provider-developers):

```hcl
# ~/.terraformrc
provider_installation {
  dev_overrides {
    "icco/porkbun" = "/Users/you/go/bin"
  }
  direct {}
}
```

`go install .` puts the binary there. With an override in place `terraform
plan` and `apply` work; `terraform init` is skipped.

## License

MPL-2.0, inherited from the upstream project.
