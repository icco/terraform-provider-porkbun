# The public IP Porkbun sees this Terraform run coming from, checked against
# the configured credentials. This is the address to allowlist when a key is
# scoped by source IP and every call is failing with IP_NOT_ALLOWED.
data "porkbun_ip" "current" {}

output "porkbun_sees_me_as" {
  value = data.porkbun_ip.current.ip
}

output "credentials_valid" {
  value = data.porkbun_ip.current.credentials_valid
}

# /ip ignores credentials entirely, so this still reports the address when
# the key is being refused -- including when it is refused by the very
# allowlist you are trying to fix, which /ping would only answer with an
# error.
data "porkbun_ip" "unauthenticated" {
  validate_credentials = false
}

# Dynamic DNS: point a name at whatever address this runner comes from. The
# precondition refuses to write the record unless Porkbun confirmed the
# credentials -- a lifecycle precondition fails the apply, where a `check`
# block would only warn and let it continue.
resource "porkbun_dns_record" "runner" {
  domain  = "trout.quest"
  name    = "runner"
  type    = "A"
  content = data.porkbun_ip.unauthenticated.ip
  ttl     = 600

  lifecycle {
    precondition {
      condition     = data.porkbun_ip.current.credentials_valid == true
      error_message = "Porkbun did not confirm the API credentials; refusing to write DNS records."
    }
  }
}
