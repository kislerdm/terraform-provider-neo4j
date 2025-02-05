// Copyright (c) HashiCorp, Inc.
// Copyright (c) Dmitry Kisler
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

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

var _ resource.Resource = &RelationshipResource{}
var _ resource.ResourceWithImportState = &RelationshipResource{}

func NewRelationshipResource() resource.Resource {
	return &RelationshipResource{}
}

// RelationshipResourceModel describes the resource data model.
type RelationshipResourceModel struct {
	Type        types.String `tfsdk:"type"`
	StartNodeID types.String `tfsdk:"start_node_id"`
	EndNodeID   types.String `tfsdk:"end_node_id"`
	Properties  types.Map    `tfsdk:"properties"`
	ID          types.String `tfsdk:"id"`
}

// RelationshipResource defines the `Node` resource implementation.
type RelationshipResource struct {
	client neo4j.SessionWithContext
}

const edgeSuffix = "_relationship"

func (e RelationshipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + edgeSuffix
}

func (e RelationshipResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Neo4j Relationship, details: " +
			"https://neo4j.com/docs/getting-started/appendix/graphdb-concepts/#graphdb-relationship",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Relationship unique identifier.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Relationship type, details: " +
					"https://neo4j.com/docs/getting-started/appendix/graphdb-concepts/#graphdb-relationship-type",
				Required: true,
			},
			"start_node_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the Node where the Relationship starts from.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"end_node_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the Node where the Relationship ends at.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"properties": schema.MapAttribute{
				MarkdownDescription: "Relationship properties, details: " +
					"https://neo4j.com/docs/getting-started/appendix/graphdb-concepts/#graphdb-properties",
				Optional:    true,
				ElementType: types.StringType,
			},
		},
	}
}

func (e *RelationshipResource) Configure(_ context.Context, req resource.ConfigureRequest,
	resp *resource.ConfigureResponse) {
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
	e.client = client
}

func (e RelationshipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data RelationshipResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Trace(ctx, "create a relationship")
	id := uuid.NewString()

	properties, diags := readProperties(ctx, data.Properties)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "faulty properties provided")
		return
	}
	if _, err := e.client.Run(ctx, `OPTIONAL MATCH (nStart{uuid:$uuidStart}), (nEnd{uuid:$uuidEnd})
MERGE (nStart)-[r:$($type)]->(nEnd)
SET r += $properties, r.uuid = $uuid
`, map[string]any{
		"uuid":       id,
		"uuidStart":  data.StartNodeID.ValueString(),
		"uuidEnd":    data.EndNodeID.ValueString(),
		"type":       data.Type.ValueString(),
		"properties": properties,
	}); err != nil {
		tflog.Debug(ctx, "failed to create the relationship")
		resp.Diagnostics.AddError("failed to create the relationship", err.Error())
		return
	}

	data.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	tflog.Trace(ctx, "created a relationship")
}

func (e RelationshipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data RelationshipResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	props := map[string]interface{}{"uuid": data.ID.ValueString()}
	tflog.Trace(ctx, "reading the relationship", props)

	id := data.ID.ValueString()
	if data.Properties.IsNull() || data.Properties.IsUnknown() {
		data.Properties = types.MapNull(types.StringType)
	}
	dbResp, err := e.client.Run(ctx, `MATCH ({uuid:$uuidStart})-[r{uuid:$uuid}]->({uuid:$uuidEnd}) RETURN r`,
		map[string]any{
			"uuid":      id,
			"uuidStart": data.StartNodeID.ValueString(),
			"uuidEnd":   data.EndNodeID.ValueString(),
			"type":      data.Type.ValueString(),
		})
	switch err != nil {
	case true:
		resp.Diagnostics.AddError("failed to read the relationship", err.Error())
	default:
		var rec *neo4j.Record
		if dbResp.NextRecord(ctx, &rec) {
			relationship := rec.Values[0].(neo4j.Relationship)

			var d diag.Diagnostics
			if len(relationship.GetProperties()) > 1 {
				var tmp = make(map[string]string, len(relationship.GetProperties())-1)
				for k, v := range relationship.GetProperties() {
					// Exclude the system property used to store the resource id.
					// It's used because the private Neo4j identifier (elementId) may not be reliable
					// beyond the scope of a single database transaction.
					if k != "uuid" {
						tmp[k] = fmt.Sprintf("%v", v)
					}
				}
				if !(data.Properties.IsNull() && len(tmp) == 0) {
					data.Properties, d = types.MapValueFrom(ctx, types.StringType, tmp)
					resp.Diagnostics.Append(d...)
				}
			}

			data.Type = types.StringValue(relationship.Type)

		} else {
			resp.Diagnostics.AddError("no relationship found", id)
		}
	}
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "failed to read the relationship", props)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	tflog.Trace(ctx, "read the relationship", props)
}

