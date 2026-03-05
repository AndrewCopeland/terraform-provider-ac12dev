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

var _ resource.Resource = &IAMGroupResource{}

type IAMGroupResource struct {
	client *client.Client
}

type iamGroupResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	IsPersonal  types.Bool   `tfsdk:"is_personal"`
	OwnerID     types.String `tfsdk:"owner_id"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func newIAMGroupResource() resource.Resource {
	return &IAMGroupResource{}
}

func (r *IAMGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_group"
}

func (r *IAMGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an IAM group. Roles are bound via ac12dev_iam_group_role; members via ac12dev_iam_group_member.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Group UUID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Group name (lowercase, alphanumeric, hyphens/underscores). Cannot start with 'user-'. Changing forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Human-readable description.",
			},
			"is_personal": schema.BoolAttribute{
				Computed:    true,
				Description: "True for auto-created personal groups (one per user). Personal groups cannot be deleted.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"owner_id": schema.StringAttribute{
				Computed:    true,
				Description: "User ID of the group owner.",
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

func (r *IAMGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *IAMGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data iamGroupResourceModel
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

	respBody, status, err := r.client.Do("POST", "/iam/groups", body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 201 {
		resp.Diagnostics.AddError("Failed to create IAM group", client.APIError(status, respBody).Error())
		return
	}

	grp, err := client.DecodeJSON[client.IAMGroup](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	setIAMGroupState(&data, grp)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *IAMGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data iamGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("GET", fmt.Sprintf("/iam/groups/%s", data.Name.ValueString()), nil)
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

	setIAMGroupState(&data, grp)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *IAMGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data iamGroupResourceModel
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

	respBody, status, err := r.client.Do("PUT", fmt.Sprintf("/iam/groups/%s", data.Name.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to update IAM group", client.APIError(status, respBody).Error())
		return
	}

	grp, err := client.DecodeJSON[client.IAMGroup](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	setIAMGroupState(&data, grp)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *IAMGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data iamGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("DELETE", fmt.Sprintf("/iam/groups/%s", data.Name.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 200 && status != 404 {
		resp.Diagnostics.AddError("Failed to delete IAM group", client.APIError(status, respBody).Error())
	}
}

func setIAMGroupState(data *iamGroupResourceModel, grp *client.IAMGroup) {
	data.ID = types.StringValue(grp.ID)
	data.Name = types.StringValue(grp.Name)
	data.IsPersonal = types.BoolValue(grp.IsPersonal)
	data.CreatedAt = types.StringValue(grp.CreatedAt)
	data.UpdatedAt = types.StringValue(grp.UpdatedAt)

	if grp.Description != "" {
		data.Description = types.StringValue(grp.Description)
	} else {
		data.Description = types.StringNull()
	}
	if grp.OwnerID != "" {
		data.OwnerID = types.StringValue(grp.OwnerID)
	} else {
		data.OwnerID = types.StringNull()
	}
}
