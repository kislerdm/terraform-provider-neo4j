// Copyright (c) HashiCorp, Inc.
// Copyright (c) Dmitry Kisler
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/assert"
)

func TestAccRelationshipResource(t *testing.T) {
	t.Setenv("DB_URI", testDbURI)
	t.Setenv("DB_USER", testDBUser)
	t.Cleanup(func() {
		t.Setenv("DB_URI", "")
		t.Setenv("DB_USER", "")
	})

	ctx := context.Background()
	c, err := NewClient(ctx, ModelProvider{
		DatabaseURI:      types.StringValue(testDbURI),
		DatabaseUser:     types.StringValue(testDBUser),
		DatabasePassword: types.StringValue(testDBPass),
	})
	if err != nil {
		t.Errorf("could not conenct to database: %v\n", err)
		return
	}
	defer func() { _ = c.Close(ctx) }()

	t.Run("start&end identical, properties->plain->properties", func(
		t *testing.T) {
		configInit := configRelationship{
			client:            c,
			resourceTfVarName: "_",
			WantType:          "foo",
			WantProperties: map[string]any{
				"foo":  int64(100),
				"bar":  "qux",
				"quux": 1.2,
			},
			Got: map[string]any{"id": nil},
		}
		resourceAddress := configInit.resourceAddress()

		configPlain := configInit
		configPlain.Got = nil
		configPlain.WantProperties = nil

		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				// Create and Read testing
				{
					Config: configInit.generateConfig(),
					ConfigStateChecks: []statecheck.StateCheck{
						statecheck.ExpectKnownValue(
							resourceAddress,
							tfjsonpath.New("id"),
							knownvalue.StringRegexp(
								regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[0-9a-f]{4}-[0-9a-f]{12}$`),
							),
						),
						configInit,
					},
				},
				// ImportState testing
				{
					ResourceName:      configInit.resourceAddress(),
					ImportState:       true,
					ImportStateVerify: true,
				},
				// Update and Read testing
				{
					Config:            configPlain.generateConfig(),
					ConfigStateChecks: []statecheck.StateCheck{configPlain},
				},
				// ImportState testing
				{
					ResourceName:      configPlain.resourceAddress(),
					ImportState:       true,
					ImportStateVerify: true,
				},
			},
		})

		// Delete testing against database
		_, err = c.Run(context.Background(),
			`OPTIONAL MATCH ()-[r{uuid:$uuid}]-() 
CALL apoc.util.validate(NOT r IS NULL, "relationship shall be deleted", [])`,
			map[string]any{"uuid": configInit.Got["id"].(string)})
		assert.NoError(t, err)
	})

	t.Run("properties = {} vs properties = null", func(t *testing.T) {
		cfg := configRelationship{
			client:            c,
			resourceTfVarName: "_",
			WantType:          "foo",
			WantProperties:    map[string]any{},
		}
		cfgNull := cfg
		cfgNull.WantProperties = nil
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config:            cfg.generateConfig(),
					ConfigStateChecks: []statecheck.StateCheck{cfg},
				},
				{
					Config:            cfgNull.generateConfig(),
					ConfigStateChecks: []statecheck.StateCheck{cfgNull},
				},
				{
					ResourceName:      cfgNull.resourceAddress(),
					ImportState:       true,
					ImportStateVerify: true,
				},
				{
					Config:            cfg.generateConfig(),
					ConfigStateChecks: []statecheck.StateCheck{cfg},
				},
			},
		})
	})
}

type configRelationship struct {
	client            neo4j.SessionWithContext
	startNodeID       string
	endNodeID         string
	resourceTfVarName string
	WantType          string
	WantProperties    map[string]any
	Got               map[string]any
}

const resourceRelationshipName = Name + edgeSuffix

func (cfg configRelationship) generateConfig() string {
	if cfg.WantType == "" {
		panic("type must be set")
	}

	var o string
	o += fmt.Sprintf(`resource "%s" "%s" {
type = "%s"
`, resourceRelationshipName, cfg.resourceTfVarName, cfg.WantType)

	startNodeID := "\"" + cfg.startNodeID + "\""
	endNodeID := "\"" + cfg.endNodeID + "\""
	if cfg.startNodeID == "" || cfg.endNodeID == "" {
		nodeCfg := configNode{
			resourceTfVarName: "_",
		}
		o = fmt.Sprintf("%s\n%s", nodeCfg.generateConfig(), o)
		if startNodeID == "" {
			startNodeID = nodeCfg.resourceAddress() + ".id"
		}
		if cfg.startNodeID == "" {
			startNodeID = nodeCfg.resourceAddress() + ".id"
		}
		if cfg.endNodeID == "" {
			endNodeID = nodeCfg.resourceAddress() + ".id"
		}
	}

	o += fmt.Sprintf("start_node_id = %s\nend_node_id = %s\n", startNodeID, endNodeID)

	switch {
	case cfg.WantProperties == nil:
	case len(cfg.WantProperties) > 0:
		o += fmt.Sprintf("properties = %s\n", newTfMapConfig(cfg.WantProperties))
	default:
		o += "properties = {}\n"
	}
	o += "}"
	return o
}

func (cfg configRelationship) resourceAddress() string {
	return resourceRelationshipName + "." + cfg.resourceTfVarName
}

func (cfg configRelationship) CheckState(ctx context.Context, req statecheck.CheckStateRequest,
	resp *statecheck.CheckStateResponse) {
	if req.State == nil {
		resp.Error = fmt.Errorf("state is nil")
		return
	}
	if req.State.Values == nil {
		resp.Error = fmt.Errorf("state does not contain any state values")
		return
	}
	if req.State.Values.RootModule == nil {
		resp.Error = fmt.Errorf("state does not contain a root module")
		return
	}

	var res *tfjson.StateResource
	for _, r := range req.State.Values.RootModule.Resources {
		if cfg.resourceAddress() == r.Address {
			res = r
			break
		}
	}
	if res == nil {
		resp.Error = fmt.Errorf("%s - Resource not found in state", cfg.resourceAddress())
		return
	}

	id, err := tfjsonpath.Traverse(res.AttributeValues, tfjsonpath.New("id"))
	if err != nil {
		resp.Error = err
		return
	}

	r, err := cfg.client.Run(ctx, `MATCH ()-[r{uuid:$uuid}]-() RETURN r`, map[string]any{"uuid": id.(string)})
	if err != nil {
		resp.Error = err
		return
	}

	var rec *neo4j.Record
	var wantType string
	if r.NextRecord(ctx, &rec) {
		var er error
		rel := rec.Values[0].(neo4j.Relationship)

		gotProperties := rel.GetProperties()
		// remove system attribute
		delete(gotProperties, "uuid")
		if !maps.Equal(cfg.WantProperties, gotProperties) {
			er = errors.Join(er,
				fmt.Errorf("properties don't match, want = %v, got = %v", cfg.WantProperties, gotProperties))
		}

		wantType = rel.Type

		resp.Error = er
	} else {
		resp.Error = fmt.Errorf("no node with id %v found", id)
	}

	if resp.Error != nil {
		return
	}

	gotType, err := tfjsonpath.Traverse(res.AttributeValues, tfjsonpath.New("type"))
	if err != nil {
		resp.Error = err
		return
	}

	if wantType != gotType {
		resp.Error = fmt.Errorf("the type doesn't match, want = %s, got = %s", wantType, gotType)
		return
	}

	gotProperties, err := tfjsonpath.Traverse(res.AttributeValues, tfjsonpath.New("properties"))
	if err != nil {
		resp.Error = err
		return
	}

	var wantProperties knownvalue.Check
	switch cfg.WantProperties == nil {
	case true:
		wantProperties = knownvalue.Null()
	default:
		wantProperties = knownvalue.MapExact(toMapCheckExact(cfg.WantProperties))
	}
	if err := wantProperties.CheckValue(gotProperties); err != nil {
		resp.Error = fmt.Errorf("properties don't match, want = %v, got = %v: %w", cfg.WantProperties,
			gotProperties, err)
		return
	}

	for k := range cfg.Got {
		cfg.Got[k], _ = tfjsonpath.Traverse(res.AttributeValues, tfjsonpath.New(k))
	}
}
