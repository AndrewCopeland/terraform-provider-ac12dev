package provider

import (
	"context"
	"fmt"

	"github.com/AndrewCopeland/terraform-provider-ac12dev/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &ProjectDataSource{}

type ProjectDataSource struct {
	client *client.Client
}

type projectDataSourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Slug         types.String `tfsdk:"slug"`
	OwnerID      types.String `tfsdk:"owner_id"`
	IsDefault    types.Bool   `tfsdk:"is_default"`
	ServiceCount types.Int64  `tfsdk:"service_count"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

func newProjectDataSource() datasource.DataSource {
	return &ProjectDataSource{}
}

func (d *ProjectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *ProjectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an ac12.dev project by ID or name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Project UUID. Provide this OR name.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Project name. Provide this OR id.",
			},
			"slug": schema.StringAttribute{
				Computed:    true,
				Description: "URL-safe slug.",
			},
			"owner_id": schema.StringAttribute{
				Computed:    true,
				Description: "User ID of the owner.",
			},
			"is_default": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether this is the user's default project.",
			},
			"service_count": schema.Int64Attribute{
				Computed:    true,
				Description: "Number of services in the project.",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "ISO 8601 creation timestamp.",
			},
		},
	}
}

func (d *ProjectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	pd, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	d.client = pd.client
}

func (d *ProjectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data projectDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsNull() && data.Name.IsNull() {
		resp.Diagnostics.AddError("Missing lookup key", "Provide either id or name to look up a project.")
		return
	}

	// Look up by ID directly if provided
	if !data.ID.IsNull() && data.ID.ValueString() != "" {
		respBody, status, err := d.client.Do("GET", fmt.Sprintf("/projects/%s", data.ID.ValueString()), nil)
		if err != nil {
			resp.Diagnostics.AddError("API request failed", err.Error())
			return
		}
		if status == 404 {
			resp.Diagnostics.AddError("Project not found", fmt.Sprintf("No project with id %q", data.ID.ValueString()))
			return
		}
		if status != 200 {
			resp.Diagnostics.AddError("Failed to read project", client.APIError(status, respBody).Error())
			return
		}
		proj, err := client.DecodeJSON[client.Project](respBody)
		if err != nil {
			resp.Diagnostics.AddError("Failed to parse response", err.Error())
			return
		}
		setProjectDataSourceState(&data, proj)
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		return
	}

	// Look up by name: list all projects and filter
	respBody, status, err := d.client.Do("GET", "/projects", nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to list projects", client.APIError(status, respBody).Error())
		return
	}

	projects, err := client.DecodeJSON[[]client.Project](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	targetName := data.Name.ValueString()
	var found *client.Project
	for i := range *projects {
		if (*projects)[i].Name == targetName {
			found = &(*projects)[i]
			break
		}
	}
	if found == nil {
		resp.Diagnostics.AddError("Project not found", fmt.Sprintf("No project with name %q", targetName))
		return
	}

	setProjectDataSourceState(&data, found)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func setProjectDataSourceState(data *projectDataSourceModel, proj *client.Project) {
	data.ID = types.StringValue(proj.ID)
	data.Name = types.StringValue(proj.Name)
	data.Slug = types.StringValue(proj.Slug)
	data.OwnerID = types.StringValue(proj.OwnerID)
	data.IsDefault = types.BoolValue(proj.IsDefault)
	data.ServiceCount = types.Int64Value(int64(proj.ServiceCount))
	data.CreatedAt = types.StringValue(proj.CreatedAt)
}
