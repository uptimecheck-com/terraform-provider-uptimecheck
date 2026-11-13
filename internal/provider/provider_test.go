package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestSchemasAreValid(t *testing.T) {
	ctx := context.Background()
	p := New("test")()
	var ps fwprovider.SchemaResponse
	p.Schema(ctx, fwprovider.SchemaRequest{}, &ps)
	if ps.Diagnostics.HasError() {
		t.Fatal(ps.Diagnostics)
	}
	for _, f := range p.Resources(ctx) {
		var resp resource.SchemaResponse
		f().Schema(ctx, resource.SchemaRequest{}, &resp)
		if d := resp.Schema.ValidateImplementation(ctx); resp.Diagnostics.HasError() || d.HasError() {
			t.Fatal(resp.Diagnostics, d)
		}
	}
	for _, f := range p.DataSources(ctx) {
		var resp datasource.SchemaResponse
		f().Schema(ctx, datasource.SchemaRequest{}, &resp)
		if d := resp.Schema.ValidateImplementation(ctx); resp.Diagnostics.HasError() || d.HasError() {
			t.Fatal(resp.Diagnostics, d)
		}
	}
}

// fakeAPI behaves like /api/v1 for monitors: it fills in config defaults and merges PATCH
// config one level deep, as the real API does.
type fakeAPI struct {
	mu       sync.Mutex
	monitors map[string]map[string]any
	// Projects and alert channels made through the API, by id.
	things map[string]map[string]any
	// The config of each alert channel as last sent: the API keeps it and never returns it.
	secrets map[string]map[string]any
	next    int
}

// things serves POST /<kind> and GET, PATCH, DELETE /<kind>/<id> for projects and alert channels.
func (f *fakeAPI) serveThings(w http.ResponseWriter, r *http.Request, parts []string, body map[string]any, reply func(int, any)) {
	if f.things == nil {
		f.things, f.secrets = map[string]map[string]any{}, map[string]map[string]any{}
	}
	channel := parts[0] == "alert-channels"
	if len(parts) == 1 {
		f.next++
		var t map[string]any
		if channel {
			id := fmt.Sprintf("ach_%d", f.next)
			t = map[string]any{"id": id, "type": body["type"], "name": body["name"], "hint": "…hint", "isDefault": true, "enabled": true, "verified": true, "delayMinutes": 0.0}
			for _, k := range []string{"isDefault", "delayMinutes"} {
				if v, ok := body[k]; ok {
					t[k] = v
				}
			}
			f.secrets[id], _ = body["config"].(map[string]any)
			f.things[id] = t
			if body["type"] == "webhook" {
				// Only the answer to POST carries the signing secret.
				once := map[string]any{"webhookSecret": "whsec_shown_once"}
				for k, v := range t {
					once[k] = v
				}
				reply(201, once)
				return
			}
		} else {
			id := fmt.Sprintf("prj_%d", f.next)
			t = map[string]any{"id": id, "name": body["name"], "color": "#1F5CFF"}
			if v, ok := body["color"]; ok {
				t["color"] = v
			}
			f.things[id] = t
		}
		reply(201, t)
		return
	}
	t, ok := f.things[parts[1]]
	if !ok {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":{"code":"not_found","message":"Not found."}}`))
		return
	}
	switch r.Method {
	case "PATCH":
		for k, v := range body {
			if k == "config" {
				for ck, cv := range v.(map[string]any) {
					f.secrets[parts[1]][ck] = cv
				}
			} else {
				t[k] = v
			}
		}
	case "DELETE":
		delete(f.things, parts[1])
		w.WriteHeader(204)
		return
	}
	reply(200, t)
}

