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
  crawl, flow). `config` is the type's settings as JSON, as in the API docs. Only the keys you write are
  managed; the app's defaults for the rest aren't drift. Removing a key leaves its current value.
  `paused` pauses and resumes. `heartbeat_url` (sensitive) is the cron job's ping URL. Import with
  `terraform import uptimecheck_monitor.name mon_…`.
- `uptimecheck_project`: a project (client) with `name` and `color`. Deleting it leaves its monitors and
  status pages without a project; its client reports and client access go with it.
- `uptimecheck_alert_channel`: where alerts go, for the types that need only their address or key (email,
  slack, teams, discord, google_chat, mattermost, pagerduty, ntfy, pushover, webhook). `config` is a
  sensitive map (`to`, `webhookUrl`, `url`, `routingKey`, `topicUrl` and `token`, `userKey` and `appToken`).
  The API never returns it, so it is sent only when it changes here and a change made in the app isn't
  drift. `is_default`, `enabled`, `delay_minutes` (escalation). A webhook's signing secret is in
  `webhook_secret` (sensitive), returned once at creation and empty after an import. SMS, phone calls,
  Telegram, push and Add to Slack are set up in the app; read them with the data source.
- `uptimecheck_status_page`: a public status page with its look (`title`, `description`, `accent_color`,
  `theme`, `language`, `logo_url`, `website_url`, `announcement`, `published`, `noindex`, `show_branding`,
  `project_id`) and its `components` (monitors under the names visitors see, in order, optionally grouped).
  `slug` can only be chosen on plans with custom addresses; left out, it comes from the title and is read back
  with `url`. The password, the email access list and custom CSS are set in the app and left alone.
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

Registry docs in `docs/` are generated from the schema descriptions, `examples/` and `templates/`:
run `go generate ./...` after changing any of them and commit the result.

## Releases

The provider is developed in the Uptimecheck app repo and mirrored to the public repo
`uptimecheck-com/terraform-provider-uptimecheck`, where a `vX.Y.Z` tag builds, signs and publishes a
release with GoReleaser (`.github/workflows/release.yml`) for the Terraform Registry.
