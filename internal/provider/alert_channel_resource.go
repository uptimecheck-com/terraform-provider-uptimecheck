package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/uptimecheck-com/terraform-provider-uptimecheck/internal/client"
)

var (
	_ resource.Resource                = &alertChannelResource{}
	_ resource.ResourceWithImportState = &alertChannelResource{}
)

func NewAlertChannelResource() resource.Resource { return &alertChannelResource{} }

type alertChannelResource struct{ client *client.Client }

type alertChannelResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Type          types.String `tfsdk:"type"`
	Name          types.String `tfsdk:"name"`
	Config        types.Map    `tfsdk:"config"`
	IsDefault     types.Bool   `tfsdk:"is_default"`
	Enabled       types.Bool   `tfsdk:"enabled"`
	DelayMinutes  types.Int64  `tfsdk:"delay_minutes"`
	Hint          types.String `tfsdk:"hint"`
	WebhookSecret types.String `tfsdk:"webhook_secret"`
}

// The types the API creates: the ones that need only their address or key. SMS, phone calls,
// Telegram, push and Add to Slack are confirmed or connected in the app.
var channelTypes = []string{"email", "slack", "teams", "discord", "google_chat", "mattermost", "pagerduty", "ntfy", "pushover", "webhook"}

func (r *alertChannelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alert_channel"
}

func (r *alertChannelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An alert channel: where alerts are sent. For the types that need only their address or key; SMS, phone calls, Telegram, push and Add to Slack are set up in the app and can be read with the uptimecheck_alert_channels data source.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Description: "The alert channel's id (ch_…).", Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"type": schema.StringAttribute{
				Description:   "One of " + fmt.Sprint(channelTypes) + ". Changing it replaces the channel.",
				Required:      true,
				Validators:    []validator.String{stringvalidator.OneOf(channelTypes...)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{Description: "Shown in the app, at most 60 characters.", Required: true, Validators: []validator.String{stringvalidator.LengthBetween(1, 60)}},
			"config": schema.MapAttribute{
				Description: "Where alerts go, by type. email: to (up to 10 addresses, separated by commas). slack, teams, discord, google_chat, mattermost: webhookUrl. pagerduty: routingKey. ntfy: topicUrl, and token for a protected topic. pushover: userKey and appToken. webhook: url. The API never returns these, so a change made in the app isn't seen as drift.",
				ElementType: types.StringType,
				Required:    true,
				Sensitive:   true,
			},
			"is_default":    schema.BoolAttribute{Description: "Attached to monitors created without channel_ids.", Optional: true, Computed: true, Default: booldefault.StaticBool(true)},
			"enabled":       schema.BoolAttribute{Description: "false pauses the channel and drops the alerts waiting for it.", Optional: true, Computed: true, Default: booldefault.StaticBool(true)},
			"delay_minutes": schema.Int64Attribute{Description: "Escalation: how long an outage lasts before this channel is alerted (0, 5, 10, 15, 30 or 60).", Optional: true, Computed: true, Default: int64default.StaticInt64(0), Validators: []validator.Int64{int64validator.OneOf(0, 5, 10, 15, 30, 60)}},
			"hint":          schema.StringAttribute{Description: "A short, non-secret trace of the address, as the app shows it.", Computed: true},
			"webhook_secret": schema.StringAttribute{
				Description:   "webhook only: the secret that signs each delivery. The API returns it once, at creation, so it is empty for an imported channel.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *alertChannelResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData != nil {
		r.client = req.ProviderData.(*client.Client)
	}
}

func channelConfig(ctx context.Context, m types.Map) (map[string]string, error) {
	out := map[string]string{}
	if m.IsNull() || m.IsUnknown() {
		return out, nil
	}
	if diags := m.ElementsAs(ctx, &out, false); diags.HasError() {
		return nil, fmt.Errorf("%v", diags)
	}
	return out, nil
}

// set copies what the API returns. Config stays as written: the API never returns it.
func (m *alertChannelResourceModel) set(c client.AlertChannel) {
	m.ID = types.StringValue(c.ID)
	m.Type = types.StringValue(c.Type)
	m.Name = types.StringValue(c.Name)
	m.IsDefault = types.BoolValue(c.IsDefault)
	m.Enabled = types.BoolValue(c.Enabled)
	m.DelayMinutes = types.Int64Value(c.DelayMinutes)
	m.Hint = types.StringValue(c.Hint)
}

func (r *alertChannelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan alertChannelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	config, err := channelConfig(ctx, plan.Config)
	if err != nil {
		resp.Diagnostics.AddError("Invalid alert channel", err.Error())
		return
	}
	name, isDefault, delay := plan.Name.ValueString(), plan.IsDefault.ValueBool(), plan.DelayMinutes.ValueInt64()
	var c client.AlertChannel
	if err := r.client.Do(ctx, "POST", "/alert-channels", client.AlertChannelInput{Type: plan.Type.ValueString(), Name: &name, Config: config, IsDefault: &isDefault, DelayMinutes: &delay}, &c); err != nil {
		resp.Diagnostics.AddError("Couldn't create the alert channel", err.Error())
		return
	}
	secret := c.WebhookSecret
	// A channel is created switched on; one that should start paused is paused right after.
	if !plan.Enabled.ValueBool() {
		off := false
		if err := r.client.Do(ctx, "PATCH", "/alert-channels/"+c.ID, client.AlertChannelInput{Enabled: &off}, &c); err != nil {
			resp.Diagnostics.AddError("Created the alert channel but couldn't pause it", err.Error())
		}
	}
	plan.set(c)
	if secret != nil {
		plan.WebhookSecret = types.StringValue(*secret)
	} else {
		plan.WebhookSecret = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *alertChannelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state alertChannelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var c client.AlertChannel
	if err := r.client.Do(ctx, "GET", "/alert-channels/"+state.ID.ValueString(), nil, &c); err != nil {
		if client.NotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Couldn't read the alert channel", err.Error())
		return
	}
	state.set(c)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *alertChannelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state alertChannelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name, isDefault, enabled, delay := plan.Name.ValueString(), plan.IsDefault.ValueBool(), plan.Enabled.ValueBool(), plan.DelayMinutes.ValueInt64()
	in := client.AlertChannelInput{Name: &name, IsDefault: &isDefault, Enabled: &enabled, DelayMinutes: &delay}
	// The addresses are sent only when they changed here, so an unrelated edit never rewrites them.
	if !plan.Config.Equal(state.Config) {
		config, err := channelConfig(ctx, plan.Config)
		if err != nil {
			resp.Diagnostics.AddError("Invalid alert channel", err.Error())
			return
		}
		in.Config = config
	}
	var c client.AlertChannel
	if err := r.client.Do(ctx, "PATCH", "/alert-channels/"+state.ID.ValueString(), in, &c); err != nil {
		resp.Diagnostics.AddError("Couldn't update the alert channel", err.Error())
		return
	}
	plan.set(c)
	plan.WebhookSecret = state.WebhookSecret
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *alertChannelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state alertChannelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Do(ctx, "DELETE", "/alert-channels/"+state.ID.ValueString(), nil, nil); err != nil && !client.NotFound(err) {
		resp.Diagnostics.AddError("Couldn't delete the alert channel", err.Error())
	}
}

func (r *alertChannelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
