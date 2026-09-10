# Everything Porkbun will provision through the API, both hosting products.
data "porkbun_hosting_plans" "all" {}

# Just the Secure Static Hosting plans.
data "porkbun_hosting_plans" "static" {
  sku_prefix = "PIXIESECURESTATIC"
}

# The monthly static plan. /hosting/create is driven by the sku, and wants
# the same row's price echoed back as acknowledgedCost.
locals {
  monthly_static = one([
    for plan in data.porkbun_hosting_plans.static.plans :
    plan if plan.plan == "monthly" && plan.price != null
  ])
}

output "static_hosting_sku" {
  description = "SKU that provisions Secure Static Hosting, billed monthly."
  value       = local.monthly_static.sku
}

output "static_hosting_cost_usd" {
  description = "Monthly cost in dollars; the API quotes cents."
  value       = local.monthly_static.price / 100
}

# What a year of that plan on every API-enabled domain in the account costs.
data "porkbun_domains" "manageable" {
  api_access = true
}

output "yearly_static_hosting_budget_usd" {
  description = "A monthly static plan on every API-enabled domain, for a year."
  value       = length(data.porkbun_domains.manageable.domains) * local.monthly_static.price * 12 / 100
}
