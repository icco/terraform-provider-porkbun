data "porkbun_domain_availability" "candidate" {
  domain = "trout.quest"
}

# price is the first year; regular_price is every year after it. On a
# promotional TLD the two differ by a lot, so budget on regular_price.
output "first_year_cost" {
  value = data.porkbun_domain_availability.candidate.price
}

output "renewal_cost" {
  value = data.porkbun_domain_availability.candidate.renewal.price
}

# Worth asserting before anything downstream acts on the price: an
# unavailable or premium name still comes back with a full price block, and
# premium names cannot be registered through the API at all.
check "registrable" {
  assert {
    condition = (data.porkbun_domain_availability.candidate.available
    && !data.porkbun_domain_availability.candidate.premium)
    error_message = "trout.quest is not registrable through the Porkbun API."
  }
}
