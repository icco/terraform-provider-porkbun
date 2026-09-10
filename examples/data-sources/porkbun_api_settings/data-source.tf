data "porkbun_api_settings" "current" {}

# Fail the plan before Porkbun does. It enforces monthly_spend_limit itself,
# but only at the moment of the charge, so a wide apply gets partway through
# and then errors on one domain. A null limit means no cap at all.
resource "porkbun_dns_record" "www" {
  domain  = "trout.quest"
  name    = "www"
  type    = "A"
  content = "203.0.113.10"

  lifecycle {
    precondition {
      condition = (
        data.porkbun_api_settings.current.monthly_spend_limit == null ||
        data.porkbun_api_settings.current.monthly_spend < data.porkbun_api_settings.current.monthly_spend_limit
      )
      error_message = "This Porkbun account has already spent its monthly API budget."
    }
  }
}

# Every amount is in cents.
output "monthly_spend_usd" {
  value = format("$%.2f", data.porkbun_api_settings.current.monthly_spend / 100)
}

output "auto_topup_enabled" {
  value = data.porkbun_api_settings.current.auto_topup
}
