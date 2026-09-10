# Porkbun issues a free Let's Encrypt certificate for every domain delegated
# to its own nameservers, so a domain already managed here needs no separate
# ACME account just to terminate TLS somewhere else.
data "porkbun_ssl_bundle" "trout_quest" {
  domain = "trout.quest"
}

# certificate_chain is the leaf plus intermediates in one PEM, which is the
# form most TLS servers want as their certificate file.
resource "local_sensitive_file" "trout_quest_fullchain" {
  filename        = "/etc/ssl/trout.quest/fullchain.pem"
  content         = data.porkbun_ssl_bundle.trout_quest.certificate_chain
  file_permission = "0644"
}

resource "local_sensitive_file" "trout_quest_key" {
  filename        = "/etc/ssl/trout.quest/privkey.pem"
  content         = data.porkbun_ssl_bundle.trout_quest.private_key
  file_permission = "0600"
}

# private_key is sensitive, so any output carrying it has to be marked too.
output "trout_quest_private_key" {
  value     = data.porkbun_ssl_bundle.trout_quest.private_key
  sensitive = true
}
