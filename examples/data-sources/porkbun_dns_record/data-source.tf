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

  # ttl, prio and notes are null on a record that carries none, and none of
  # these attributes accept null, so each needs a floor.
  ttl   = coalesce(data.porkbun_dns_record.legacy_www.ttl, 600)
  prio  = coalesce(data.porkbun_dns_record.legacy_www.prio, 0)
  notes = coalesce(data.porkbun_dns_record.legacy_www.notes, "")
}
