package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/uptimecheck-com/terraform-provider-uptimecheck/internal/client"
)

var (
	_ resource.Resource                = &monitorResource{}
	_ resource.ResourceWithImportState = &monitorResource{}
)

func NewMonitorResource() resource.Resource { return &monitorResource{} }

type monitorResource struct{ client *client.Client }

type monitorModel struct {
	ID              types.String         `tfsdk:"id"`
	Type            types.String         `tfsdk:"type"`
	Name            types.String         `tfsdk:"name"`
	Target          types.String         `tfsdk:"target"`
	Port            types.Int64          `tfsdk:"port"`
	IntervalSeconds types.Int64          `tfsdk:"interval_seconds"`
	Regions         types.Set            `tfsdk:"regions"`
	ProjectID       types.String         `tfsdk:"project_id"`
	ChannelIDs      types.Set            `tfsdk:"channel_ids"`
	Config          jsontypes.Normalized `tfsdk:"config"`
	Paused          types.Bool           `tfsdk:"paused"`
	Status          types.String         `tfsdk:"status"`
	HeartbeatURL    types.String         `tfsdk:"heartbeat_url"`
}

var monitorTypes = []string{"http", "tcp", "ping", "dns", "domain", "heartbeat", "server", "vendor", "crawl", "flow"}

// Types our locations check on an interval (the API requires one; the provider defaults to 300 s).
var checked = map[string]bool{"http": true, "tcp": true, "ping": true, "dns": true}

func (r *monitorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor"
}