var defaults = map[string]map[string]any{
	"http":      {"method": "GET", "expectedStatus": "200-399", "timeoutMs": 10000.0, "followRedirects": true, "verifyTls": true, "keyword": nil},
	"heartbeat": {"graceSeconds": 300.0, "schedule": nil, "maxRunSeconds": nil},
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(status int, data any) {
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}
	if r.Header.Get("Authorization") != "Bearer uc_test" {
		w.WriteHeader(401)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	switch {
	case parts[0] == "status-pages" && (len(parts) == 2 || r.Method == "POST"):
		f.serveStatusPages(w, r, parts, body, reply)
	case (parts[0] == "projects" || parts[0] == "alert-channels") && (len(parts) == 2 || r.Method == "POST"):
		f.serveThings(w, r, parts, body, reply)
	case r.URL.Path == "/alert-channels":
		reply(200, []any{map[string]any{"id": "ch_1", "name": "#ops", "type": "slack", "isDefault": true, "enabled": true}})
	case r.URL.Path == "/projects":
		reply(200, []any{map[string]any{"id": "prj_1", "name": "Acme", "color": "#1F5CFF"}})
	case r.URL.Path == "/status-pages":
		reply(200, []any{})
	case r.URL.Path == "/monitors" && r.Method == "POST" && (hasNull(body, "regions") || hasNull(body, "channelIds")):
		// Like the real API: arrays may be left out, but null is invalid.
		w.WriteHeader(422)
		w.Write([]byte(`{"error":{"code":"validation_failed","message":"Check the highlighted fields.","fields":{"regions":"Invalid input: expected array, received null"}}}`))
	case r.URL.Path == "/monitors" && r.Method == "POST":
		// Like the real API: checked types need an interval.
		if _, ok := body["intervalSeconds"]; !ok && body["type"] == "http" {
			w.WriteHeader(422)
			w.Write([]byte(`{"error":{"code":"validation_failed","message":"Check the highlighted fields.","fields":{"intervalSeconds":"Pick an interval."}}}`))
			return
		}
		f.next++
		id := fmt.Sprintf("mon_%d", f.next)
		kind := body["type"].(string)
		config := map[string]any{}
		for k, v := range defaults[kind] {
			config[k] = v
		}
		if c, ok := body["config"].(map[string]any); ok {
			for k, v := range c {
				config[k] = v
			}
		}
		m := map[string]any{"id": id, "type": kind, "name": body["name"], "target": body["target"], "port": body["port"], "config": config, "intervalSeconds": 300.0, "regions": []any{}, "projectId": body["projectId"], "channelIds": []any{}, "status": "pending", "heartbeatUrl": nil}
		if v, ok := body["intervalSeconds"]; ok {
			m["intervalSeconds"] = v
		}
		for _, k := range []string{"regions", "channelIds"} {
			if v, ok := body[k]; ok && v != nil {
				m[k] = v
			}
		}
		if kind == "heartbeat" {
			m["target"] = nil
			m["heartbeatUrl"] = "https://platform.test/api/hb/secret-token"
		}
		f.monitors[id] = m
		reply(201, m)
	case len(parts) >= 2 && parts[0] == "monitors":
		m, ok := f.monitors[parts[1]]
		if !ok {
			w.WriteHeader(404)
			w.Write([]byte(`{"error":{"code":"not_found","message":"Monitor not found."}}`))
			return
		}
		switch {
		case len(parts) == 3 && parts[2] == "pause":
			m["status"] = "paused"
		case len(parts) == 3 && parts[2] == "resume":
			m["status"] = "pending"
		case r.Method == "PATCH":
			for k, v := range body {
				if k == "config" {
					for ck, cv := range v.(map[string]any) {
						m["config"].(map[string]any)[ck] = cv
					}
				} else {
					m[k] = v
				}
			}
		case r.Method == "DELETE":
			delete(f.monitors, parts[1])
			reply(200, map[string]any{"id": parts[1], "deleted": true})
			return
		}
		reply(200, m)
	default:
		w.WriteHeader(404)
	}
}

// serveStatusPages behaves like /status-pages: the address comes from the title, a slug is refused (this
// workspace's plan has no custom addresses), and `components` comes back as `componentList`.
func (f *fakeAPI) serveStatusPages(w http.ResponseWriter, r *http.Request, parts []string, body map[string]any, reply func(int, any)) {
	if f.things == nil {
		f.things, f.secrets = map[string]map[string]any{}, map[string]map[string]any{}
	}
	if _, sent := body["slug"]; sent {
		w.WriteHeader(402)
		w.Write([]byte(`{"error":{"code":"plan_limit","message":"Choosing a status page's address needs a plan with custom addresses."}}`))
		return
	}
	apply := func(t map[string]any) {
		for k, v := range body {
			if k == "components" {
				list := []any{}
				for _, c := range v.([]any) {
					item := c.(map[string]any)
					if _, ok := item["group"]; !ok {
						item["group"] = nil
					}
					list = append(list, item)
				}
				t["componentList"] = list
				t["components"] = float64(len(list))
			} else {
				t[k] = v
			}
		}
	}
	if len(parts) == 1 {
		f.next++
		id := fmt.Sprintf("sp_%d", f.next)
		slug := strings.ReplaceAll(strings.ToLower(body["title"].(string)), " ", "-") + "-x7"
		t := map[string]any{"id": id, "slug": slug, "url": "https://platform.test/s/" + slug, "description": "", "logoUrl": nil, "websiteUrl": nil, "accentColor": "#1F5CFF", "theme": "auto", "language": "en", "announcement": nil, "published": true, "noindex": false, "showBranding": true, "projectId": nil, "passwordProtected": false, "emailAccess": false, "components": 0.0, "componentList": []any{}}
		apply(t)
		f.things[id] = t
		reply(201, t)
		return
	}
	t, ok := f.things[parts[1]]
	if !ok {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":{"code":"not_found","message":"Status page not found."}}`))
		return
	}
	switch r.Method {
	case "PATCH":
		apply(t)
	case "DELETE":
		delete(f.things, parts[1])
		w.WriteHeader(204)
		return
	}
	reply(200, t)
}

func hasNull(body map[string]any, key string) bool {
	v, ok := body[key]
	return ok && v == nil
}

func factories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){"uptimecheck": providerserver.NewProtocol6WithError(New("test")())}
}

