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

var _ resource.Resource = &DomainResource{}

type DomainResource struct {
	client           *client.Client
	defaultProjectID string
}

type domainResourceModel struct {
	ID            types.String `tfsdk:"id"`
	ProjectID     types.String `tfsdk:"project_id"`
	Subdomain     types.String `tfsdk:"subdomain"`
	TargetType    types.String `tfsdk:"target_type"`
	TargetService types.String `tfsdk:"target_service"`
	TargetPath    types.String `tfsdk:"target_path"`
	CustomDomain  types.String `tfsdk:"custom_domain"`
	URL           types.String `tfsdk:"url"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

func newDomainResource() resource.Resource {
	return &DomainResource{}
}

func (r *DomainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (r *DomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Maps a subdomain (e.g. myapp.p.ac12.dev) to a service or file path.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Project ID. Defaults to the provider-level project_id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"subdomain": schema.StringAttribute{
				Required:    true,
				Description: "Subdomain name (e.g. \"myapp\" → myapp.p.ac12.dev). Changing forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"target_type": schema.StringAttribute{
				Required:    true,
				Description: "Target type: \"service\" or \"file\".",
			},
			"target_service": schema.StringAttribute{
				Optional:    true,
				Description: "Service name to route traffic to (required when target_type = \"service\").",
			},
			"target_path": schema.StringAttribute{
				Optional:    true,
				Description: "File path prefix to serve (required when target_type = \"file\").",
			},
			"custom_domain": schema.StringAttribute{
				Optional:    true,
				Description: "Optional custom domain (e.g. \"myapp.example.com\"). Requires a CNAME DNS record.",
			},
			"url": schema.StringAttribute{
				Computed:    true,
				Description: "Generated HTTPS URL for this subdomain.",
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

func (r *DomainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *DomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data domainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pid, ok := resolveProjectID(data.ProjectID.ValueString(), r.defaultProjectID)
	if !ok {
		resp.Diagnostics.AddError("Missing project_id", "Set project_id on the resource or in the provider block.")
		return
	}
	data.ProjectID = types.StringValue(pid)

	body := map[string]interface{}{
		"subdomain":   data.Subdomain.ValueString(),
		"target_type": data.TargetType.ValueString(),
	}
	if !data.TargetService.IsNull() && !data.TargetService.IsUnknown() {
		body["target_service"] = data.TargetService.ValueString()
	}
	if !data.TargetPath.IsNull() && !data.TargetPath.IsUnknown() {
		body["target_path"] = data.TargetPath.ValueString()
	}
	if !data.CustomDomain.IsNull() && !data.CustomDomain.IsUnknown() {
		body["custom_domain"] = data.CustomDomain.ValueString()
	}

	respBody, status, err := r.client.Do("POST", fmt.Sprintf("/projects/%s/domains", pid), body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 201 {
		resp.Diagnostics.AddError("Failed to create domain", client.APIError(status, respBody).Error())
		return
	}

	d, err := client.DecodeJSON[client.Domain](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	setDomainState(&data, d)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data domainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// No GET-by-ID endpoint — list all domains and find by ID
	respBody, status, err := r.client.Do("GET",
		fmt.Sprintf("/projects/%s/domains", data.ProjectID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to list domains", client.APIError(status, respBody).Error())
		return
	}

	domains, err := client.DecodeJSON[[]client.Domain](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	var found *client.Domain
	for i := range *domains {
		if (*domains)[i].ID == data.ID.ValueString() {
			found = &(*domains)[i]
			break
		}
	}
	if found == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	setDomainState(&data, found)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DomainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data domainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"target_type": data.TargetType.ValueString(),
	}
	if !data.TargetService.IsNull() && !data.TargetService.IsUnknown() {
		body["target_service"] = data.TargetService.ValueString()
	}
	if !data.TargetPath.IsNull() && !data.TargetPath.IsUnknown() {
		body["target_path"] = data.TargetPath.ValueString()
	}
	if !data.CustomDomain.IsNull() {
		body["custom_domain"] = data.CustomDomain.ValueString()
	}

	respBody, status, err := r.client.Do("PATCH",
		fmt.Sprintf("/projects/%s/domains/%s", data.ProjectID.ValueString(), data.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to update domain", client.APIError(status, respBody).Error())
		return
	}

	d, err := client.DecodeJSON[client.Domain](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	setDomainState(&data, d)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data domainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("DELETE",
		fmt.Sprintf("/projects/%s/domains/%s", data.ProjectID.ValueString(), data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 204 && status != 404 {
		resp.Diagnostics.AddError("Failed to delete domain", client.APIError(status, respBody).Error())
	}
}

func setDomainState(data *domainResourceModel, d *client.Domain) {
	data.ID = types.StringValue(d.ID)
	data.Subdomain = types.StringValue(d.Subdomain)
	data.TargetType = types.StringValue(d.TargetType)
	data.URL = types.StringValue(d.URL)
	data.CreatedAt = types.StringValue(d.CreatedAt)
	data.UpdatedAt = types.StringValue(d.UpdatedAt)

	if d.TargetService != "" {
		data.TargetService = types.StringValue(d.TargetService)
	} else {
		data.TargetService = types.StringNull()
	}
	if d.TargetPath != "" {
		data.TargetPath = types.StringValue(d.TargetPath)
	} else {
		data.TargetPath = types.StringNull()
	}
	if d.CustomDomain != "" {
		data.CustomDomain = types.StringValue(d.CustomDomain)
	} else {
		data.CustomDomain = types.StringNull()
	}
}
