# What trout.quest publishes right now, read from whichever nameservers are
# authoritative for it today. During an inbound transfer that is the losing
# registrar's zone, and this is the only copy of it that survives the move.
#
# Costs one of 20 scans per hour, per account, and re-runs on every plan.
data "porkbun_dns_scan" "trout_quest" {
  domain = "trout.quest"
}

# Keyed by the pair that identifies a record to porkbun_dns_record, so the
# map is stable even if a later scan returns a different number of records.
locals {
  published = {
    for r in data.porkbun_dns_scan.trout_quest.records :
    "${r.type}/${r.name}" => r
  }

  managed = {
    for r in [porkbun_dns_record.www] :
    "${r.type}/${r.name}" => r
  }
}

# Drift: published from somewhere, managed by nobody. NS and SOA are the
# delegation itself rather than zone contents, so they are never managed here.
output "unmanaged_published_records" {
  value = {
    for k, r in local.published : k => r.content
    if !contains(keys(local.managed), k) && !contains(["NS", "SOA"], r.type)
  }
}

# The scan's own count, which is not necessarily the number of records it
# returned. Neither number is a zone size: DNS cannot be enumerated, so a
# scan finds what it probes for and nothing tells you what it missed.
output "scan_record_count" {
  value = data.porkbun_dns_scan.trout_quest.record_count
}

resource "porkbun_dns_record" "www" {
  domain  = "trout.quest"
  name    = "www"
  type    = "A"
  content = "203.0.113.10"
  ttl     = 600

  lifecycle {
    # A CNAME and an A record cannot coexist at one name (RFC 1034), and
    # Porkbun refuses the write mid-apply. Catching it in the plan needs a
    # precondition: a check block's failed assertion is only a warning and
    # the apply carries on.
    precondition {
      condition     = !contains(keys(local.published), "CNAME/www")
      error_message = "www.trout.quest is published as a CNAME; remove it before writing an A record."
    }
  }
}
