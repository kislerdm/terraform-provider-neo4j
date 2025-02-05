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
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
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

func readProperties(ctx context.Context, props types.Map) (o map[string]any, diags diag.Diagnostics) {
	if !props.IsNull() && !props.IsUnknown() {
		elements := make(map[string]types.String, len(props.Elements()))
		if _, ok := elements["uuid"]; ok {
			diags.AddError("reserved key is set as property", "uuid is reserved")
		}
		diags.Append(props.ElementsAs(ctx, &elements, false)...)
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
	} else {
		o = nil
	}
	if diags.HasError() {
		o = nil
	}
	return o, diags
}

const nodeSuffix = "_node"

func (r *NodeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + nodeSuffix
}

func (r *NodeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Neo4j Node, details: " +
			"https://neo4j.com/docs/getting-started/appendix/graphdb-concepts/#graphdb-node",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Node unique identifier.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"labels": schema.ListAttribute{
				MarkdownDescription: "Node labels, details: " +
					"https://neo4j.com/docs/getting-started/appendix/graphdb-concepts/#graphdb-labels",
				Optional:    true,
				ElementType: types.StringType,
			},
			"properties": schema.MapAttribute{
				MarkdownDescription: "Node properties, details: " +
					"https://neo4j.com/docs/getting-started/appendix/graphdb-concepts/#graphdb-properties",
				Optional:    true,
				ElementType: types.StringType,
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

	properties, diags := readProperties(ctx, data.Properties)
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
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	props := map[string]interface{}{"uuid": data.ID.ValueString()}
	tflog.Trace(ctx, "reading the node", props)
	resp.Diagnostics.Append(r.read(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "failed to reade the node", props)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	tflog.Trace(ctx, "read the node", props)
}

func (r *NodeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data NodeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := data.ID.ValueString()
	tflog.Trace(ctx, "updating the node", map[string]interface{}{"id": id})

	labels, diags := data.ReadLabels(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "faulty labels provided")
		return
	}

	properties, diags := readProperties(ctx, data.Properties)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "faulty properties provided")
		return
	}

	if _, err := r.client.Run(ctx, `MATCH (n{uuid:$uuid})
FOREACH (l in labels(n) | REMOVE n:$(l)) 
FOREACH (l in $labels | SET n:$(l))
SET n = {}
SET n += $properties, n.uuid = $uuid
`, map[string]any{"uuid": id, "labels": labels, "properties": properties}); err != nil {
		tflog.Debug(ctx, "failed to update the node")
		resp.Diagnostics.AddError("failed to update the node", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if !resp.Diagnostics.HasError() {
		tflog.Trace(ctx, "failed to update state")
		return
	}
	tflog.Trace(ctx, "updated the node", map[string]interface{}{"id": id})
}

func (r *NodeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data NodeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Trace(ctx, "delete the node")
	if _, err := r.client.Run(ctx,
		`MATCH (n{uuid:$uuid}) DETACH DELETE n`,
		map[string]any{"uuid": data.ID.ValueString()},
	); err != nil {
		tflog.Debug(ctx, "failed to delete the node")
		resp.Diagnostics.AddError("failed to delete the node", err.Error())
		return
	}
	data.ID = types.StringNull()
	data.Labels = types.ListNull(basetypes.StringType{})
	data.Properties = types.MapNull(basetypes.StringType{})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	tflog.Trace(ctx, "deleted the node")
}

func (r *NodeResource) ImportState(ctx context.Context, req resource.ImportStateRequest,
	resp *resource.ImportStateResponse) {
	var data NodeResourceModel
	data.ID = basetypes.NewStringValue(req.ID)
	tflog.Trace(ctx, "importing the node", map[string]interface{}{"id": req.ID})
	resp.Diagnostics.Append(r.read(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		tflog.Trace(ctx, "failed to import the node", map[string]interface{}{"id": req.ID})
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if !resp.Diagnostics.HasError() {
		tflog.Trace(ctx, "failed to import to state")
		return
	}
	tflog.Trace(ctx, "imported the node", map[string]interface{}{"id": req.ID})
}

func (r *NodeResource) read(ctx context.Context, data *NodeResourceModel) (diags diag.Diagnostics) {
	id := data.ID.ValueString()
	if data.Labels.IsNull() || data.Labels.IsUnknown() {
		data.Labels = types.ListNull(types.StringType)
	}
	if data.Properties.IsNull() || data.Properties.IsUnknown() {
		data.Properties = types.MapNull(types.StringType)
	}
	dbResp, err := r.client.Run(ctx, `MATCH (n{uuid:$uuid}) RETURN n`, map[string]any{"uuid": id})
	switch err != nil {
	case true:
		diags.AddError("failed to read the node", err.Error())
	default:
		var rec *neo4j.Record
		if dbResp.NextRecord(ctx, &rec) {
			node := rec.Values[0].(neo4j.Node)

			var d diag.Diagnostics
			if !(data.Labels.IsNull() && len(node.Labels) == 0) {
				data.Labels, d = types.ListValueFrom(ctx, types.StringType, node.Labels)
				diags.Append(d...)
			}

			if len(node.GetProperties()) > 1 {
				var tmp = make(map[string]string, len(node.GetProperties())-1)
				for k, v := range node.GetProperties() {
					// Exclude the system property used to store the resource id.
					// It's used because the private Neo4j identifier (elementId) may not be reliable
					// beyond the scope of a single database transaction.
					if k != "uuid" {
						tmp[k] = fmt.Sprintf("%v", v)
					}
				}
				if !(data.Properties.IsNull() && len(tmp) == 0) {
					data.Properties, d = types.MapValueFrom(ctx, types.StringType, tmp)
					diags.Append(d...)
				}
			}

		} else {
			diags.AddError("no node found", id)
		}
	}

	return diags
}
