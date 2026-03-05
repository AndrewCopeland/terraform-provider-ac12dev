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

var _ resource.Resource = &EmailAccountResource{}

type EmailAccountResource struct {
	client *client.Client
}

type emailAccountResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Address     types.String `tfsdk:"address"`
	DisplayName types.String `tfsdk:"display_name"`
	FullAddress types.String `tfsdk:"full_address"`
	UserID      types.String `tfsdk:"user_id"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func newEmailAccountResource() resource.Resource {
	return &EmailAccountResource{}
}

func (r *EmailAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_email_account"
}

func (r *EmailAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Provisions an @ac12.dev email account (mailbox). Changing address or display_name forces a new resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Account UUID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"address": schema.StringAttribute{
				Required:    true,
				Description: "Local part of the email address (e.g. \"mybot\" gives mybot@ac12.dev). Lowercase alphanumeric with dots, hyphens, underscores. Changing forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"display_name": schema.StringAttribute{
				Optional:    true,
				Description: "Human-readable sender name. Changing forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"full_address": schema.StringAttribute{
				Computed:    true,
				Description: "Full email address including domain (e.g. mybot@ac12.dev).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"user_id": schema.StringAttribute{
				Computed:    true,
				Description: "Platform user ID who owns this account.",
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
		},
	}
}

func (r *EmailAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *EmailAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data emailAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"address": data.Address.ValueString(),
	}
	if !data.DisplayName.IsNull() && !data.DisplayName.IsUnknown() {
		body["display_name"] = data.DisplayName.ValueString()
	}

	respBody, status, err := r.client.Do("POST", "/email/accounts", body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 201 {
		resp.Diagnostics.AddError("Failed to create email account", client.APIError(status, respBody).Error())
		return
	}

	acct, err := client.DecodeJSON[client.EmailAccount](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	setEmailAccountState(&data, acct)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *EmailAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data emailAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("GET", fmt.Sprintf("/email/accounts/%s", data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to read email account", client.APIError(status, respBody).Error())
		return
	}

	acct, err := client.DecodeJSON[client.EmailAccount](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	setEmailAccountState(&data, acct)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *EmailAccountResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
	// All mutable attributes use RequiresReplace — Update is never called.
}

func (r *EmailAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data emailAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("DELETE", fmt.Sprintf("/email/accounts/%s", data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 204 && status != 404 {
		resp.Diagnostics.AddError("Failed to delete email account", client.APIError(status, respBody).Error())
	}
}

func setEmailAccountState(data *emailAccountResourceModel, acct *client.EmailAccount) {
	data.ID = types.StringValue(acct.ID)
	data.FullAddress = types.StringValue(acct.Address)
	data.UserID = types.StringValue(acct.UserID)
	data.CreatedAt = types.StringValue(acct.CreatedAt)

	if acct.DisplayName != "" {
		data.DisplayName = types.StringValue(acct.DisplayName)
	} else {
		data.DisplayName = types.StringNull()
	}
}
