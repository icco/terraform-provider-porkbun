# Delegate a domain registered at Porkbun to a zone hosted somewhere else.
resource "porkbun_domain_nameservers" "trout_quest" {
  domain      = "trout.quest"
  nameservers = google_dns_managed_zone.trout_quest.name_servers
}

# Trailing dots, case and ordering are all ignored, so the raw output of
# another provider can be passed straight through.

# Managing a fleet from one map:
resource "porkbun_domain_nameservers" "delegation" {
  for_each = {
    for name, zone in google_dns_managed_zone.zones : name => zone.name_servers
  }

  domain      = each.key
  nameservers = each.value
}
