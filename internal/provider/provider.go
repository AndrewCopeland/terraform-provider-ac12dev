package provider

import (
	"context"
	"os"

	"github.com/AndrewCopeland/terraform-provider-ac12dev/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the provider satisfies the framework interface.
var _ provider.Provider = &AC12DevProvider{}

// providerData is passed to resources and data sources via Configure.
type providerData struct {
	client           *client.Client
	defaultProjectID string
}

// AC12DevProvider implements the Terraform provider.
type AC12DevProvider struct{}

// providerModel mirrors the provider HCL configuration block.
type providerModel struct {
	Server     types.String `tfsdk:"server"`
	Username   types.String `tfsdk:"username"`
	PrivateKey types.String `tfsdk:"private_key"`
	ProjectID  types.String `tfsdk:"project_id"`
}

// New returns the provider factory function.
func New() func() provider.Provider {
	return func() provider.Provider {
		return &AC12DevProvider{}
	}
}

func (p *AC12DevProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "ac12dev"
}

func (p *AC12DevProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Interact with the ac12.dev PaaS platform.",
		Attributes: map[string]schema.Attribute{
			"server": schema.StringAttribute{
				Optional:    true,
				Description: "Base URL of the ac12.dev API. Defaults to https://ac12.dev. Can be set via AC12DEV_SERVER env var.",
			},
			"username": schema.StringAttribute{
				Optional:    true,
				Description: "ac12.dev username. Can be set via AC12DEV_USERNAME env var.",
			},
			"private_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Ed25519 private key in PEM format. Use file() to read from disk. Can be set via AC12DEV_PRIVATE_KEY env var.",
			},
			"project_id": schema.StringAttribute{
				Optional:    true,
				Description: "Default project ID for all resources. Can be set via AC12DEV_PROJECT_ID env var.",
			},
		},
	}
}

func (p *AC12DevProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	server := firstNonEmpty(config.Server.ValueString(), os.Getenv("AC12DEV_SERVER"), "https://ac12.dev")
	username := firstNonEmpty(config.Username.ValueString(), os.Getenv("AC12DEV_USERNAME"))
	privateKey := firstNonEmpty(config.PrivateKey.ValueString(), os.Getenv("AC12DEV_PRIVATE_KEY"))
	projectID := firstNonEmpty(config.ProjectID.ValueString(), os.Getenv("AC12DEV_PROJECT_ID"))

	if username == "" {
		resp.Diagnostics.AddError(
			"Missing username",
			"Set username in the provider block or via AC12DEV_USERNAME.",
		)
	}
	if privateKey == "" {
		resp.Diagnostics.AddError(
			"Missing private_key",
			"Set private_key in the provider block or via AC12DEV_PRIVATE_KEY.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	c, err := client.New(server, username, privateKey)
	if err != nil {
		resp.Diagnostics.AddError("Invalid private key", err.Error())
		return
	}

	pd := &providerData{
		client:           c,
		defaultProjectID: projectID,
	}
	resp.DataSourceData = pd
	resp.ResourceData = pd
}

func (p *AC12DevProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newProjectResource,
		newServiceResource,
		newDomainResource,
		newCronJobResource,
		newFileResource,
		newSecretResource,
		newIAMRoleResource,
		newIAMGroupResource,
		newIAMGroupRoleResource,
		newIAMGroupMemberResource,
		newEmailAccountResource,
		newAgentResource,
	}
}

func (p *AC12DevProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newProjectDataSource,
	}
}

// firstNonEmpty returns the first non-empty string from candidates.
func firstNonEmpty(candidates ...string) string {
	for _, s := range candidates {
		if s != "" {
			return s
		}
	}
	return ""
}

// resolveProjectID returns the resource-level project_id or the provider default.
func resolveProjectID(resourceLevel, defaultLevel string) (string, bool) {
	if resourceLevel != "" {
		return resourceLevel, true
	}
	if defaultLevel != "" {
		return defaultLevel, true
	}
	return "", false
}
