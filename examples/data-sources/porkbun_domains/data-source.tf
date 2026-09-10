# Every domain this API key can actually operate on.
data "porkbun_domains" "manageable" {
  api_access = true
}

# Warn when a domain in the account is not under Terraform management.
# A check block reports; it does not block. A failed assertion here is a
# warning and the apply continues — which is what you want for a coverage
# audit, but do not reach for a check block to gate anything that spends
# money. Use lifecycle.precondition on the spending resource for that.
check "delegation_coverage" {
  assert {
    condition = length(setsubtract(
      data.porkbun_domains.manageable.domains,
      keys(local.nameservers),
    )) == 0
    error_message = format(
      "Porkbun domains not managed in Terraform: %s",
      join(", ", setsubtract(data.porkbun_domains.manageable.domains, keys(local.nameservers))),
    )
  }
}

data "porkbun_domains" "expiring" {
  expiring_within_days = 30
  auto_renew           = false
}