const monitorConfig = `
data "uptimecheck_alert_channels" "all" {}

resource "uptimecheck_monitor" "shop" {
  type        = "http"
  name        = %q
  target      = "https://shop.example.com"
  paused      = %t
  channel_ids = [for c in data.uptimecheck_alert_channels.all.items : c.id if c.type == "slack"]
  config      = jsonencode({ expectedStatus = "200-299", keyword = { value = "Add to cart", mode = "contains" } })
}

resource "uptimecheck_monitor" "backup" {
  type   = "heartbeat"
  name   = "Nightly backup"
  config = jsonencode({ schedule = { cron = "0 3 * * *", timeZone = "Europe/Stockholm" } })
}
`

func config(name string, paused bool) string {
	return strings.Replace(strings.Replace(monitorConfig, "%q", `"`+name+`"`, 1), "%t", map[bool]string{true: "true", false: "false"}[paused], 1)
}

// Runs with TF_ACC=1 and a terraform binary (CI); talks only to the fake API.
func TestAccMonitor(t *testing.T) {
	api := &fakeAPI{monitors: map[string]map[string]any{}}
	srv := httptest.NewServer(api)
	defer srv.Close()
	t.Setenv("UPTIMECHECK_ENDPOINT", srv.URL)
	t.Setenv("UPTIMECHECK_API_KEY", "uc_test")

	tfresource.Test(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories(),
		Steps: []tfresource.TestStep{
			{
				Config: config("Shop", false),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr("uptimecheck_monitor.shop", "status", "pending"),
					tfresource.TestCheckResourceAttr("uptimecheck_monitor.shop", "interval_seconds", "300"),
					tfresource.TestCheckResourceAttr("uptimecheck_monitor.shop", "channel_ids.#", "1"),
					// Only the keys written are tracked: the server's defaults aren't drift.
					tfresource.TestCheckResourceAttr("uptimecheck_monitor.shop", "config", `{"expectedStatus":"200-299","keyword":{"mode":"contains","value":"Add to cart"}}`),
					tfresource.TestCheckResourceAttr("uptimecheck_monitor.backup", "heartbeat_url", "https://platform.test/api/hb/secret-token"),
				),
			},
			{
				Config: config("Shop checkout", true),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr("uptimecheck_monitor.shop", "name", "Shop checkout"),
					tfresource.TestCheckResourceAttr("uptimecheck_monitor.shop", "status", "paused"),
				),
			},
			{ResourceName: "uptimecheck_monitor.shop", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"config"}},
			{
				// Someone changes a managed key in the app: the plan wants it back.
				PreConfig: func() {
					// Terraform creates resources in parallel, so find the shop by name.
					api.mu.Lock()
					defer api.mu.Unlock()
					for _, m := range api.monitors {
						if m["name"] == "Shop checkout" {
							m["config"].(map[string]any)["expectedStatus"] = "500"
						}
					}
				},
				Config:             config("Shop checkout", true),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// The API rejects null arrays: a monitor without regions or channel_ids must leave them out, so
// POST uses the defaults (all locations, the workspace's default channels).
func TestInputLeavesOutUnsetLists(t *testing.T) {
	ctx := context.Background()
	r := &monitorResource{}
	unset := monitorModel{
		Type:            types.StringValue("heartbeat"),
		Name:            types.StringValue("Nightly backup"),
		IntervalSeconds: types.Int64Null(),
		Regions:         types.SetUnknown(types.StringType),
		ChannelIDs:      types.SetUnknown(types.StringType),
	}
	in, err := r.input(ctx, unset, monitorModel{Regions: types.SetNull(types.StringType), ChannelIDs: types.SetNull(types.StringType)})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(in)
	if strings.Contains(string(body), "regions") || strings.Contains(string(body), "channelIds") {
		t.Fatalf("unset lists must be left out, got %s", body)
	}
	empty := unset
	empty.ChannelIDs = types.SetValueMust(types.StringType, nil)
	in, err = r.input(ctx, empty, monitorModel{Regions: types.SetNull(types.StringType), ChannelIDs: types.SetNull(types.StringType)})
	if err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(in)
	if !strings.Contains(string(body), `"channelIds":[]`) {
		t.Fatalf("an empty channel_ids must send [], got %s", body)
	}
}

const clientConfig = `
resource "uptimecheck_project" "acme" {
  name = %q
}

resource "uptimecheck_alert_channel" "ops" {
  type          = "slack"
  name          = "#ops"
  delay_minutes = %d
  config        = { webhookUrl = %q }
}

resource "uptimecheck_alert_channel" "hook" {
  type       = "webhook"
  name       = "Deploy bot"
  is_default = false
  enabled    = false
  config     = { url = "https://hooks.example.com/uptime" }
}

resource "uptimecheck_monitor" "site" {
  type        = "http"
  name        = "Acme site"
  target      = "https://acme.example.com"
  project_id  = uptimecheck_project.acme.id
  channel_ids = [uptimecheck_alert_channel.ops.id]
}
`

// A client set up from nothing: project, channels, and a monitor that uses both.
func TestAccProjectAndAlertChannels(t *testing.T) {
	api := &fakeAPI{monitors: map[string]map[string]any{}}
	srv := httptest.NewServer(api)
	defer srv.Close()
	t.Setenv("UPTIMECHECK_ENDPOINT", srv.URL)
	t.Setenv("UPTIMECHECK_API_KEY", "uc_test")
	secretOf := func(name string) map[string]any {
		api.mu.Lock()
		defer api.mu.Unlock()
		for id, c := range api.things {
			if c["name"] == name {
				return api.secrets[id]
			}
		}
		return nil
	}
	hook := "https://hooks.slack.com/services/T0/B0/first"

	tfresource.Test(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories(),
		Steps: []tfresource.TestStep{
			{
				Config: fmt.Sprintf(clientConfig, "Acme", 0, hook),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr("uptimecheck_project.acme", "color", "#1F5CFF"),
					tfresource.TestCheckResourceAttr("uptimecheck_alert_channel.ops", "is_default", "true"),
					tfresource.TestCheckResourceAttr("uptimecheck_alert_channel.ops", "enabled", "true"),
					tfresource.TestCheckNoResourceAttr("uptimecheck_alert_channel.ops", "webhook_secret"),
					// A webhook's signing secret is kept from creation; it starts paused as written.
					tfresource.TestCheckResourceAttr("uptimecheck_alert_channel.hook", "webhook_secret", "whsec_shown_once"),
					tfresource.TestCheckResourceAttr("uptimecheck_alert_channel.hook", "enabled", "false"),
					tfresource.TestCheckResourceAttr("uptimecheck_alert_channel.hook", "is_default", "false"),
					tfresource.TestCheckResourceAttrPair("uptimecheck_monitor.site", "project_id", "uptimecheck_project.acme", "id"),
					tfresource.TestCheckResourceAttr("uptimecheck_monitor.site", "channel_ids.#", "1"),
					func(*terraform.State) error {
						if got := secretOf("#ops")["webhookUrl"]; got != hook {
							return fmt.Errorf("the API got webhookUrl %v", got)
						}
						return nil
					},
				),
			},
			{
				// A rename and a delay: the address isn't sent again, and the secret stays in state.
				PreConfig: func() {
					api.mu.Lock()
					defer api.mu.Unlock()
					for id, c := range api.things {
						if c["name"] == "#ops" {
							api.secrets[id]["webhookUrl"] = "untouched"
						}
					}
				},
				Config: fmt.Sprintf(clientConfig, "Acme Corp", 5, hook),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr("uptimecheck_project.acme", "name", "Acme Corp"),
					tfresource.TestCheckResourceAttr("uptimecheck_alert_channel.ops", "delay_minutes", "5"),
					tfresource.TestCheckResourceAttr("uptimecheck_alert_channel.hook", "webhook_secret", "whsec_shown_once"),
					func(*terraform.State) error {
						if got := secretOf("#ops")["webhookUrl"]; got != "untouched" {
							return fmt.Errorf("an unrelated edit sent the address again: %v", got)
						}
						return nil
					},
				),
			},
			{
				// A new address is sent.
				Config: fmt.Sprintf(clientConfig, "Acme Corp", 5, "https://hooks.slack.com/services/T0/B0/second"),
				Check: func(*terraform.State) error {
					if got := secretOf("#ops")["webhookUrl"]; got != "https://hooks.slack.com/services/T0/B0/second" {
						return fmt.Errorf("the new address wasn't sent: %v", got)
					}
					return nil
				},
			},
			{ResourceName: "uptimecheck_project.acme", ImportState: true, ImportStateVerify: true},
			// The API never returns a channel's addresses or its signing secret.
			{ResourceName: "uptimecheck_alert_channel.hook", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"config", "webhook_secret"}},
		},
	})
}

