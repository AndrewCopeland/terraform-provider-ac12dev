package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/AndrewCopeland/terraform-provider-ac12dev/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &CronJobResource{}

type CronJobResource struct {
	client           *client.Client
	defaultProjectID string
}

type cronJobResourceModel struct {
	ID            types.String `tfsdk:"id"`
	ProjectID     types.String `tfsdk:"project_id"`
	Name          types.String `tfsdk:"name"`
	Schedule      types.String `tfsdk:"schedule"`
	TargetType    types.String `tfsdk:"target_type"`
	TargetService types.String `tfsdk:"target_service"`
	TargetPath    types.String `tfsdk:"target_path"`
	HTTPMethod    types.String `tfsdk:"http_method"`
	HTTPBody      types.String `tfsdk:"http_body"`
	Enabled       types.Bool   `tfsdk:"enabled"`
	LastRunAt     types.String `tfsdk:"last_run_at"`
	LastStatus    types.String `tfsdk:"last_status"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

func newCronJobResource() resource.Resource {
	return &CronJobResource{}
}

func (r *CronJobResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cron_job"
}

func (r *CronJobResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a scheduled HTTP task (cron job) that fires against a service or agent.",
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
				Description: "Cron job name. Changing forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"schedule": schema.StringAttribute{
				Required:    true,
				Description: "Cron expression (e.g. \"0 * * * *\" for hourly).",
			},
			"target_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Target type: \"service\" (default) or \"agent\".",
				Default:     stringdefault.StaticString("service"),
			},
			"target_service": schema.StringAttribute{
				Optional:    true,
				Description: "Service name to call (required when target_type = \"service\").",
			},
			"target_path": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "URL path on the service to call (default \"/\").",
				Default:     stringdefault.StaticString("/"),
			},
			"http_method": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "HTTP method to use (default \"GET\").",
				Default:     stringdefault.StaticString("GET"),
			},
			"http_body": schema.StringAttribute{
				Optional:    true,
				Description: "Optional request body for POST/PUT cron calls.",
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the cron job is active (default true).",
				Default:     booldefault.StaticBool(true),
			},
			"last_run_at": schema.StringAttribute{
				Computed:    true,
				Description: "Timestamp of the most recent execution.",
			},
			"last_status": schema.StringAttribute{
				Computed:    true,
				Description: "Status of the most recent execution.",
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

func (r *CronJobResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *CronJobResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data cronJobResourceModel
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
		"name":        data.Name.ValueString(),
		"schedule":    data.Schedule.ValueString(),
		"target_type": data.TargetType.ValueString(),
		"target_path": data.TargetPath.ValueString(),
		"http_method": data.HTTPMethod.ValueString(),
		"enabled":     data.Enabled.ValueBool(),
	}
	if !data.TargetService.IsNull() && !data.TargetService.IsUnknown() {
		body["target_service"] = data.TargetService.ValueString()
	}
	if !data.HTTPBody.IsNull() && !data.HTTPBody.IsUnknown() {
		body["http_body"] = data.HTTPBody.ValueString()
	}

	respBody, status, err := r.client.Do("POST", fmt.Sprintf("/projects/%s/cron", pid), body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 201 {
		resp.Diagnostics.AddError("Failed to create cron job", client.APIError(status, respBody).Error())
		return
	}

	job, err := client.DecodeJSON[client.CronJob](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	setCronJobState(&data, job)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CronJobResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data cronJobResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("GET",
		fmt.Sprintf("/projects/%s/cron/%s", data.ProjectID.ValueString(), data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to read cron job", client.APIError(status, respBody).Error())
		return
	}

	job, err := client.DecodeJSON[client.CronJob](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	setCronJobState(&data, job)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CronJobResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data cronJobResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"schedule":    data.Schedule.ValueString(),
		"target_type": data.TargetType.ValueString(),
		"target_path": data.TargetPath.ValueString(),
		"http_method": data.HTTPMethod.ValueString(),
		"enabled":     data.Enabled.ValueBool(),
	}
	if !data.TargetService.IsNull() && !data.TargetService.IsUnknown() {
		body["target_service"] = data.TargetService.ValueString()
	}
	if !data.HTTPBody.IsNull() && !data.HTTPBody.IsUnknown() {
		body["http_body"] = data.HTTPBody.ValueString()
	}

	respBody, status, err := r.client.Do("PATCH",
		fmt.Sprintf("/projects/%s/cron/%s", data.ProjectID.ValueString(), data.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to update cron job", client.APIError(status, respBody).Error())
		return
	}

	job, err := client.DecodeJSON[client.CronJob](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	setCronJobState(&data, job)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CronJobResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data cronJobResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("DELETE",
		fmt.Sprintf("/projects/%s/cron/%s", data.ProjectID.ValueString(), data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 204 && status != 404 {
		resp.Diagnostics.AddError("Failed to delete cron job", client.APIError(status, respBody).Error())
	}
}

func setCronJobState(data *cronJobResourceModel, job *client.CronJob) {
	data.ID = types.StringValue(job.ID)
	data.Name = types.StringValue(job.Name)
	data.Schedule = types.StringValue(job.Schedule)
	data.TargetType = types.StringValue(job.TargetType)
	data.TargetPath = types.StringValue(job.TargetPath)
	data.HTTPMethod = types.StringValue(job.HTTPMethod)
	data.Enabled = types.BoolValue(job.Enabled)
	data.CreatedAt = types.StringValue(job.CreatedAt)
	data.UpdatedAt = types.StringValue(job.UpdatedAt)

	if job.TargetService != "" {
		data.TargetService = types.StringValue(job.TargetService)
	} else {
		data.TargetService = types.StringNull()
	}
	if job.HTTPBody != "" {
		data.HTTPBody = types.StringValue(job.HTTPBody)
	} else {
		data.HTTPBody = types.StringNull()
	}
	if job.LastRunAt != "" {
		data.LastRunAt = types.StringValue(job.LastRunAt)
	} else {
		data.LastRunAt = types.StringNull()
	}
	if job.LastStatus != nil {
		switch v := job.LastStatus.(type) {
		case float64:
			data.LastStatus = types.StringValue(strconv.Itoa(int(v)))
		case string:
			data.LastStatus = types.StringValue(v)
		default:
			data.LastStatus = types.StringValue(fmt.Sprintf("%v", v))
		}
	} else {
		data.LastStatus = types.StringNull()
	}
}
