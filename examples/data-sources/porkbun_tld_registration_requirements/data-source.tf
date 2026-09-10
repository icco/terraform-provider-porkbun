# What .com asks of a registrant, and what .us asks on top of it.
data "porkbun_tld_registration_requirements" "com" {
  tld = "com"
}

data "porkbun_tld_registration_requirements" "us" {
  tld = "us"
}

# Both schema attributes are JSON documents in a string, and both are null
# when Porkbun sent no schema — so guard every jsondecode with a null check.
output "com_create_fields" {
  value = data.porkbun_tld_registration_requirements.com.request_schema == null ? [] : keys(
    jsondecode(data.porkbun_tld_registration_requirements.com.request_schema).properties
  )
}

# Null whenever the TLD has no structured eligibility rules, which is most.
output "us_eligibility_fields" {
  value = data.porkbun_tld_registration_requirements.us.registry_requirements == null ? [] : keys(
    jsondecode(data.porkbun_tld_registration_requirements.us.registry_requirements).properties
  )
}

# A hard gate: this fails the apply when Porkbun cannot register the TLD
# through the API. A `check` block would only warn and let the apply finish.
resource "terraform_data" "com_is_api_registerable" {
  input = data.porkbun_tld_registration_requirements.com.tld

  lifecycle {
    precondition {
      condition = data.porkbun_tld_registration_requirements.com.api_registerable
      error_message = format(
        ".%s cannot be registered through the Porkbun API: %s",
        data.porkbun_tld_registration_requirements.com.tld,
        coalesce(data.porkbun_tld_registration_requirements.com.not_api_registerable_reason, "no reason given"),
      )
    }
  }
}
