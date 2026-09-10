data "porkbun_domain" "trout_quest" {
  domain = "trout.quest"
}

# not_local is true once the domain is delegated away from Porkbun, which is
# when porkbun_dns_record stops having any effect.
output "dns_is_hosted_at_porkbun" {
  value = !data.porkbun_domain.trout_quest.not_local
}
