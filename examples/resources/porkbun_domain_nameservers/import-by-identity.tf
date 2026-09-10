# Terraform 1.12 and later. Adopt a delegation that was set up by hand.
import {
  to = porkbun_domain_nameservers.trout_quest

  identity = {
    domain = "trout.quest"
  }
}
