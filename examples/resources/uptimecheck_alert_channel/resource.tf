variable "slack_webhook_url" {
  type      = string
  sensitive = true
}

resource "uptimecheck_alert_channel" "slack" {
  type   = "slack"
  name   = "#ops"
  config = { webhookUrl = var.slack_webhook_url }
}

# Escalation: the inbox hears about an outage that has lasted 15 minutes.
resource "uptimecheck_alert_channel" "inbox" {
  type          = "email"
  name          = "On-call inbox"
  is_default    = false
  delay_minutes = 15
  config        = { to = "oncall@example.com" }
}
