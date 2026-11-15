package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/uptimecheck-com/terraform-provider-uptimecheck/internal/client"
)

var (
	_ resource.Resource                = &statusPageResource{}
	_ resource.ResourceWithImportState = &statusPageResource{}
)

func NewStatusPageResource() resource.Resource { return &statusPageResource{} }

type statusPageResource struct{ client *client.Client }

type statusPageResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Title        types.String `tfsdk:"title"`
	Description  types.String `tfsdk:"description"`
	Slug         types.String `tfsdk:"slug"`
	URL          types.String `tfsdk:"url"`
	LogoURL      types.String `tfsdk:"logo_url"`
	WebsiteURL   types.String `tfsdk:"website_url"`
	AccentColor  types.String `tfsdk:"accent_color"`
	Theme        types.String `tfsdk:"theme"`
	Language     types.String `tfsdk:"language"`
	Announcement types.String `tfsdk:"announcement"`
	Published    types.Bool   `tfsdk:"published"`
	Noindex      types.Bool   `tfsdk:"noindex"`
	ShowBranding types.Bool   `tfsdk:"show_branding"`
	ShowResponse types.Bool   `tfsdk:"show_response_times"`
	ProjectID    types.String `tfsdk:"project_id"`
	Components   types.List   `tfsdk:"components"`
}

type statusPageComponentModel struct {
	MonitorID types.String `tfsdk:"monitor_id"`
	Name      types.String `tfsdk:"name"`
	Group     types.String `tfsdk:"group"`
}

var statusPageComponentType = types.ObjectType{AttrTypes: map[string]attr.Type{"monitor_id": types.StringType, "name": types.StringType, "group": types.StringType}}

func (r *statusPageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page"
}

func (r *statusPageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A public status page: its look and the monitors it shows. The password, the email access list and custom CSS are set in the app and left alone here. Deleting it deletes its subscribers, written updates and visitor statistics.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Description: "The status page's id (sp_…).", Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"title":       schema.StringAttribute{Description: "The page's heading, at most 80 characters.", Required: true, Validators: []validator.String{stringvalidator.LengthBetween(1, 80)}},
			"description": schema.StringAttribute{Description: "A line under the title.", Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"slug": schema.StringAttribute{
				Description:   "The address after /s/. Only plans with custom addresses may choose it; left out, it comes from the title when the page is created.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"url":                 schema.StringAttribute{Description: "The public address.", Computed: true},
			"logo_url":            schema.StringAttribute{Description: "An https:// image shown instead of the title.", Optional: true},
			"website_url":         schema.StringAttribute{Description: "An https:// link back to your site.", Optional: true},
			"accent_color":        schema.StringAttribute{Description: "Hex color, e.g. #1F5CFF (the default).", Optional: true, Computed: true, Default: stringdefault.StaticString("#1F5CFF")},
			"theme":               schema.StringAttribute{Description: "auto (follows the visitor, the default), light or dark.", Optional: true, Computed: true, Default: stringdefault.StaticString("auto"), Validators: []validator.String{stringvalidator.OneOf("auto", "light", "dark")}},
			"language":            schema.StringAttribute{Description: "The language visitors and subscribers see the page in (default en).", Optional: true, Computed: true, Default: stringdefault.StaticString("en")},
			"announcement":        schema.StringAttribute{Description: "A notice at the top of the page.", Optional: true},
			"published":           schema.BoolAttribute{Description: "false takes the page offline without deleting it.", Optional: true, Computed: true, Default: booldefault.StaticBool(true)},
			"noindex":             schema.BoolAttribute{Description: "Ask search engines not to list the page.", Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
			"show_branding":       schema.BoolAttribute{Description: "false removes \"Powered by Uptimecheck\" (paid plans).", Optional: true, Computed: true, Default: booldefault.StaticBool(true)},
			"show_response_times": schema.BoolAttribute{Description: "Each component also shows its average response time and a 24-hour line.", Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
			"project_id":          schema.StringAttribute{Description: "The project (client) it belongs to.", Optional: true},
			"components": schema.ListNestedAttribute{
				Description: "The monitors on the page, in the order visitors see them.",
				Optional:    true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"monitor_id": schema.StringAttribute{Description: "The monitor shown (mon_…).", Required: true},
					"name":       schema.StringAttribute{Description: "What visitors see.", Required: true, Validators: []validator.String{stringvalidator.LengthBetween(1, 80)}},
					"group":      schema.StringAttribute{Description: "Components with the same group fold under it.", Optional: true},
				}},
			},
		},
	}
}

func (r *statusPageResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData != nil {
		r.client = req.ProviderData.(*client.Client)
	}
}

