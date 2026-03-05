package provider

import (
	"context"
	"fmt"

	"github.com/AndrewCopeland/terraform-provider-ac12dev/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &AgentResource{}

type AgentResource struct {
	client           *client.Client
	defaultProjectID string
}

type agentResourceModel struct {
	ID               types.String `tfsdk:"id"`
	ProjectID        types.String `tfsdk:"project_id"`
	Name             types.String `tfsdk:"name"`
	AgentType        types.String `tfsdk:"agent_type"`
	Model            types.String `tfsdk:"model"`
	SystemPrompt     types.String `tfsdk:"system_prompt"`
	Skills           types.List   `tfsdk:"skills"`
	EffectiveSkills  types.List   `tfsdk:"effective_skills"`
	IdentityUsername types.String `tfsdk:"identity_username"`
	CreatedAt        types.String `tfsdk:"created_at"`
	UpdatedAt        types.String `tfsdk:"updated_at"`
}

func newAgentResource() resource.Resource {
	return &AgentResource{}
}

func (r *AgentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_agent"
}

func (r *AgentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an AI coding agent. The agent gets its own platform identity (Ed25519 keypair) automatically. Use the ac12dev CLI to trigger runs.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Agent UUID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Project to create the agent in. Defaults to the provider's default_project_id. Changing forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Agent name (lowercase, 3-64 chars, alphanumeric + hyphens). Changing forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"agent_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Agent runtime: \"cursor\" (default) or \"claude\". Changing forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"model": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Model identifier passed to the agent runtime (e.g. \"claude-4.6-opus-thinking\").",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"system_prompt": schema.StringAttribute{
				Optional:    true,
				Description: "Custom system prompt prepended to every agent run.",
			},
			"skills": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Description: "Skill names to enable for this agent (e.g. [\"ac12dev\"]). \"ac12dev\" is always included.",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"effective_skills": schema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "Resolved skill list (skills with ac12dev prepended if missing).",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"identity_username": schema.StringAttribute{
				Computed:    true,
				Description: "Platform username of the agent's auto-created identity.",
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

func (r *AgentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *AgentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data agentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectID, ok := resolveProjectID(data.ProjectID.ValueString(), r.defaultProjectID)
	if !ok {
		resp.Diagnostics.AddError("project_id required", "Set project_id on the resource or default_project_id on the provider.")
		return
	}

	body := map[string]interface{}{
		"name": data.Name.ValueString(),
	}
	if !data.AgentType.IsNull() && !data.AgentType.IsUnknown() {
		body["agent_type"] = data.AgentType.ValueString()
	}
	if !data.Model.IsNull() && !data.Model.IsUnknown() {
		body["model"] = data.Model.ValueString()
	}
	if !data.SystemPrompt.IsNull() && !data.SystemPrompt.IsUnknown() {
		body["system_prompt"] = data.SystemPrompt.ValueString()
	}
	if !data.Skills.IsNull() && !data.Skills.IsUnknown() {
		var skills []string
		resp.Diagnostics.Append(data.Skills.ElementsAs(ctx, &skills, false)...)
		if !resp.Diagnostics.HasError() {
			body["skills"] = filterAc12devSkill(skills)
		}
	}

	respBody, status, err := r.client.Do("POST", fmt.Sprintf("/projects/%s/agents", projectID), body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 201 {
		resp.Diagnostics.AddError("Failed to create agent", client.APIError(status, respBody).Error())
		return
	}

	agent, err := client.DecodeJSON[client.Agent](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	data.ProjectID = types.StringValue(projectID)
	resp.Diagnostics.Append(setAgentState(ctx, &data, agent)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AgentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data agentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("GET",
		fmt.Sprintf("/projects/%s/agents/%s", data.ProjectID.ValueString(), data.Name.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to read agent", client.APIError(status, respBody).Error())
		return
	}

	agent, err := client.DecodeJSON[client.Agent](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	resp.Diagnostics.Append(setAgentState(ctx, &data, agent)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AgentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data agentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{}

	if !data.Model.IsNull() && !data.Model.IsUnknown() {
		body["model"] = data.Model.ValueString()
	}
	if !data.SystemPrompt.IsNull() && !data.SystemPrompt.IsUnknown() {
		body["system_prompt"] = data.SystemPrompt.ValueString()
	} else {
		body["system_prompt"] = nil
	}
	if !data.Skills.IsNull() && !data.Skills.IsUnknown() {
		var skills []string
		resp.Diagnostics.Append(data.Skills.ElementsAs(ctx, &skills, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		body["skills"] = filterAc12devSkill(skills)
	}

	respBody, status, err := r.client.Do("PUT",
		fmt.Sprintf("/projects/%s/agents/%s", data.ProjectID.ValueString(), data.Name.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to update agent", client.APIError(status, respBody).Error())
		return
	}

	agent, err := client.DecodeJSON[client.Agent](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	resp.Diagnostics.Append(setAgentState(ctx, &data, agent)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AgentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data agentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("DELETE",
		fmt.Sprintf("/projects/%s/agents/%s", data.ProjectID.ValueString(), data.Name.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 204 && status != 404 {
		resp.Diagnostics.AddError("Failed to delete agent", client.APIError(status, respBody).Error())
	}
}

// filterAc12devSkill strips "ac12dev" from a skills slice — the API always
// auto-includes it in effective_skills so it must never appear in stored skills.
func filterAc12devSkill(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "ac12dev" {
			out = append(out, s)
		}
	}
	return out
}

func setAgentState(ctx context.Context, data *agentResourceModel, agent *client.Agent) diag.Diagnostics {
	var diags diag.Diagnostics
	data.ID = types.StringValue(agent.ID)
	data.ProjectID = types.StringValue(agent.ProjectID)
	data.Name = types.StringValue(agent.Name)
	data.AgentType = types.StringValue(agent.AgentType)
	data.Model = types.StringValue(agent.Model)
	data.CreatedAt = types.StringValue(agent.CreatedAt)
	data.UpdatedAt = types.StringValue(agent.UpdatedAt)

	if agent.SystemPrompt != "" {
		data.SystemPrompt = types.StringValue(agent.SystemPrompt)
	} else {
		data.SystemPrompt = types.StringNull()
	}
	if agent.IdentityUsername != "" {
		data.IdentityUsername = types.StringValue(agent.IdentityUsername)
	} else {
		data.IdentityUsername = types.StringNull()
	}

	// The API silently strips "ac12dev" from stored skills since it is always
	// automatically prepended in effective_skills. Normalize here so the state
	// never drifts from a config that includes it.
	filteredSkills := make([]string, 0, len(agent.Skills))
	for _, s := range agent.Skills {
		if s != "ac12dev" {
			filteredSkills = append(filteredSkills, s)
		}
	}
	skills, d := types.ListValueFrom(ctx, types.StringType, filteredSkills)
	diags.Append(d...)
	if !diags.HasError() {
		data.Skills = skills
	}

	effectiveSkills, d := types.ListValueFrom(ctx, types.StringType, agent.EffectiveSkills)
	diags.Append(d...)
	if !diags.HasError() {
		data.EffectiveSkills = effectiveSkills
	}

	return diags
}
