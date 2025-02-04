// Copyright (c) HashiCorp, Inc.
// Copyright (c) Dmitry Kisler
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &NodeResource{}
var _ resource.ResourceWithImportState = &NodeResource{}

func NewNodeResource() resource.Resource {
	return &NodeResource{}
}

// NodeResource defines the `Node` resource implementation.
type NodeResource struct {
	client neo4j.SessionWithContext
}

// NodeResourceModel describes the resource data model.
type NodeResourceModel struct {
	Labels     types.List   `tfsdk:"labels"`
	Properties types.Map    `tfsdk:"properties"`
	ID         types.String `tfsdk:"id"`
}

func (n NodeResourceModel) ReadLabels(ctx context.Context) (o []string, diags diag.Diagnostics) {
	if !n.Labels.IsNull() && !n.Labels.IsUnknown() {
		elements := make([]types.String, 0, len(n.Labels.Elements()))
		diags = n.Labels.ElementsAs(ctx, &elements, false)
		if !diags.HasError() {
			o = make([]string, len(elements))
			for i, v := range elements {
				if v.IsUnknown() {
					diags.AddError("element is unknown", fmt.Sprintf("label %d", i))
					continue
				}
				if v.IsNull() {
					diags.AddError("element is null", fmt.Sprintf("label %d", i))
					continue
				}
				o[i] = v.ValueString()
			}
		}
	}
	return o, diags
}

func (n NodeResourceModel) ReadProperties(ctx context.Context) (o map[string]any, diags diag.Diagnostics) {
	if !n.Properties.IsNull() && !n.Properties.IsUnknown() {
		elements := make(map[string]types.String, len(n.Properties.Elements()))
		if _, ok := elements["uuid"]; ok {
			diags.AddError("reserved key is set as property", "uuid is reserved")
		}
		diags.Append(n.Properties.ElementsAs(ctx, &elements, false)...)

		if !diags.HasError() {
			o = make(map[string]any, len(elements))

			for k, v := range elements {
				if v.IsNull() {
					diags.AddError("key is null", k)
				}
				if v.IsUnknown() {
					diags.AddError("key is unknown", k)
				}

				s := v.ValueString()
				if vv, err := strconv.ParseInt(s, 10, 64); err == nil {
					o[k] = vv
				} else if vv, err := strconv.ParseFloat(s, 64); err == nil {
					o[k] = vv
				} else {
					o[k] = s
				}
			}
		}
	}
	if diags.HasError() {
		o = nil
	}
	return o, diags
}

func (r *NodeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_node"
}

func (r *NodeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Neo4j Node.",
		Attributes: map[string]schema.Attribute{
			"labels": schema.ListAttribute{
				MarkdownDescription: "Node labels, details: https://neo4j.com/docs/getting-started/appendix/graphdb-concepts/#graphdb-labels",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Node unique identifier.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"properties": schema.MapAttribute{
				MarkdownDescription: "Node properties, details: https://neo4j.com/docs/getting-started/appendix/graphdb-concepts/#graphdb-properties",
				Optional:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

func (r *NodeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(neo4j.SessionWithContext)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected neo4j.DriverWithContext, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *NodeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data NodeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Trace(ctx, "create a node")
	id := uuid.NewString()

	labels, diags := data.ReadLabels(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "faulty labels provided")
		return
	}

	properties, diags := data.ReadProperties(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "faulty properties provided")
		return
	}

	if _, err := r.client.Run(ctx, `MERGE (n{uuid:$uuid})
FOREACH (l in $labels | SET n:$(l))
SET n += $properties
`, map[string]any{"uuid": id, "labels": labels, "properties": properties}); err != nil {
		tflog.Debug(ctx, "failed to create the node")
		resp.Diagnostics.AddError("failed to create the node", err.Error())
		return
	}

	data.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	tflog.Trace(ctx, "created a node")
}

func (r *NodeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data NodeResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// If applicable, this is a great opportunity to initialize any necessary
	// provider client data and make a call using it.
	// httpResp, err := r.client.Do(httpReq)
	// if err != nil {
	//     resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read example, got error: %s", err))
	//     return
	// }

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *NodeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data NodeResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// If applicable, this is a great opportunity to initialize any necessary
	// provider client data and make a call using it.
	// httpResp, err := r.client.Do(httpReq)
	// if err != nil {
	//     resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update example, got error: %s", err))
	//     return
	// }

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *NodeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data NodeResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// If applicable, this is a great opportunity to initialize any necessary
	// provider client data and make a call using it.
	// httpResp, err := r.client.Do(httpReq)
	// if err != nil {
	//     resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete example, got error: %s", err))
	//     return
	// }
}

func (r *NodeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
