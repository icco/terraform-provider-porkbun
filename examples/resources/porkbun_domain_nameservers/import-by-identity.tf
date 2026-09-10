# Terraform 1.12 and later. This is the practical way to bring a fleet of
# already hand-configured domains under management in one pass.
import {
  to = porkbun_domain_nameservers.trout_quest

  identity = {
    domain = "trout.quest"
  }
}
