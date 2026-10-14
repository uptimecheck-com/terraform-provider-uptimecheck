terraform {
  required_providers {
    uptimecheck = {
      source = "uptimecheck-com/uptimecheck"
    }
  }
}

# The key comes from UPTIMECHECK_API_KEY (Settings › API, read & write).
provider "uptimecheck" {}

data "uptimecheck_alert_channels" "all" {}
data "uptimecheck_projects" "all" {}

locals {
  slack  = [for c in data.uptimecheck_alert_channels.all.items : c.id if c.type == "slack"]
  client = one([for p in data.uptimecheck_projects.all.items : p.id if p.name == "Acme"])
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

output "backup_ping_url" {
  value     = uptimecheck_monitor.backup.heartbeat_url
  sensitive = true
}
