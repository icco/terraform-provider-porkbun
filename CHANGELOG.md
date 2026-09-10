# Changelog

## 1.0.0

First release of the `icco/porkbun` fork of [`cullenmcdermott/terraform-provider-porkbun`](https://github.com/cullenmcdermott/terraform-provider-porkbun), which was archived in November 2025. The provider was rebuilt on current tooling, with a hand-written client for the subset of the Porkbun v3 JSON API it needs in place of `nrdcg/porkbun`.

FEATURES:

* **New resource** `porkbun_domain_nameservers` — sets the nameservers a domain is delegated to at the registry. Nameservers are modelled as a set and compared ignoring case, trailing dots, ordering and duplicates, so a delegation read back from the registry never shows phantom drift.
* **New data source** `porkbun_domain_nameservers` — reads a delegation without managing it.
* **New data source** `porkbun_domain` — registrar metadata for one domain, including `api_access` (the per-domain API opt-in gate) and `not_local` (whether the domain is delegated away from Porkbun).
* **New data source** `porkbun_domains` — lists and filters domains in the account. The `api_access` filter verifies the opt-in gate before a wide apply.
* Resource identity on both resources, for Terraform 1.12+ `import` blocks.

BREAKING CHANGES (versus upstream):

* `porkbun_dns_record.ttl` and `porkbun_dns_record.prio` are now `number` instead of `string`: change `ttl = "600"` to `ttl = 600`. There is no state upgrader, because this fork publishes under a new registry namespace and no existing state can reach it by upgrading.
* `porkbun_dns_record` import now takes `domain/record_id` instead of a bare record ID. Import never worked upstream — `domain` is required by every API call and was never set — so nothing that worked has stopped working.
* `porkbun_dns_record.notes` is now Optional + Computed with a default of `""`, so an unset `notes` no longer plans as a change after the API returns an empty value.
* The provider address is now `registry.terraform.io/icco/porkbun`.
* Destroying `porkbun_domain_nameservers` deliberately makes no API call: the domain stays delegated where it is. See the resource documentation.

BUG FIXES (all of these bugs were inherited from upstream):

* `porkbun_dns_record` Create no longer writes `id = "0"` into state after a failed create; the missing `return` after the error diagnostic is fixed.
* `porkbun_dns_record` Read now refreshes every attribute. It previously refreshed only `name`, so drift in `content`, `ttl`, `type`, `prio` or `notes` was invisible forever.
* `porkbun_dns_record` Read now removes the resource from state when the record has been deleted out of band.
* `porkbun_dns_record` Read now strips the domain suffix with `TrimSuffix` rather than `ReplaceAll`, which corrupted names containing the domain more than once.
* The provider now honours `base_url` from the configuration. It previously declared the attribute but read only `PORKBUN_BASE_URL`.
* An unknown `api_key` or `secret_key` at plan time is now an error rather than a warning followed by a nil client and a panic.

INTERNALS:

* Go 1.25 floor with a pinned `go1.27.1` toolchain; `terraform-plugin-framework` v1.19.0; protocol 6.
* API errors are detected from the JSON `status` field, never the HTTP status code: Porkbun answers HTTP 200 with `{"status":"ERROR"}` on some endpoints.
* Credentials are sent as `X-API-Key`/`X-Secret-API-Key` headers rather than in request bodies, and every POST carries an `Idempotency-Key`.
* Tests run with no credentials: client decode tests against Porkbun's public `/mock` endpoint, and full lifecycle acceptance tests against an in-process fake.
