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

var _ resource.Resource = &IAMGroupRoleResource{}

type IAMGroupRoleResource struct {
	client *client.Client
}

type iamGroupRoleResourceModel struct {
	ID    types.String `tfsdk:"id"`
	Group types.String `tfsdk:"group"`
	Role  types.String `tfsdk:"role"`
}

func newIAMGroupRoleResource() resource.Resource {
	return &IAMGroupRoleResource{}
}

func (r *IAMGroupRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_group_role"
}

func (r *IAMGroupRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Binds an IAM role to a group. Changing group or role forces a new resource (destroy + re-create).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Composite ID: \"<group>/<role>\".",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"group": schema.StringAttribute{
				Required:    true,
				Description: "Group name to bind the role to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"role": schema.StringAttribute{
				Required:    true,
				Description: "Role name to bind.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *IAMGroupRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *IAMGroupRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data iamGroupRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group := data.Group.ValueString()
	role := data.Role.ValueString()

	body := map[string]interface{}{"role": role}
	respBody, status, err := r.client.Do("POST", fmt.Sprintf("/iam/groups/%s/roles", group), body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 201 {
		resp.Diagnostics.AddError("Failed to bind role to group", client.APIError(status, respBody).Error())
		return
	}

	data.ID = types.StringValue(fmt.Sprintf("%s/%s", group, role))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *IAMGroupRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data iamGroupRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group := data.Group.ValueString()
	targetRole := data.Role.ValueString()

	// Check existence by reading the group details and looking for the role
	respBody, status, err := r.client.Do("GET", fmt.Sprintf("/iam/groups/%s", group), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to read IAM group", client.APIError(status, respBody).Error())
		return
	}

	grp, err := client.DecodeJSON[client.IAMGroup](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	for _, r := range grp.Roles {
		if r.Name == targetRole {
			// Binding still exists — no state change needed
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
			return
		}
	}

	// Role not bound to group anymore — remove from state
	resp.State.RemoveResource(ctx)
}

func (r *IAMGroupRoleResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
	// All attributes have RequiresReplace, so Update is never called.
}

func (r *IAMGroupRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data iamGroupRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("DELETE",
		fmt.Sprintf("/iam/groups/%s/roles/%s", data.Group.ValueString(), data.Role.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 200 && status != 404 {
		resp.Diagnostics.AddError("Failed to unbind role from group", client.APIError(status, respBody).Error())
	}
}
