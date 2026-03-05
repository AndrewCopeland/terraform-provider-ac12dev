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

var _ resource.Resource = &IAMGroupMemberResource{}

type IAMGroupMemberResource struct {
	client *client.Client
}

type iamGroupMemberResourceModel struct {
	ID       types.String `tfsdk:"id"`
	Group    types.String `tfsdk:"group"`
	Username types.String `tfsdk:"username"`
	AddedAt  types.String `tfsdk:"added_at"`
}

func newIAMGroupMemberResource() resource.Resource {
	return &IAMGroupMemberResource{}
}

func (r *IAMGroupMemberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_group_member"
}

func (r *IAMGroupMemberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Adds a user to an IAM group. Changing group or username forces a new resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Composite ID: \"<group>/<username>\".",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"group": schema.StringAttribute{
				Required:    true,
				Description: "Group name to add the user to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"username": schema.StringAttribute{
				Required:    true,
				Description: "Username of the user to add.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"added_at": schema.StringAttribute{
				Computed:    true,
				Description: "Timestamp when the user was added to the group.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *IAMGroupMemberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *IAMGroupMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data iamGroupMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group := data.Group.ValueString()
	username := data.Username.ValueString()

	body := map[string]interface{}{"username": username}
	respBody, status, err := r.client.Do("POST", fmt.Sprintf("/iam/groups/%s/members", group), body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 201 {
		resp.Diagnostics.AddError("Failed to add member to group", client.APIError(status, respBody).Error())
		return
	}

	data.ID = types.StringValue(fmt.Sprintf("%s/%s", group, username))

	// Read back the group to get the added_at timestamp
	grpBody, grpStatus, grpErr := r.client.Do("GET", fmt.Sprintf("/iam/groups/%s", group), nil)
	if grpErr == nil && grpStatus == 200 {
		if grp, err := client.DecodeJSON[client.IAMGroup](grpBody); err == nil {
			for _, m := range grp.Members {
				if m.Username == username {
					data.AddedAt = types.StringValue(m.AddedAt)
					break
				}
			}
		}
	}
	if data.AddedAt.IsUnknown() {
		data.AddedAt = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *IAMGroupMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data iamGroupMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group := data.Group.ValueString()
	username := data.Username.ValueString()

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

	for _, m := range grp.Members {
		if m.Username == username {
			data.AddedAt = types.StringValue(m.AddedAt)
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
			return
		}
	}

	// User not in group anymore
	resp.State.RemoveResource(ctx)
}

func (r *IAMGroupMemberResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
	// All attributes have RequiresReplace — Update is never called.
}

func (r *IAMGroupMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data iamGroupMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("DELETE",
		fmt.Sprintf("/iam/groups/%s/members/%s", data.Group.ValueString(), data.Username.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 200 && status != 404 {
		resp.Diagnostics.AddError("Failed to remove member from group", client.APIError(status, respBody).Error())
	}
}
