# Import as "domain/record_id". The record ID alone is not enough: every API
# call needs the domain too. Record IDs are visible in the Porkbun DNS UI.
terraform import porkbun_dns_record.www trout.quest/123456789
