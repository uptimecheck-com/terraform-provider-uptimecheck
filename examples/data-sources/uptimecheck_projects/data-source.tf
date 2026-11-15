data "uptimecheck_projects" "all" {}

locals {
  acme_project_id = one([for p in data.uptimecheck_projects.all.items : p.id if p.name == "Acme"])
}
