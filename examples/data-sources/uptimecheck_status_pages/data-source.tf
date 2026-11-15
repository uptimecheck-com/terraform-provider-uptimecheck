data "uptimecheck_status_pages" "all" {}

output "status_page_urls" {
  value = [for p in data.uptimecheck_status_pages.all.items : p.url]
}
