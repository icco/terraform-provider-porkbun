# Every editable record in the zone, as Porkbun currently holds it.
data "porkbun_dns_records" "zone" {
  domain = "example.com"
}

# Just the mail records, sorted and ready to compare against what you expect.
data "porkbun_dns_records" "mx" {
  domain = "example.com"
  type   = "MX"
}

# prio is null on any record that has no priority, and a null cannot go into
# a string template, so it needs a floor even where MX makes one unlikely.
output "mx_hosts" {
  value = [for r in data.porkbun_dns_records.mx.records : "${coalesce(r.prio, 0)} ${r.content}"]
}

# cloudflare_enabled says whether this zone is still the one that answers
# queries. Once a domain is moved to Cloudflare, Porkbun keeps accepting
# record writes against its own stale copy and reports SUCCESS, so guard the
# write itself: a failed precondition stops the apply.
resource "porkbun_dns_record" "www" {
  domain  = "example.com"
  name    = "www"
  type    = "A"
  content = "203.0.113.10"
  ttl     = 600

  lifecycle {
    precondition {
      condition     = data.porkbun_dns_records.zone.cloudflare != "enabled"
      error_message = "example.com has been moved to Cloudflare; records written to the Porkbun zone no longer resolve."
    }
  }
}

# The record IDs the porkbun_dns_record data source takes, grouped by what
# they serve. The apex comes back with an empty subdomain.
output "record_ids" {
  value = {
    for r in data.porkbun_dns_records.zone.records :
    "${r.type} ${r.subdomain == "" ? "@" : r.subdomain}" => r.id...
  }
}
