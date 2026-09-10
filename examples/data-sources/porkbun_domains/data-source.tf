# Every domain this API key can actually operate on.
data "porkbun_domains" "manageable" {
  api_access = true
}

# Fail the plan if a domain in the account is not under Terraform management.
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
