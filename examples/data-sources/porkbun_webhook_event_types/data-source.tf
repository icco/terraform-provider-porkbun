# The event types this Porkbun account can subscribe a webhook endpoint to.
# Read live rather than hard-coded: Porkbun adds event types over time.
data "porkbun_webhook_event_types" "available" {}

output "webhook_event_types" {
  value = data.porkbun_webhook_event_types.available.event_types
}

locals {
  # The events this configuration cares about.
  wanted = [
    "domain.expiring",
    "domain.renewed",
  ]

  # Anything left here is a name Porkbun no longer publishes -- usually a
  # typo, or an event type that was renamed. Note that "*" and prefix
  # wildcards such as "dns.*" are valid subscription values that never
  # appear in the catalog, so do not run those through this comparison.
  unknown_event_types = setsubtract(
    toset(local.wanted),
    data.porkbun_webhook_event_types.available.event_types,
  )
}

output "unknown_webhook_event_types" {
  value = local.unknown_event_types
}
