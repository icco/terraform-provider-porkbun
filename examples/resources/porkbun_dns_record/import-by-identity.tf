# Terraform 1.12 and later.
import {
  to = porkbun_dns_record.www

  identity = {
    domain    = "trout.quest"
    record_id = "123456789"
  }
}
