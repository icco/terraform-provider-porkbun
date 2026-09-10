# Only useful while the domain is still on Porkbun's own nameservers. Check
# data.porkbun_domain.<name>.not_local first: for a domain delegated
# elsewhere these writes succeed and change nothing anyone can resolve.
resource "porkbun_dns_record" "www" {
  domain  = "trout.quest"
  name    = "www"
  type    = "A"
  content = "203.0.113.10"
  ttl     = 600
}

resource "porkbun_dns_record" "apex" {
  domain  = "trout.quest"
  type    = "A"
  content = "203.0.113.10"
}

resource "porkbun_dns_record" "mail" {
  domain  = "trout.quest"
  type    = "MX"
  content = "mail.trout.quest"
  prio    = 10
  notes   = "primary MX"
}
