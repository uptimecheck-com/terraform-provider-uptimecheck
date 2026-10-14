# Terraform provider for Uptimecheck

Monitors as code, plus lookups of alert channels, projects and status pages. It talks to the REST
API (`/api/v1`) with a read & write API key from Settings › API.

```hcl
provider "uptimecheck" {} # api_key from UPTIMECHECK_API_KEY

resource "uptimecheck_monitor" "checkout" {
  type   = "http"
  name   = "Checkout"
  target = "https://shop.example.com/checkout"
  config = jsonencode({ expectedStatus = "200-299" })
}
```

More in [examples/main.tf](examples/main.tf).

## What's in it

- `uptimecheck_monitor`: every monitor type (http, tcp, ping, dns, domain, heartbeat, server, vendor,
  crawl). `config` is the type's settings as JSON, as in the API docs. Only the keys you write are
  managed; the app's defaults for the rest aren't drift. Removing a key leaves its current value.
  `paused` pauses and resumes. `heartbeat_url` (sensitive) is the cron job's ping URL. Import with
  `terraform import uptimecheck_monitor.name mon_…`.
- Data sources: `uptimecheck_alert_channels`, `uptimecheck_projects`, `uptimecheck_status_pages`
  (each an `items` list).

## Development

```bash
cd terraform
go test ./...                 # unit tests and schema checks
TF_ACC=1 go test ./...        # also real Terraform against a fake API (needs terraform on PATH)
```

To try it against the real API locally, build it and point Terraform at it with a dev override
(`~/.terraformrc`):

```hcl
provider_installation {
  dev_overrides { "uptimecheck-com/uptimecheck" = "/path/to/uptimecheck-platform/terraform" }
  direct {}
}
```

`go build -o terraform-provider-uptimecheck` puts the binary there.

## Publishing (not done yet)

The Terraform Registry needs a public GitHub repo named `terraform-provider-uptimecheck`, signed
releases (GPG) built with GoReleaser, and the repo added in the registry. See TODO.md.
