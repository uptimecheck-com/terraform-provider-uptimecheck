resource "uptimecheck_monitor" "site" {
  type   = "http"
  name   = "Website"
  target = "https://example.com"
}

resource "uptimecheck_status_page" "public" {
  title       = "Example status"
  description = "Our website and API."
  components = [
    { monitor_id = uptimecheck_monitor.site.id, name = "Website", group = "Public" },
  ]
}

output "status_page_url" {
  value = uptimecheck_status_page.public.url
}
