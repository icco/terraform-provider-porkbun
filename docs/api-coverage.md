# API coverage

Every path in the [Porkbun v3 OpenAPI spec](https://porkbun.com/api/json/v3/spec) (`3.21`, 90 paths), and where it surfaces in this provider. Generated against the spec and checked in so coverage claims stay falsifiable — the list is the denominator, not a summary of what was convenient.

Porkbun offers several endpoints under both `GET` and `POST` for compatibility. Those collapse to one logical operation here, keyed by path, because they do the same thing.

Dispositions:

- **resource** — managed lifecycle, a `porkbun_*` resource.
- **data_source** — read-only, a `porkbun_*` data source.
- **client_only** — reachable on `internal/porkbun.Client` but no Terraform surface of its own, because it is an action rather than a piece of state.
- **out_of_scope** — deliberately not implemented, with the reason given.

## Not supported

**Cloudflare connect (`/cloudflare/*`, 17 paths).** Deferred — this provider does not manage moving domains into a customer's own Cloudflare account. Note that DNS writes against a domain whose DNS Cloudflare already serves still return `SUCCESS` from `/dns/*` while changing nothing that resolves; the client decodes the `warnings` Porkbun attaches to those responses and `porkbun_dns_record` surfaces them, so the case is visible even though it is not manageable here.

**Credential bootstrap (`/apikey/*`).** The provider needs credentials before it can be configured, so it cannot mint its own.

**Test fixtures (`/mock*`, `/sandbox/*`).** Not infrastructure. The client exposes `MockBaseURL` for the decode tests.

## Coverage

| Path | Verbs | Disposition | Surface |
| --- | --- | --- | --- |
| `/ping` | `POST+GET` | client_only | Ping (already implemented; credential check) |
| `/webhook/resend` | `POST` | client_only | delivery resend helper |
| `/webhook/test` | `POST` | client_only | porkbun_webhook test event helper |
| `/account/apiSettings` | `GET` | data_source | porkbun_api_settings |
| `/account/balance` | `GET` | data_source | porkbun_account_balance |
| `/dns/getDnssecRecords/{domain}` | `POST+GET` | data_source | porkbun_dnssec_records + resource read |
| `/dns/retrieve/{domain}/{id}` | `POST+GET` | data_source | porkbun_dns_record |
| `/dns/retrieve/{domain}` | `POST+GET` | data_source | porkbun_dns_records |
| `/dns/scan/{domain}` | `GET` | data_source | porkbun_dns_scan |
| `/domain/checkDomain/{domain}` | `POST` | data_source | porkbun_domain_availability |
| `/domain/get/{domain}` | `GET` | data_source | porkbun_domain (shipped) |
| `/domain/getGlue/{domain}` | `POST+GET` | data_source | porkbun_glue_records + resource read |
| `/domain/getRegistrationRequirements/{tld}` | `GET` | data_source | porkbun_tld_registration_requirements |
| `/domain/getUrlForwarding/{domain}` | `POST+GET` | data_source | porkbun_url_forwards + resource read |
| `/domain/listAll` | `POST+GET` | data_source | porkbun_domains (shipped) |
| `/domain/listTransfers` | `GET` | data_source | porkbun_domain_transfers |
| `/hosting/files/{domain}` | `GET` | data_source | porkbun_hosting_files + resource read |
| `/hosting/plans` | `GET` | data_source | porkbun_hosting_plans |
| `/ip` | `POST+GET` | data_source | porkbun_ip |
| `/marketplace/getAll` | `GET+POST` | data_source | porkbun_marketplace_listings |
| `/pricing/get` | `POST+GET` | data_source | porkbun_pricing |
| `/ssl/retrieve/{domain}` | `POST+GET` | data_source | porkbun_ssl_bundle |
| `/webhook/deliveries` | `GET` | data_source | porkbun_webhook_deliveries |
| `/webhook/delivery/{id}` | `GET` | data_source | porkbun_webhook_delivery |
| `/webhook/eventTypes` | `GET` | data_source | porkbun_webhook_event_types |
| `/webhook/list` | `GET` | data_source | porkbun_webhooks |
| `/apikey/request` | `POST` | out_of_scope | credential bootstrap: provider needs keys to configure |
| `/apikey/retrieve` | `POST` | out_of_scope | credential bootstrap: provider needs keys to configure |
| `/cloudflare/connect` | `POST` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/createRecord/{domain}` | `POST` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/deleteRecord/{domain}/{recordId}` | `POST` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/disconnect` | `POST` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/editRecord/{domain}/{recordId}` | `POST` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/get/{domain}` | `GET` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/getConnection` | `GET` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/getQueue` | `GET` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/getRecords/{domain}` | `GET` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/getZone/{domain}` | `GET` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/getZoneSettings/{domain}` | `GET` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/inventory` | `GET` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/preview/{domain}` | `GET` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/retry/{domain}` | `POST` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/rollback/{domain}` | `POST` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/setProxy/{domain}` | `POST` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/cloudflare/setZoneSettings/{domain}` | `POST` | out_of_scope | deferred: Cloudflare connect is not supported by this provider for now |
| `/mock/{path}` | `GET` | out_of_scope | test harness; client exposes MockBaseURL |
| `/mock` | `GET` | out_of_scope | test harness; client exposes MockBaseURL |
| `/sandbox/reset` | `POST` | out_of_scope | sandbox-only test fixture, not infrastructure |
| `/sandbox/topup` | `POST` | out_of_scope | sandbox-only test fixture, not infrastructure |
| `/sandbox/triggerWebhook` | `POST` | out_of_scope | sandbox-only test fixture, not infrastructure |
| `/account/invite` | `POST` | resource | porkbun_account_invite |
| `/account/inviteStatus` | `GET` | resource | porkbun_account_invite (read) |
| `/dns/create/{domain}` | `POST` | resource | porkbun_dns_record (shipped) |
| `/dns/createDnssecRecord/{domain}` | `POST` | resource | porkbun_dnssec_record (create) |
| `/dns/delete/{domain}/{id}` | `POST` | resource | porkbun_dns_record (shipped) |
| `/dns/deleteByNameType/{domain}/{type}/{subdomain}` | `POST` | resource | porkbun_dns_record_set (delete) |
| `/dns/deleteDnssecRecord/{domain}/{keytag}` | `POST` | resource | porkbun_dnssec_record (delete) |
| `/dns/edit/{domain}/{id}` | `POST` | resource | porkbun_dns_record (shipped) |
| `/dns/editByNameType/{domain}/{type}/{subdomain}` | `POST` | resource | porkbun_dns_record_set (write) |
| `/dns/import/{domain}` | `POST` | resource | porkbun_dns_records_import |
| `/dns/retrieveByNameType/{domain}/{type}/{subdomain}` | `POST+GET` | resource | porkbun_dns_record_set (read) |
| `/domain/addUrlForward/{domain}` | `POST` | resource | porkbun_url_forward (create) |
| `/domain/create/{domain}` | `POST` | resource | porkbun_domain (register; no-op delete) |
| `/domain/createGlue/{domain}/{subdomain}` | `POST` | resource | porkbun_glue_record (create) |
| `/domain/deleteGlue/{domain}/{subdomain}` | `POST` | resource | porkbun_glue_record (delete) |
| `/domain/deleteUrlForward/{domain}/{id}` | `POST` | resource | porkbun_url_forward (delete) |
| `/domain/getContacts/{domain}` | `GET` | resource | porkbun_domain_contacts (read) |
| `/domain/getNs/{domain}` | `POST+GET` | resource | porkbun_domain_nameservers (shipped) |
| `/domain/getTransfer/{domain}` | `GET` | resource | porkbun_domain_transfer (read) |
| `/domain/renew/{domain}` | `POST` | resource | porkbun_domain (renew) |
| `/domain/transfer/{domain}` | `POST` | resource | porkbun_domain_transfer |
| `/domain/updateAutoRenew/{domain}` | `POST` | resource | porkbun_domain_auto_renew |
| `/domain/updateContacts/{domain}` | `POST` | resource | porkbun_domain_contacts (write) |
| `/domain/updateGlue/{domain}/{subdomain}` | `POST` | resource | porkbun_glue_record (update) |
| `/domain/updateNs/{domain}` | `POST` | resource | porkbun_domain_nameservers (shipped) |
| `/email/setPassword` | `POST` | resource | porkbun_email_password |
| `/hosting/create/{domain}` | `POST` | resource | porkbun_hosting (create) |
| `/hosting/createWpCredentials/{domain}` | `POST` | resource | porkbun_hosting_wp_credential (create) |
| `/hosting/delete/{domain}` | `POST` | resource | porkbun_hosting (delete) |
| `/hosting/deleteFile/{domain}` | `POST` | resource | porkbun_hosting_file (delete) |
| `/hosting/deleteWpCredentials/{domain}` | `POST` | resource | porkbun_hosting_wp_credential (delete) |
| `/hosting/deploy/{domain}` | `POST` | resource | porkbun_hosting_file (upload) |
| `/hosting/get/{domain}` | `GET` | resource | porkbun_hosting (read) |
| `/hosting/getWpCredentials/{domain}` | `GET` | resource | porkbun_hosting_wp_credential (read) |
| `/hosting/makeDir/{domain}` | `POST` | resource | porkbun_hosting_directory |
| `/webhook/create` | `POST` | resource | porkbun_webhook (create) |
| `/webhook/delete` | `POST` | resource | porkbun_webhook (delete) |
| `/webhook/get/{id}` | `GET` | resource | porkbun_webhook (read) |
| `/webhook/rotateSecret` | `POST` | resource | porkbun_webhook (secret rotation) |
| `/webhook/update` | `POST` | resource | porkbun_webhook (update) |
