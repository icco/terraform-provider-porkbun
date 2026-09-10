# One record, by the ID shown in the Porkbun DNS UI. An ID that no longer
# exists fails the plan rather than returning an empty result.
data "porkbun_dns_record" "legacy_www" {
  domain = "example.com"
  id     = "123456789"
}

# subdomain is the form porkbun_dns_record.name takes; name is Porkbun's
# fully-qualified spelling. Reproducing a record that was created by hand:
resource "porkbun_dns_record" "www_copy" {
  domain  = data.porkbun_dns_record.legacy_www.domain
  name    = data.porkbun_dns_record.legacy_www.subdomain
  type    = data.porkbun_dns_record.legacy_www.type
  content = data.porkbun_dns_record.legacy_www.content

  # ttl and prio are null on a record that carries neither, and the resource
  # attributes do not accept null, so each needs a floor. `notes` is left
  # unset: coalesce() rejects "" as a fallback, and the resource defaults it.
  ttl  = coalesce(data.porkbun_dns_record.legacy_www.ttl, 600)
  prio = coalesce(data.porkbun_dns_record.legacy_www.prio, 0)
}
