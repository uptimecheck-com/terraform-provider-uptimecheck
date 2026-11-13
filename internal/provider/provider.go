// Package provider implements the uptimecheck Terraform provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/uptimecheck-com/terraform-provider-uptimecheck/internal/client"
)

// DefaultEndpoint is the production API.
const DefaultEndpoint = "https://platform.uptimecheck.com/api/v1"

type uptimecheckProvider struct{ version string }

type providerModel struct {
	APIKey   types.String `tfsdk:"api_key"`
	Endpoint types.String `tfsdk:"endpoint"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &uptimecheckProvider{version: version} }
}

func (p *uptimecheckProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "uptimecheck"
	resp.Version = p.version
}

func (p *uptimecheckProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Uptimecheck monitors as code, and look up alert channels, projects and status pages.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Description: "A read & write API key (Settings › API in the app). Defaults to the UPTIMECHECK_API_KEY environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
			"endpoint": schema.StringAttribute{
				Description: "The API's base URL. Defaults to UPTIMECHECK_ENDPOINT, else " + DefaultEndpoint + ".",
				Optional:    true,
			},
		},
	}
}

func (p *uptimecheckProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	apiKey := os.Getenv("UPTIMECHECK_API_KEY")
	if !config.APIKey.IsNull() && !config.APIKey.IsUnknown() {
		apiKey = config.APIKey.ValueString()
	}
	endpoint := DefaultEndpoint
	if env := os.Getenv("UPTIMECHECK_ENDPOINT"); env != "" {
		endpoint = env
	}
	if !config.Endpoint.IsNull() && !config.Endpoint.IsUnknown() {
		endpoint = config.Endpoint.ValueString()
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(path.Root("api_key"), "Missing API key", "Set api_key in the provider block or the UPTIMECHECK_API_KEY environment variable. Create a key under Settings › API.")
		return
	}
	c := client.New(endpoint, apiKey, "terraform-provider-uptimecheck/"+p.version)
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *uptimecheckProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewMonitorResource, NewAlertChannelResource, NewProjectResource}
}

func (p *uptimecheckProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{NewAlertChannelsDataSource, NewProjectsDataSource, NewStatusPagesDataSource}
}
