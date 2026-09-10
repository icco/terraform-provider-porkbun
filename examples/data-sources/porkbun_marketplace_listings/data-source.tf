# Short .com and .io names for sale, cheapest first. Any filter parameter
# puts the API in filtered mode, which caps the answer at 1000 matches.
data "porkbun_marketplace_listings" "short_and_cheap" {
  tlds           = ["com", "io"]
  sld_length_max = 5
  sort_name      = "price"
  sort_direction = "asc"
  max_results    = 100
}

# Anything on the marketplace whose SLD contains "trout" but not "fish".
data "porkbun_marketplace_listings" "trout" {
  query = "trout -fish"
}

output "trout_asking_prices" {
  value = {
    for l in data.porkbun_marketplace_listings.trout.listings : l.domain => l.price
  }
}

# Names you want, for sale, under budget. price is null when Porkbun sent no
# usable price, which is not the same as free.
locals {
  wanted = ["trout.quest", "pigs.com"]

  affordable = [
    for l in data.porkbun_marketplace_listings.trout.listings :
    l.domain if contains(local.wanted, l.domain) && l.price != null && l.price <= 500
  ]
}
