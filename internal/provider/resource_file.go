package provider

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"

	"github.com/AndrewCopeland/terraform-provider-ac12dev/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &FileResource{}

type FileResource struct {
	client           *client.Client
	defaultProjectID string
}

type fileResourceModel struct {
	ID          types.String `tfsdk:"id"`
	ProjectID   types.String `tfsdk:"project_id"`
	Path        types.String `tfsdk:"path"`
	Content     types.String `tfsdk:"content"`
	Source      types.String `tfsdk:"source"`
	SourceHash  types.String `tfsdk:"source_hash"`
	IsPublic    types.Bool   `tfsdk:"is_public"`
	ContentType types.String `tfsdk:"content_type"`
	SizeBytes   types.Int64  `tfsdk:"size_bytes"`
	URL         types.String `tfsdk:"url"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func newFileResource() resource.Resource {
	return &FileResource{}
}

func (r *FileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_file"
}

func (r *FileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Uploads and manages a file in an ac12.dev project. Use `content` for inline text or `source` + `source_hash` for local files.",
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
			"path": schema.StringAttribute{
				Required:    true,
				Description: "Remote file path (e.g. \"index.html\" or \"assets/logo.png\"). Changing forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"content": schema.StringAttribute{
				Optional:    true,
				Description: "Inline text content. Mutually exclusive with source.",
			},
			"source": schema.StringAttribute{
				Optional:    true,
				Description: "Local file path to upload. Mutually exclusive with content. Pair with source_hash to detect changes.",
			},
			"source_hash": schema.StringAttribute{
				Optional:    true,
				Description: "Hash of the source file used to detect changes (e.g. filemd5(\"./dist/app.js\")).",
			},
			"is_public": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the file is publicly accessible via /f/{project_id}/{path} (default false).",
				Default:     booldefault.StaticBool(false),
			},
			"content_type": schema.StringAttribute{
				Computed:    true,
				Description: "Detected MIME type.",
			},
			"size_bytes": schema.Int64Attribute{
				Computed:    true,
				Description: "File size in bytes.",
			},
			"url": schema.StringAttribute{
				Computed:    true,
				Description: "Public URL (only meaningful when is_public = true).",
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

func (r *FileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *FileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data fileResourceModel
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

	f, err := r.uploadFile(pid, &data)
	if err != nil {
		resp.Diagnostics.AddError("Failed to upload file", err.Error())
		return
	}

	setFileState(&data, f)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data fileResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("GET",
		fmt.Sprintf("/projects/%s/files/%s?view=true", data.ProjectID.ValueString(), data.Path.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if status != 200 {
		resp.Diagnostics.AddError("Failed to read file", client.APIError(status, respBody).Error())
		return
	}

	// The view=true response is a FileContentResponse — use the metadata portion
	type fileContentResp struct {
		Path        string `json:"path"`
		ContentType string `json:"content_type"`
		SizeBytes   int64  `json:"size_bytes"`
		IsPublic    bool   `json:"is_public"`
	}
	fc, err := client.DecodeJSON[fileContentResp](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response", err.Error())
		return
	}

	data.ContentType = types.StringValue(fc.ContentType)
	data.SizeBytes = types.Int64Value(fc.SizeBytes)
	data.IsPublic = types.BoolValue(fc.IsPublic)
	// content/source/source_hash stay as-is from state (API doesn't return binary content for non-text)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data fileResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pid := data.ProjectID.ValueString()

	f, err := r.uploadFile(pid, &data)
	if err != nil {
		resp.Diagnostics.AddError("Failed to upload file", err.Error())
		return
	}

	setFileState(&data, f)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data fileResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("DELETE",
		fmt.Sprintf("/projects/%s/files/%s", data.ProjectID.ValueString(), data.Path.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("API request failed", err.Error())
		return
	}
	if status != 204 && status != 404 {
		resp.Diagnostics.AddError("Failed to delete file", client.APIError(status, respBody).Error())
	}
}

// uploadFile handles both content (inline text via PUT) and source (binary via POST).
func (r *FileResource) uploadFile(pid string, data *fileResourceModel) (*client.File, error) {
	remotePath := data.Path.ValueString()
	isPublic := data.IsPublic.ValueBool()

	if !data.Content.IsNull() && !data.Content.IsUnknown() {
		// Inline text content — use the write_file PUT endpoint
		body := map[string]interface{}{
			"content": data.Content.ValueString(),
		}
		respBody, status, err := r.client.Do("PUT",
			fmt.Sprintf("/projects/%s/files/%s", pid, remotePath), body)
		if err != nil {
			return nil, err
		}
		if status != 200 && status != 201 {
			return nil, client.APIError(status, respBody)
		}

		// The PUT/write_file endpoint doesn't accept is_public — PATCH separately
		// and return the PATCH response as the authoritative final state.
		if isPublic {
			patchBody := map[string]interface{}{"is_public": true}
			pRespBody, pStatus, pErr := r.client.Do("PATCH",
				fmt.Sprintf("/projects/%s/files/%s", pid, remotePath), patchBody)
			if pErr != nil {
				return nil, pErr
			}
			if pStatus != 200 {
				return nil, client.APIError(pStatus, pRespBody)
			}
			return client.DecodeJSON[client.File](pRespBody)
		}

		return client.DecodeJSON[client.File](respBody)
	}

	if !data.Source.IsNull() && !data.Source.IsUnknown() {
		// Local file upload — use raw POST endpoint
		localPath := data.Source.ValueString()
		fileData, err := os.ReadFile(localPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read source file %q: %w", localPath, err)
		}

		ct := mime.TypeByExtension(filepath.Ext(localPath))
		if ct == "" {
			ct = "application/octet-stream"
		}

		params := map[string]string{}
		if isPublic {
			params["is_public"] = "true"
		}

		respBody, status, err := r.client.Upload(
			fmt.Sprintf("/projects/%s/files/%s", pid, remotePath),
			fileData, ct, params,
		)
		if err != nil {
			return nil, err
		}
		if status != 200 && status != 201 {
			return nil, client.APIError(status, respBody)
		}
		return client.DecodeJSON[client.File](respBody)
	}

	return nil, fmt.Errorf("either content or source must be set")
}

func setFileState(data *fileResourceModel, f *client.File) {
	data.ID = types.StringValue(f.ID)
	data.Path = types.StringValue(f.Path)
	data.ContentType = types.StringValue(f.ContentType)
	data.SizeBytes = types.Int64Value(f.SizeBytes)
	data.IsPublic = types.BoolValue(f.IsPublic)
	data.URL = types.StringValue(f.URL)
	data.CreatedAt = types.StringValue(f.CreatedAt)
	data.UpdatedAt = types.StringValue(f.UpdatedAt)
}