const statusPageConfig = `
resource "uptimecheck_monitor" "shop" {
  type   = "http"
  name   = "Shop"
  target = "https://shop.acme.example"
}

resource "uptimecheck_monitor" "api" {
  type   = "http"
  name   = "API"
  target = "https://api.acme.example"
}

resource "uptimecheck_status_page" "acme" {
  title = "Acme status"
  %s
  components = [
    { monitor_id = uptimecheck_monitor.shop.id, name = "Webshop", group = "Shop" },
    %s
  ]
}

resource "uptimecheck_status_page" "bare" {
  title     = "Internal"
  published = false
}
`

// A page with its components: created, changed in place (an announcement added and removed, components
// reordered and renamed), imported; a page without components stays without drift.
func TestAccStatusPage(t *testing.T) {
	api := &fakeAPI{monitors: map[string]map[string]any{}}
	srv := httptest.NewServer(api)
	defer srv.Close()
	t.Setenv("UPTIMECHECK_ENDPOINT", srv.URL)
	t.Setenv("UPTIMECHECK_API_KEY", "uc_test")
	second := `{ monitor_id = uptimecheck_monitor.api.id, name = "API" },`

	tfresource.Test(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories(),
		Steps: []tfresource.TestStep{
			{
				Config: fmt.Sprintf(statusPageConfig, "", second),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					// The address comes from the API, and no slug is sent when none is written.
					tfresource.TestCheckResourceAttr("uptimecheck_status_page.acme", "slug", "acme-status-x7"),
					tfresource.TestCheckResourceAttr("uptimecheck_status_page.acme", "url", "https://platform.test/s/acme-status-x7"),
					tfresource.TestCheckResourceAttr("uptimecheck_status_page.acme", "theme", "auto"),
					tfresource.TestCheckResourceAttr("uptimecheck_status_page.acme", "published", "true"),
					tfresource.TestCheckResourceAttr("uptimecheck_status_page.acme", "components.#", "2"),
					tfresource.TestCheckResourceAttr("uptimecheck_status_page.acme", "components.0.name", "Webshop"),
					tfresource.TestCheckResourceAttr("uptimecheck_status_page.acme", "components.0.group", "Shop"),
					tfresource.TestCheckNoResourceAttr("uptimecheck_status_page.acme", "components.1.group"),
					tfresource.TestCheckNoResourceAttr("uptimecheck_status_page.acme", "announcement"),
					tfresource.TestCheckResourceAttr("uptimecheck_status_page.bare", "published", "false"),
					tfresource.TestCheckNoResourceAttr("uptimecheck_status_page.bare", "components.#"),
				),
			},
			{
				Config: fmt.Sprintf(statusPageConfig, `announcement = "Planned maintenance on Sunday."`, ""),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr("uptimecheck_status_page.acme", "announcement", "Planned maintenance on Sunday."),
					tfresource.TestCheckResourceAttr("uptimecheck_status_page.acme", "components.#", "1"),
					tfresource.TestCheckResourceAttr("uptimecheck_status_page.acme", "slug", "acme-status-x7"),
				),
			},
			{
				// Taking the announcement out of the configuration clears it on the page.
				Config: fmt.Sprintf(statusPageConfig, "", second),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckNoResourceAttr("uptimecheck_status_page.acme", "announcement"),
					tfresource.TestCheckResourceAttr("uptimecheck_status_page.acme", "components.#", "2"),
				),
			},
			{ResourceName: "uptimecheck_status_page.acme", ImportState: true, ImportStateVerify: true},
		},
	})
}
