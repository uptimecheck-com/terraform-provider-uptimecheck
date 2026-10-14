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
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
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
	next     int
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
	case r.URL.Path == "/alert-channels":
		reply(200, []any{map[string]any{"id": "ch_1", "name": "#ops", "type": "slack", "isDefault": true, "enabled": true}})
	case r.URL.Path == "/projects":
		reply(200, []any{map[string]any{"id": "prj_1", "name": "Acme", "color": "#1F5CFF"}})
	case r.URL.Path == "/status-pages":
		reply(200, []any{})
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
