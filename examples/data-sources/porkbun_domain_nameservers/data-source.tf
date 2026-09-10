# Audit a delegation before bringing it under Terraform management.
data "porkbun_domain_nameservers" "trout_quest" {
  domain = "trout.quest"
}

output "current_delegation" {
  value = data.porkbun_domain_nameservers.trout_quest.nameservers
}