func (r *monitorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A monitor: a website or API, port, ping, DNS records, domain expiry, cron job, server, third-party service, site health scan or multi-step flow.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"type": schema.StringAttribute{
				Description:   "One of " + fmt.Sprint(monitorTypes) + ". Changing it replaces the monitor.",
				Required:      true,
				Validators:    []validator.String{stringvalidator.OneOf(monitorTypes...)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{Description: "Shown in the app and in alerts.", Required: true},
			"target": schema.StringAttribute{
				Description: "URL (http, crawl, and the start page of a flow), host (tcp, ping, dns), domain (domain), or a catalog id or status page address (vendor). Not used for heartbeat and server.",
				Optional:    true,
			},
			"port":             schema.Int64Attribute{Description: "tcp only.", Optional: true},
			"interval_seconds": schema.Int64Attribute{Description: "How often to check (60–3600, default 300), or how often a cron job runs. Follows from the type for domain, server, vendor and crawl, and from config.schedule for cron schedules.", Optional: true, Computed: true, PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}},
			"regions": schema.SetAttribute{
				Description:   "Check locations (e.g. eu-north); empty = all online.",
				ElementType:   types.StringType,
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"project_id": schema.StringAttribute{Description: "The project (client) it belongs to.", Optional: true},
			"channel_ids": schema.SetAttribute{
				Description:   "Alert channels that get its alerts (see the uptimecheck_alert_channels data source).",
				ElementType:   types.StringType,
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"config": schema.StringAttribute{
				CustomType:  jsontypes.NormalizedType{},
				Description: "Type-specific settings as JSON, e.g. jsonencode({ expectedStatus = \"200-299\", keyword = { value = \"Add to cart\", mode = \"contains\" } }). Only the keys you write are managed; the rest keep the app's defaults. See the API docs for every key.",
				Optional:    true,
			},
			"paused":        schema.BoolAttribute{Description: "Paused monitors aren't checked and don't alert.", Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
			"status":        schema.StringAttribute{Description: "pending, up, down or paused.", Computed: true},
			"heartbeat_url": schema.StringAttribute{Description: "heartbeat only: the URL your job pings.", Computed: true, Sensitive: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *monitorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData != nil {
		r.client = req.ProviderData.(*client.Client)
	}
}

func stringList(ctx context.Context, set types.Set) ([]string, error) {
	out := []string{}
	if set.IsNull() || set.IsUnknown() {
		return out, nil
	}
	if diags := set.ElementsAs(ctx, &out, false); diags.HasError() {
		return nil, fmt.Errorf("%v", diags)
	}
	return out, nil
}

func optional(v types.String) *string {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil
	}
	s := v.ValueString()
	return &s
}

// input builds the API body from the plan; sets left unknown (not in the config) keep `prior`.
func (r *monitorResource) input(ctx context.Context, plan, prior monitorModel) (client.MonitorInput, error) {
	in := client.MonitorInput{Type: plan.Type.ValueString(), Name: plan.Name.ValueString(), Target: optional(plan.Target), ProjectID: optional(plan.ProjectID)}
	if !plan.Port.IsNull() && !plan.Port.IsUnknown() {
		p := plan.Port.ValueInt64()
		in.Port = &p
	}
	if !plan.IntervalSeconds.IsNull() && !plan.IntervalSeconds.IsUnknown() {
		i := plan.IntervalSeconds.ValueInt64()
		in.IntervalSeconds = &i
	} else if checked[in.Type] && prior.IntervalSeconds.IsNull() {
		// Checked types need an interval; like the app, default to every 5 minutes.
		i := int64(300)
		in.IntervalSeconds = &i
	}
	pick := func(planned, before types.Set) (types.Set, bool) {
		if planned.IsUnknown() {
			return before, !before.IsNull() && !before.IsUnknown()
		}
		return planned, true
	}
	if set, ok := pick(plan.Regions, prior.Regions); ok {
		list, err := stringList(ctx, set)
		if err != nil {
			return in, err
		}
		in.Regions = &list
	}
	if set, ok := pick(plan.ChannelIDs, prior.ChannelIDs); ok {
		list, err := stringList(ctx, set)
		if err != nil {
			return in, err
		}
		in.ChannelIDs = &list
	}
	if !plan.Config.IsNull() && !plan.Config.IsUnknown() {
		in.Config = json.RawMessage(plan.Config.ValueString())
	}
	return in, nil
}

// apply copies the API's answer into the model; `config` keeps only the keys the user manages.
func apply(ctx context.Context, m *client.Monitor, model *monitorModel) error {
	model.ID = types.StringValue(m.ID)
	model.Type = types.StringValue(m.Type)
	model.Name = types.StringValue(m.Name)
	if m.Type != "heartbeat" && m.Type != "server" && m.Target != nil {
		model.Target = types.StringValue(*m.Target)
	}
	if m.Port != nil {
		model.Port = types.Int64Value(*m.Port)
	} else {
		model.Port = types.Int64Null()
	}
	model.IntervalSeconds = types.Int64Value(m.IntervalSeconds)
	if m.ProjectID != nil {
		model.ProjectID = types.StringValue(*m.ProjectID)
	} else {
		model.ProjectID = types.StringNull()
	}
	var diags interface{ HasError() bool }
	model.Regions, diags = types.SetValueFrom(ctx, types.StringType, nonNil(m.Regions))
	if diags.HasError() {
		return fmt.Errorf("regions: %v", diags)
	}
	model.ChannelIDs, diags = types.SetValueFrom(ctx, types.StringType, nonNil(m.ChannelIDs))
	if diags.HasError() {
		return fmt.Errorf("channel_ids: %v", diags)
	}
	if !model.Config.IsNull() && !model.Config.IsUnknown() {
		managed, err := managedConfig(model.Config.ValueString(), m.Config)
		if err != nil {
			return err
		}
		model.Config = jsontypes.NewNormalizedValue(managed)
	}
	model.Paused = types.BoolValue(m.Status == "paused")
	model.Status = types.StringValue(m.Status)
	if m.HeartbeatURL != nil {
		model.HeartbeatURL = types.StringValue(*m.HeartbeatURL)
	} else {
		model.HeartbeatURL = types.StringNull()
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (r *monitorResource) setPaused(ctx context.Context, id string, paused bool) (*client.Monitor, error) {
	action := "resume"
	if paused {
		action = "pause"
	}
	var m client.Monitor
	if err := r.client.Do(ctx, "POST", "/monitors/"+id+"/"+action, nil, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *monitorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan monitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in, err := r.input(ctx, plan, monitorModel{Regions: types.SetNull(types.StringType), ChannelIDs: types.SetNull(types.StringType)})
	if err != nil {
		resp.Diagnostics.AddError("Invalid monitor", err.Error())
		return
	}
	var m client.Monitor
	if err := r.client.Do(ctx, "POST", "/monitors", in, &m); err != nil {
		resp.Diagnostics.AddError("Couldn't create the monitor", err.Error())
		return
	}
	if plan.Paused.ValueBool() {
		paused, err := r.setPaused(ctx, m.ID, true)
		if err != nil {
			resp.Diagnostics.AddError("Created the monitor but couldn't pause it", err.Error())
		} else {
			m = *paused
		}
	}
	if err := apply(ctx, &m, &plan); err != nil {
		resp.Diagnostics.AddError("Unexpected answer", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *monitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state monitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var m client.Monitor
	if err := r.client.Do(ctx, "GET", "/monitors/"+state.ID.ValueString(), nil, &m); err != nil {
		if client.NotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Couldn't read the monitor", err.Error())
		return
	}
	if err := apply(ctx, &m, &state); err != nil {
		resp.Diagnostics.AddError("Unexpected answer", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *monitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state monitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in, err := r.input(ctx, plan, state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid monitor", err.Error())
		return
	}
	in.Type = ""
	id := state.ID.ValueString()
	var m client.Monitor
	if err := r.client.Do(ctx, "PATCH", "/monitors/"+id, in, &m); err != nil {
		resp.Diagnostics.AddError("Couldn't update the monitor", err.Error())
		return
	}
	if plan.Paused.ValueBool() != (m.Status == "paused") {
		paused, err := r.setPaused(ctx, id, plan.Paused.ValueBool())
		if err != nil {
			resp.Diagnostics.AddError("Couldn't pause or resume the monitor", err.Error())
			return
		}
		m = *paused
	}
	plan.ID = state.ID
	if err := apply(ctx, &m, &plan); err != nil {
		resp.Diagnostics.AddError("Unexpected answer", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *monitorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state monitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Do(ctx, "DELETE", "/monitors/"+state.ID.ValueString(), nil, nil); err != nil && !client.NotFound(err) {
		resp.Diagnostics.AddError("Couldn't delete the monitor", err.Error())
	}
}

func (r *monitorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
