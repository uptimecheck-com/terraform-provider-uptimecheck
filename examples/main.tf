terraform {
  required_providers {
    uptimecheck = {
      source = "uptimecheck-com/uptimecheck"
    }
  }
}

# The key comes from UPTIMECHECK_API_KEY (Settings › API, read & write).
provider "uptimecheck" {}

variable "slack_webhook_url" {
  type      = string
  sensitive = true
}

# A client from nothing: its project, where its alerts go, and its monitors.
resource "uptimecheck_project" "acme" {
  name  = "Acme"
  color = "#22D37A"
}

resource "uptimecheck_alert_channel" "slack" {
  type   = "slack"
  name   = "#acme-ops"
  config = { webhookUrl = var.slack_webhook_url }
}

# Escalation: the inbox hears about an outage that has lasted 15 minutes.
resource "uptimecheck_alert_channel" "inbox" {
  type          = "email"
  name          = "Acme on-call inbox"
  is_default    = false
  delay_minutes = 15
  config        = { to = "oncall@acme.example, cto@acme.example" }
}

# Channels set up in the app (SMS, Telegram, push, Add to Slack) are read, not managed.
data "uptimecheck_alert_channels" "all" {}

locals {
  client = uptimecheck_project.acme.id
  slack  = [uptimecheck_alert_channel.slack.id, uptimecheck_alert_channel.inbox.id]
  sms    = [for c in data.uptimecheck_alert_channels.all.items : c.id if c.type == "sms"]
}

resource "uptimecheck_monitor" "checkout" {
  type             = "http"
  name             = "Acme checkout"
  target           = "https://shop.acme.example/checkout"
  interval_seconds = 60
  project_id       = local.client
  channel_ids      = local.slack
  config = jsonencode({
    expectedStatus = "200-299"
    keyword        = { value = "Add to cart", mode = "contains" }
    slowMs         = 1500
  })
}

resource "uptimecheck_monitor" "backup" {
  type        = "heartbeat"
  name        = "Acme nightly backup"
  project_id  = local.client
  channel_ids = local.slack
  config = jsonencode({
    schedule      = { cron = "0 3 * * *", timeZone = "Europe/Stockholm" }
    graceSeconds  = 900
    maxRunSeconds = 3600
  })
}

resource "uptimecheck_monitor" "site" {
  type       = "crawl"
  name       = "Acme site health"
  target     = "https://shop.acme.example"
  project_id = local.client
  config     = jsonencode({ maxPages = 250 })
}

resource "uptimecheck_status_page" "acme" {
  title       = "Acme status"
  description = "The shop and its nightly jobs."
  project_id  = local.client
  components = [
    { monitor_id = uptimecheck_monitor.checkout.id, name = "Checkout", group = "Shop" },
    { monitor_id = uptimecheck_monitor.backup.id, name = "Nightly backup" },
  ]
}

output "status_page_url" {
  value = uptimecheck_status_page.acme.url
}

output "backup_ping_url" {
  value     = uptimecheck_monitor.backup.heartbeat_url
  sensitive = true
}
