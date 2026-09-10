# No arguments: the provider credentials select the account.
data "porkbun_account_balance" "current" {}

# Registrations, renewals and transfers are paid from account credit. Fail the
# plan rather than discover mid-apply that the account ran dry partway through
# a batch of domains. The comparison is in whole cents, so no float rounding
# decides whether the guard trips.
check "sufficient_credit" {
  assert {
    condition = data.porkbun_account_balance.current.balance_cents >= 5000
    error_message = format(
      "Porkbun account credit is %s, below the $50.00 this configuration expects.",
      data.porkbun_account_balance.current.display,
    )
  }
}

output "porkbun_credit" {
  value = data.porkbun_account_balance.current.display
}