func (r *statusPageResource) input(ctx context.Context, plan statusPageResourceModel) (client.StatusPageInput, diag.Diagnostics) {
	in := client.StatusPageInput{
		Title:        plan.Title.ValueString(),
		Description:  plan.Description.ValueString(),
		Slug:         optional(plan.Slug),
		LogoURL:      optional(plan.LogoURL),
		WebsiteURL:   optional(plan.WebsiteURL),
		AccentColor:  plan.AccentColor.ValueString(),
		Theme:        plan.Theme.ValueString(),
		Language:     plan.Language.ValueString(),
		Announcement: optional(plan.Announcement),
		Published:    plan.Published.ValueBool(),
		Noindex:      plan.Noindex.ValueBool(),
		ShowBranding: plan.ShowBranding.ValueBool(),
		ShowResponse: plan.ShowResponse.ValueBool(),
		ProjectID:    optional(plan.ProjectID),
		Components:   []client.StatusPageComponent{},
	}
	var items []statusPageComponentModel
	var diags diag.Diagnostics
	if !plan.Components.IsNull() && !plan.Components.IsUnknown() {
		diags = plan.Components.ElementsAs(ctx, &items, false)
	}
	for _, c := range items {
		in.Components = append(in.Components, client.StatusPageComponent{MonitorID: c.MonitorID.ValueString(), Name: c.Name.ValueString(), Group: optional(c.Group)})
	}
	return in, diags
}

func stringOrNull(v *string) types.String {
	if v == nil || *v == "" {
		return types.StringNull()
	}
	return types.StringValue(*v)
}

// set copies what the API returns. A page without components keeps a configuration that left them out as null.
func (m *statusPageResourceModel) set(ctx context.Context, p client.StatusPageDetail) diag.Diagnostics {
	m.ID = types.StringValue(p.ID)
	m.Title = types.StringValue(p.Title)
	m.Description = types.StringValue(p.Description)
	m.Slug = types.StringValue(p.Slug)
	m.URL = types.StringValue(p.URL)
	m.LogoURL = stringOrNull(p.LogoURL)
	m.WebsiteURL = stringOrNull(p.WebsiteURL)
	m.AccentColor = types.StringValue(p.AccentColor)
	m.Theme = types.StringValue(p.Theme)
	m.Language = types.StringValue(p.Language)
	m.Announcement = stringOrNull(p.Announcement)
	m.Published = types.BoolValue(p.Published)
	m.Noindex = types.BoolValue(p.Noindex)
	m.ShowBranding = types.BoolValue(p.ShowBranding)
	m.ShowResponse = types.BoolValue(p.ShowResponse)
	m.ProjectID = stringOrNull(p.ProjectID)
	if len(p.Components) == 0 && m.Components.IsNull() {
		return nil
	}
	items := make([]statusPageComponentModel, 0, len(p.Components))
	for _, c := range p.Components {
		items = append(items, statusPageComponentModel{MonitorID: types.StringValue(c.MonitorID), Name: types.StringValue(c.Name), Group: stringOrNull(c.Group)})
	}
	list, diags := types.ListValueFrom(ctx, statusPageComponentType, items)
	m.Components = list
	return diags
}

func (r *statusPageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan statusPageResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in, diags := r.input(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	var p client.StatusPageDetail
	if err := r.client.Do(ctx, "POST", "/status-pages", in, &p); err != nil {
		resp.Diagnostics.AddError("Couldn't create the status page", err.Error())
		return
	}
	resp.Diagnostics.Append(plan.set(ctx, p)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *statusPageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state statusPageResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var p client.StatusPageDetail
	if err := r.client.Do(ctx, "GET", "/status-pages/"+state.ID.ValueString(), nil, &p); err != nil {
		if client.NotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Couldn't read the status page", err.Error())
		return
	}
	resp.Diagnostics.Append(state.set(ctx, p)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *statusPageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state statusPageResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in, diags := r.input(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The address is sent only when it changes here: plans without custom addresses refuse any slug.
	if plan.Slug.Equal(state.Slug) {
		in.Slug = nil
	}
	var p client.StatusPageDetail
	if err := r.client.Do(ctx, "PATCH", "/status-pages/"+state.ID.ValueString(), in, &p); err != nil {
		resp.Diagnostics.AddError("Couldn't update the status page", err.Error())
		return
	}
	resp.Diagnostics.Append(plan.set(ctx, p)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *statusPageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state statusPageResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Do(ctx, "DELETE", "/status-pages/"+state.ID.ValueString(), nil, nil); err != nil && !client.NotFound(err) {
		resp.Diagnostics.AddError("Couldn't delete the status page", err.Error())
	}
}

func (r *statusPageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
