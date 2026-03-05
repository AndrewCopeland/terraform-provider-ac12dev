package provider

import (
	"context"
	"fmt"

	"github.com/AndrewCopeland/terraform-provider-ac12dev/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &IAMRoleResource{}

type IAMRoleResource struct {
	client *client.Client
}

type iamRoleResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Operations  types.String `tfsdk:"operations"`
	Resource    types.String `tfsdk:"resource"`
	Description types.String `tfsdk:"description"`
	IsSystem    types.Bool   `tfsdk:"is_system"`
	CreatedBy   types.String `tfsdk:"created_by"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func newIAMRoleResource() resource.Resource {
	return &IAMRoleResource{}
}

func (r *IAMRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_role"
}

func (r *IAMRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an IAM role — a named permission set that can be bound to groups.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Role UUID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Role name (lowercase, alphanumeric, hyphens/underscores). Changing forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"operations": schema.StringAttribute{
				Required:    true,
				Description: "Comma-separated operations this role allows. Use '*' for all, or specific names like 'list_service,get_service'. Glob patterns like 'list_*' are supported.",
			},
			"resource": schema.StringAttribute{
				Required:    true,
				Description: "Resource URI pattern this role applies to (must start with /). Examples: '/project/**', '/project/my-proj/service/*'.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Human-readable description.",
			},
			"is_system": schema.BoolAttribute{
				Computed:    true,
				Description: "True for built-in platform roles that cannot be deleted.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"created_by": schema.StringAttribute{
				Computed:    true,
				Description: "User ID who created this role.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func (r *IAMRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	pd, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.client = pd.client
}

func (r *IAMRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data iamRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name":       data.Name.ValueString(),
		"operations": data.Operations.ValueString(),
		"resource":   data.Resource.ValueString(),
	}
	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		body["description"] = data.Description.ValueString()
	}

	respBody, status, err := r.client.Do("POST", "/iam/roles", body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 201 {
		resp.Diagnostics.AddError("Failed to create IAM role", client.APIError(status, respBody).Error())
		return
	}

	role, err := client.DecodeJSON[client.IAMRole](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	setIAMRoleState(&data, role)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *IAMRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data iamRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("GET", fmt.Sprintf("/iam/roles/%s", data.Name.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to read IAM role", client.APIError(status, respBody).Error())
		return
	}

	role, err := client.DecodeJSON[client.IAMRole](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	setIAMRoleState(&data, role)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *IAMRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data iamRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"operations": data.Operations.ValueString(),
		"resource":   data.Resource.ValueString(),
	}
	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		body["description"] = data.Description.ValueString()
	} else {
		body["description"] = nil
	}

	respBody, status, err := r.client.Do("PUT", fmt.Sprintf("/iam/roles/%s", data.Name.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to update IAM role", client.APIError(status, respBody).Error())
		return
	}

	role, err := client.DecodeJSON[client.IAMRole](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	setIAMRoleState(&data, role)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *IAMRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data iamRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("DELETE", fmt.Sprintf("/iam/roles/%s", data.Name.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 200 && status != 404 {
		resp.Diagnostics.AddError("Failed to delete IAM role", client.APIError(status, respBody).Error())
	}
}

func setIAMRoleState(data *iamRoleResourceModel, role *client.IAMRole) {
	data.ID = types.StringValue(role.ID)
	data.Name = types.StringValue(role.Name)
	data.Operations = types.StringValue(role.Operations)
	data.Resource = types.StringValue(role.Resource)
	data.IsSystem = types.BoolValue(role.IsSystem)
	data.CreatedAt = types.StringValue(role.CreatedAt)
	data.UpdatedAt = types.StringValue(role.UpdatedAt)

	if role.Description != "" {
		data.Description = types.StringValue(role.Description)
	} else {
		data.Description = types.StringNull()
	}
	if role.CreatedBy != "" {
		data.CreatedBy = types.StringValue(role.CreatedBy)
	} else {
		data.CreatedBy = types.StringNull()
	}
}