func (e RelationshipResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data RelationshipResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := data.ID.ValueString()
	tflog.Trace(ctx, "updating the relationship", map[string]interface{}{"id": id})

	properties, diags := readProperties(ctx, data.Properties)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "faulty properties provided")
		return
	}

	if _, err := e.client.Run(ctx, `OPTIONAL MATCH ({uuid:$uuidStart})-[r:$($type){uuid:$uuid}]-({uuid:$uuidEnd})
SET r = {}
SET r += $properties, r.uuid = $uuid
`, map[string]any{
		"uuid":       id,
		"uuidStart":  data.StartNodeID.ValueString(),
		"uuidEnd":    data.EndNodeID.ValueString(),
		"type":       data.Type.ValueString(),
		"properties": properties,
	}); err != nil {
		tflog.Debug(ctx, "failed to update the relationship")
		resp.Diagnostics.AddError("failed to update the relationship", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if !resp.Diagnostics.HasError() {
		tflog.Trace(ctx, "failed to update state")
		return
	}
	tflog.Trace(ctx, "updated the relationship", map[string]interface{}{"id": id})
}

func (e RelationshipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data RelationshipResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Trace(ctx, "delete the relationship")
	if _, err := e.client.Run(ctx,
		`OPTIONAL MATCH ({uuid:$uuidStart})-[r:$($type){uuid:$uuid}]-({uuid:$uuidEnd}) DELETE r`,
		map[string]any{
			"uuid":      data.ID.ValueString(),
			"uuidStart": data.StartNodeID.ValueString(),
			"uuidEnd":   data.EndNodeID.ValueString(),
			"type":      data.Type.ValueString(),
		},
	); err != nil {
		tflog.Debug(ctx, "failed to delete the relationship")
		resp.Diagnostics.AddError("failed to delete the relationship", err.Error())
		return
	}
	data.ID = types.StringNull()
	data.Type = types.StringNull()
	data.StartNodeID = types.StringNull()
	data.EndNodeID = types.StringNull()
	data.Properties = types.MapNull(basetypes.StringType{})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	tflog.Trace(ctx, "deleted the relationship")
}

func (e RelationshipResource) ImportState(ctx context.Context, req resource.ImportStateRequest,
	resp *resource.ImportStateResponse) {
	var data RelationshipResourceModel
	data.ID = basetypes.NewStringValue(req.ID)
	tflog.Trace(ctx, "importing the relationship", map[string]interface{}{"id": req.ID})

	if data.Properties.IsNull() || data.Properties.IsUnknown() {
		data.Properties = types.MapNull(types.StringType)
	}

	id := data.ID.ValueString()
	dbResp, err := e.client.Run(ctx, `MATCH (n)-[r{uuid:$uuid}]->(m) 
RETURN {start_node_id:n.uuid, end_node_id:n.uuid, r: r} AS resp`, map[string]any{"uuid": id})
	switch err != nil {
	case true:
		resp.Diagnostics.AddError("failed to read the relationship", err.Error())
	default:
		var rec *neo4j.Record
		if dbResp.NextRecord(ctx, &rec) {
			m := rec.AsMap()["resp"].(map[string]any)
			relationship := m["r"].(neo4j.Relationship)

			var d diag.Diagnostics
			if len(relationship.GetProperties()) > 1 {
				var tmp = make(map[string]string, len(relationship.GetProperties())-1)
				for k, v := range relationship.GetProperties() {
					// Exclude the system property used to store the resource id.
					// It's used because the private Neo4j identifier (elementId) may not be reliable
					// beyond the scope of a single database transaction.
					if k != "uuid" {
						tmp[k] = fmt.Sprintf("%v", v)
					}
				}
				if !(data.Properties.IsNull() && len(tmp) == 0) {
					data.Properties, d = types.MapValueFrom(ctx, types.StringType, tmp)
					resp.Diagnostics.Append(d...)
				}
			}

			data.Type = types.StringValue(relationship.Type)
			data.StartNodeID = types.StringValue(m["start_node_id"].(string))
			data.EndNodeID = types.StringValue(m["end_node_id"].(string))

		} else {
			resp.Diagnostics.AddError("no relationship found", id)
		}
	}
	if resp.Diagnostics.HasError() {
		tflog.Trace(ctx, "failed to import the relationship", map[string]interface{}{"id": req.ID})
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if !resp.Diagnostics.HasError() {
		tflog.Trace(ctx, "failed to import to state")
		return
	}
	tflog.Trace(ctx, "imported the relationship", map[string]interface{}{"id": req.ID})
}
