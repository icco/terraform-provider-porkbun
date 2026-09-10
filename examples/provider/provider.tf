terraform {
  required_providers {
    porkbun = {
      source  = "icco/porkbun"
      version = "~> 1.0"
    }
  }
}

# Credentials also come from PORKBUN_API_KEY and PORKBUN_SECRET_KEY.
# API access must be enabled for the account at https://porkbun.com/account/api,
# and opted in per domain (or globally, with "Opt In All Domains").
provider "porkbun" {
  api_key    = var.porkbun_api_key
  secret_key = var.porkbun_secret_key
}
