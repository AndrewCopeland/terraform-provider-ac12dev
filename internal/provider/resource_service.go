package provider

import (
	"context"
	"fmt"

	"github.com/AndrewCopeland/terraform-provider-ac12dev/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &ServiceResource{}

type ServiceResource struct {
	client           *client.Client
	defaultProjectID string
}

type serviceResourceModel struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	Name      types.String `tfsdk:"name"`
	Image     types.String `tfsdk:"image"`
	Port      types.Int64  `tfsdk:"port"`
	Env       types.Map    `tfsdk:"env"`
	Daemon    types.Bool   `tfsdk:"daemon"`
	Status    types.String `tfsdk:"status"`
	URL       types.String `tfsdk:"url"`
	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

func newServiceResource() resource.Resource {
	return &ServiceResource{}
}

func (r *ServiceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service"
}

func (r *ServiceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an ac12.dev container service. Creating deploys the container; updates trigger a force-redeploy.",
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
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Service name. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"image": schema.StringAttribute{
				Required:    true,
				Description: "Docker image reference (e.g. my-app:latest).",
			},
			"port": schema.Int64Attribute{
				Optional:    true,
				Description: "Container port to expose.",
			},
			"env": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Environment variables. Use secret refs as values: \"secret:MY_SECRET_NAME\".",
			},
			"daemon": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Run as a persistent daemon (default true). Set false for one-shot jobs.",
				Default:     booldefault.StaticBool(true),
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Current service status (running, stopped, created).",
			},
			"url": schema.StringAttribute{
				Computed:    true,
				Description: "Proxy URL for the service.",
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

func (r *ServiceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ServiceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data serviceResourceModel
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
		"name":   data.Name.ValueString(),
		"image":  data.Image.ValueString(),
		"daemon": data.Daemon.ValueBool(),
		"deploy": true,
	}
	if !data.Port.IsNull() && !data.Port.IsUnknown() {
		body["port"] = data.Port.ValueInt64()
	}
	if !data.Env.IsNull() && !data.Env.IsUnknown() {
		body["env"] = mapToStringMap(data.Env)
	}

	respBody, status, err := r.client.Do("POST", fmt.Sprintf("/projects/%s/services", pid), body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 201 {
		resp.Diagnostics.AddError("Failed to create service", client.APIError(status, respBody).Error())
		return
	}

	svc, err := client.DecodeJSON[client.Service](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	r.setStateFromService(&data, svc)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data serviceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("GET",
		fmt.Sprintf("/projects/%s/services/%s", data.ProjectID.ValueString(), data.Name.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to read service", client.APIError(status, respBody).Error())
		return
	}

	svc, err := client.DecodeJSON[client.Service](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	r.setStateFromService(&data, svc)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data serviceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pid := data.ProjectID.ValueString()
	name := data.Name.ValueString()

	// Deploy with updated values (force-recreate picks up new image tags etc.)
	deployBody := map[string]interface{}{
		"image":  data.Image.ValueString(),
		"daemon": data.Daemon.ValueBool(),
	}
	if !data.Port.IsNull() && !data.Port.IsUnknown() {
		deployBody["port"] = data.Port.ValueInt64()
	}
	if !data.Env.IsNull() && !data.Env.IsUnknown() {
		deployBody["env"] = mapToStringMap(data.Env)
	}

	respBody, status, err := r.client.Do("POST",
		fmt.Sprintf("/projects/%s/services/%s/deploy", pid, name), deployBody)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to deploy service", client.APIError(status, respBody).Error())
		return
	}

	// Read back the full service state
	readBody, status, err := r.client.Do("GET", fmt.Sprintf("/projects/%s/services/%s", pid, name), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed (re-read)", err.Error())
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to read service after update", client.APIError(status, readBody).Error())
		return
	}

	svc, err := client.DecodeJSON[client.Service](readBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	r.setStateFromService(&data, svc)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data serviceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("DELETE",
		fmt.Sprintf("/projects/%s/services/%s", data.ProjectID.ValueString(), data.Name.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 204 && status != 404 {
		resp.Diagnostics.AddError("Failed to delete service", client.APIError(status, respBody).Error())
	}
}

func (r *ServiceResource) setStateFromService(data *serviceResourceModel, svc *client.Service) {
	data.ID = types.StringValue(svc.ID)
	data.Name = types.StringValue(svc.Name)
	data.Image = types.StringValue(svc.Image)
	data.Daemon = types.BoolValue(svc.Daemon)
	data.Status = types.StringValue(svc.Status)
	data.URL = types.StringValue(svc.URL)
	data.CreatedAt = types.StringValue(svc.CreatedAt)
	data.UpdatedAt = types.StringValue(svc.UpdatedAt)

	if svc.Port != nil {
		data.Port = types.Int64Value(*svc.Port)
	} else {
		data.Port = types.Int64Null()
	}

	if svc.Env != nil {
		envMap := make(map[string]attr.Value, len(svc.Env))
		for k, v := range svc.Env {
			envMap[k] = types.StringValue(v)
		}
		envVal, _ := types.MapValue(types.StringType, envMap)
		data.Env = envVal
	} else {
		data.Env = types.MapNull(types.StringType)
	}
}

// mapToStringMap converts a types.Map to map[string]string for API calls.
func mapToStringMap(m types.Map) map[string]string {
	result := make(map[string]string, len(m.Elements()))
	for k, v := range m.Elements() {
		result[k] = v.(types.String).ValueString()
	}
	return result
}
