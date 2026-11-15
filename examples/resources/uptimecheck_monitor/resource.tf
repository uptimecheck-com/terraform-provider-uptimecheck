# A website check with a keyword and a slow-response limit.
resource "uptimecheck_monitor" "checkout" {
  type             = "http"
  name             = "Checkout"
  target           = "https://shop.example.com/checkout"
  interval_seconds = 60
  config = jsonencode({
    expectedStatus = "200-299"
    keyword        = { value = "Add to cart", mode = "contains" }
    slowMs         = 1500
  })
}

# A cron job that pings Uptimecheck when it runs.
resource "uptimecheck_monitor" "backup" {
  type = "heartbeat"
  name = "Nightly backup"
  config = jsonencode({
    schedule      = { cron = "0 3 * * *", timeZone = "Europe/Stockholm" }
    graceSeconds  = 900
    maxRunSeconds = 3600
  })
}

output "backup_ping_url" {
  value     = uptimecheck_monitor.backup.heartbeat_url
  sensitive = true
}
