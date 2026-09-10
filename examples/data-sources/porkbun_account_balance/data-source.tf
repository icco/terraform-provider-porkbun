# No arguments: the provider credentials select the account.
data "porkbun_account_balance" "current" {}

# Registrations, renewals and transfers are paid from account credit.
#
# To stop an apply that would run the account dry, the guard has to be a
# precondition on the resource that spends. A failed `check` block assertion
# is only a warning: the apply continues past it and spends anyway, which is
# the outcome the guard exists to prevent. A failed precondition stops the
# apply before the resource is created.
#
# terraform_data stands in here for whichever resource does the spending.
# The comparison is in whole cents, so no float rounding decides whether the
# guard trips.
resource "terraform_data" "registration_guard" {
  input = "gates the resources that draw down Porkbun credit"

  lifecycle {
    precondition {
      condition = data.porkbun_account_balance.current.balance_cents >= 5000
      error_message = format(
        "Porkbun account credit is %s, below the $50.00 this configuration expects.",
        data.porkbun_account_balance.current.display,
      )
    }
  }
}

output "porkbun_credit" {
  value = data.porkbun_account_balance.current.display
}
