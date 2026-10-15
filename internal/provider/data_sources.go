package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/uptimecheck-com/terraform-provider-uptimecheck/internal/client"
)

// listSource is a read-only list from the API, e.g. alert channels.
type listSource[T any, M any] struct {
	client      *client.Client
	name        string
	path        string
	description string
	attrs       map[string]schema.Attribute
	toModel     func(T) M
}

func (d *listSource[T, M]) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.name
}

func (d *listSource[T, M]) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: d.description,
		Attributes: map[string]schema.Attribute{
			"items": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: d.attrs}},
		},
	}
}

func (d *listSource[T, M]) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	if req.ProviderData != nil {
		d.client = req.ProviderData.(*client.Client)
	}
}

func (d *listSource[T, M]) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var rows []T
	if err := d.client.Do(ctx, "GET", d.path, nil, &rows); err != nil {
		resp.Diagnostics.AddError("Couldn't read "+d.name, err.Error())
		return
	}
	items := make([]M, 0, len(rows))
	for _, row := range rows {
		items = append(items, d.toModel(row))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, struct {
		Items []M `tfsdk:"items"`
	}{items})...)
}

type alertChannelModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Type      types.String `tfsdk:"type"`
	IsDefault types.Bool   `tfsdk:"is_default"`
	Enabled   types.Bool   `tfsdk:"enabled"`
}

func NewAlertChannelsDataSource() datasource.DataSource {
	return &listSource[client.AlertChannel, alertChannelModel]{
		name:        "alert_channels",
		path:        "/alert-channels",
		description: "The workspace's alert channels (without their secrets). Pick ids for a monitor's channel_ids, e.g. [for c in data.uptimecheck_alert_channels.all.items : c.id if c.type == \"slack\"].",
		attrs: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Computed: true},
			"name":       schema.StringAttribute{Computed: true},
			"type":       schema.StringAttribute{Computed: true, Description: "email, push, slack, teams, discord, google_chat, mattermost, telegram, pagerduty, ntfy, pushover, webhook, sms or voice."},
			"is_default": schema.BoolAttribute{Computed: true, Description: "New monitors in the app get it automatically."},
			"enabled":    schema.BoolAttribute{Computed: true},
		},
		toModel: func(c client.AlertChannel) alertChannelModel {
			return alertChannelModel{types.StringValue(c.ID), types.StringValue(c.Name), types.StringValue(c.Type), types.BoolValue(c.IsDefault), types.BoolValue(c.Enabled)}
		},
	}
}

type projectModel struct {
	ID    types.String `tfsdk:"id"`
	Name  types.String `tfsdk:"name"`
	Color types.String `tfsdk:"color"`
}

func NewProjectsDataSource() datasource.DataSource {
	return &listSource[client.Project, projectModel]{
		name:        "projects",
		path:        "/projects",
		description: "The workspace's projects (often one per client).",
		attrs: map[string]schema.Attribute{
			"id":    schema.StringAttribute{Computed: true},
			"name":  schema.StringAttribute{Computed: true},
			"color": schema.StringAttribute{Computed: true},
		},
		toModel: func(p client.Project) projectModel {
			return projectModel{types.StringValue(p.ID), types.StringValue(p.Name), types.StringValue(p.Color)}
		},
	}
}

type statusPageModel struct {
	ID        types.String `tfsdk:"id"`
	Slug      types.String `tfsdk:"slug"`
	Title     types.String `tfsdk:"title"`
	URL       types.String `tfsdk:"url"`
	Published types.Bool   `tfsdk:"published"`
	ProjectID types.String `tfsdk:"project_id"`
	Language  types.String `tfsdk:"language"`
}

func NewStatusPagesDataSource() datasource.DataSource {
	return &listSource[client.StatusPage, statusPageModel]{
		name:        "status_pages",
		path:        "/status-pages",
		description: "The workspace's public status pages.",
		attrs: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Computed: true},
			"slug":       schema.StringAttribute{Computed: true},
			"title":      schema.StringAttribute{Computed: true},
			"url":        schema.StringAttribute{Computed: true},
			"published":  schema.BoolAttribute{Computed: true},
			"project_id": schema.StringAttribute{Computed: true},
			"language":   schema.StringAttribute{Computed: true},
		},
		toModel: func(p client.StatusPage) statusPageModel {
			project := types.StringNull()
			if p.ProjectID != nil {
				project = types.StringValue(*p.ProjectID)
			}
			return statusPageModel{types.StringValue(p.ID), types.StringValue(p.Slug), types.StringValue(p.Title), types.StringValue(p.URL), types.BoolValue(p.Published), project, types.StringValue(p.Language)}
		},
	}
}
