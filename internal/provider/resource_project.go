package provider

import (
	"context"
	"fmt"

	"github.com/AndrewCopeland/terraform-provider-ac12dev/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &ProjectResource{}

type ProjectResource struct {
	client           *client.Client
	defaultProjectID string
}

type projectResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Slug        types.String `tfsdk:"slug"`
	OwnerID     types.String `tfsdk:"owner_id"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func newProjectResource() resource.Resource {
	return &ProjectResource{}
}

func (r *ProjectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *ProjectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an ac12.dev project.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Project UUID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Project name (2–64 chars).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Optional project description.",
			},
			"slug": schema.StringAttribute{
				Computed:    true,
				Description: "URL-safe slug derived from name.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"owner_id": schema.StringAttribute{
				Computed:    true,
				Description: "User ID of the project owner.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"is_default": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether this is the user's default project.",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "ISO 8601 creation timestamp.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *ProjectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	pd, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.client = pd.client
	r.defaultProjectID = pd.defaultProjectID
}

func (r *ProjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name": data.Name.ValueString(),
	}
	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		body["description"] = data.Description.ValueString()
	}

	respBody, status, err := r.client.Do("POST", "/projects", body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 201 {
		resp.Diagnostics.AddError("Failed to create project", client.APIError(status, respBody).Error())
		return
	}

	proj, err := client.DecodeJSON[client.Project](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	r.setStateFromProject(&data, proj)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ProjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("GET", fmt.Sprintf("/projects/%s", data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status == 404 {
		resp.State.RemoveResource(ctx)
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

	r.setStateFromProject(&data, proj)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ProjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{}
	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		body["description"] = data.Description.ValueString()
	} else {
		body["description"] = nil
	}

	respBody, status, err := r.client.Do("PUT", fmt.Sprintf("/projects/%s", data.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to update project", client.APIError(status, respBody).Error())
		return
	}

	proj, err := client.DecodeJSON[client.Project](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	r.setStateFromProject(&data, proj)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ProjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("DELETE", fmt.Sprintf("/projects/%s", data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 204 && status != 404 {
		resp.Diagnostics.AddError("Failed to delete project", client.APIError(status, respBody).Error())
	}
}

func (r *ProjectResource) setStateFromProject(data *projectResourceModel, proj *client.Project) {
	data.ID = types.StringValue(proj.ID)
	data.Name = types.StringValue(proj.Name)
	data.Slug = types.StringValue(proj.Slug)
	data.OwnerID = types.StringValue(proj.OwnerID)
	data.IsDefault = types.BoolValue(proj.IsDefault)
	data.CreatedAt = types.StringValue(proj.CreatedAt)
}
