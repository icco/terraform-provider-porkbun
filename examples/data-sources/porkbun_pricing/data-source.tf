# List prices for the TLDs this configuration registers under.
data "porkbun_pricing" "candidates" {
  tlds = ["com", "io", "quest"]
}

# Porkbun formats money for display, so a price can carry a thousands
# separator: tonumber("2,060.25") fails. Strip it before comparing.
locals {
  renewal_usd = {
    for tld, price in data.porkbun_pricing.candidates.pricing :
    tld => tonumber(replace(price.renewal, ",", ""))
  }
}

# Refuse to plan against a TLD that got more expensive than the budget.
check "renewal_budget" {
  assert {
    condition = alltrue([for tld, usd in local.renewal_usd : usd <= 50])
    error_message = format(
      "TLD renewals over $50/yr: %s",
      jsonencode({ for tld, usd in local.renewal_usd : tld => usd if usd > 50 }),
    )
  }
}

# Any coupon Porkbun is currently running on .quest, keyed by product.
output "quest_coupons" {
  value = data.porkbun_pricing.candidates.pricing["quest"].coupons
}
