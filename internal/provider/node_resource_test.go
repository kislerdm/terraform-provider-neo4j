// Copyright (c) HashiCorp, Inc.
// Copyright (c) Dmitry Kisler
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
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

func TestAccNodeResource(t *testing.T) {
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

	configInit := config{
		resourceName:      "neo4j_node",
		resourceTfVarName: "_",
		WantLabels:        []string{"foo", "bar"},
		WantProperties: map[string]any{
			"foo":  int64(100),
			"bar":  "qux",
			"quux": 1.2,
		},
		Got:    map[string]any{"id": nil},
		client: c,
	}
	resourceAddress := configInit.resourceAddress()

	configUpdate := configInit
	configUpdate.Got = nil
	configUpdate.WantLabels = nil

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// create initial state with labels and properties
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
		},
	})

	//	check deleted state
	_, err = c.Run(context.Background(),
		`OPTIONAL MATCH (n{uuid:$uuid}) CALL apoc.util.validate(NOT n IS NULL, "node shall be deleted", [])`,
		map[string]any{"uuid": configInit.Got["id"].(string)})
	assert.NoError(t, err)
}

var _ statecheck.StateCheck = config{}

type config struct {
	client            neo4j.SessionWithContext
	resourceName      string
	resourceTfVarName string
	WantLabels        []string
	WantProperties    map[string]any
	Got               map[string]any
}

func (cfg config) generateConfig() string {
	var o string
	o += fmt.Sprintf("resource \"%s\" \"%s\" {\n", cfg.resourceName, cfg.resourceTfVarName)
	if len(cfg.WantLabels) > 0 {
		o += fmt.Sprintf("labels = %s\n", newTfListConfig(cfg.WantLabels))
	}
	if len(cfg.WantProperties) > 0 {
		o += fmt.Sprintf("properties = %s\n", newTfMapConfig(cfg.WantProperties))
	}
	o += "}"
	return o
}

func (cfg config) resourceAddress() string {
	return cfg.resourceName + "." + cfg.resourceTfVarName
}

func (cfg config) CheckState(ctx context.Context, req statecheck.CheckStateRequest,
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

	r, err := cfg.client.Run(ctx, `MATCH (n{uuid:$uuid}) RETURN n`, map[string]any{"uuid": id.(string)})
	if err != nil {
		resp.Error = err
		return
	}

	var rec *neo4j.Record
	if r.NextRecord(ctx, &rec) {
		var er error
		node := rec.Values[0].(neo4j.Node)

		gotLabels := node.Labels
		slices.Sort(gotLabels)

		wantLabels := slices.Clone(cfg.WantLabels)
		slices.Sort(wantLabels)
		if !slices.Equal(gotLabels, wantLabels) {
			er = fmt.Errorf("lables don't match, want = %v, got = %v", wantLabels, gotLabels)
		}

		gotProperties := node.GetProperties()
		// remove system attribute
		delete(gotProperties, "uuid")
		if !maps.Equal(cfg.WantProperties, gotProperties) {
			er = errors.Join(er,
				fmt.Errorf("properties don't match, want = %v, got = %v", cfg.WantProperties, gotProperties))
		}

		resp.Error = er
	} else {
		resp.Error = fmt.Errorf("no node with id %v found", id)
	}

	if resp.Error != nil {
		return
	}

	gotLabels, err := tfjsonpath.Traverse(res.AttributeValues, tfjsonpath.New("labels"))
	if err != nil {
		resp.Error = err
		return
	}
	wantLabels := knownvalue.ListExact(toListCheckExact(cfg.WantLabels))
	if err := wantLabels.CheckValue(gotLabels); err != nil {
		resp.Error = fmt.Errorf("lables don't match, want = %v, got = %v: %w", cfg.WantLabels, gotLabels, err)
		return
	}

	gotProperties, err := tfjsonpath.Traverse(res.AttributeValues, tfjsonpath.New("properties"))
	if err != nil {
		resp.Error = err
		return
	}
	wantProperties := knownvalue.MapExact(toMapCheckExact(cfg.WantProperties))
	if err := wantProperties.CheckValue(gotProperties); err != nil {
		resp.Error = fmt.Errorf("properties don't match, want = %v, got = %v: %w", cfg.WantProperties,
			gotProperties, err)
		return
	}

	for k := range cfg.Got {
		cfg.Got[k], _ = tfjsonpath.Traverse(res.AttributeValues, tfjsonpath.New(k))
	}
}

func toMapCheckExact(m map[string]any) map[string]knownvalue.Check {
	var o = make(map[string]knownvalue.Check, len(m))
	for k, v := range m {
		o[k] = knownvalue.StringExact(fmt.Sprintf("%v", v))
	}
	return o
}

func toListCheckExact(v []string) []knownvalue.Check {
	var o = make([]knownvalue.Check, len(v))
	for i, vv := range v {
		o[i] = knownvalue.StringExact(vv)
	}
	return o
}

func newTfListConfig(v []string) string {
	var o bytes.Buffer
	o.WriteString("[")
	for i, s := range v {
		o.WriteString("\"")
		o.WriteString(s)
		o.WriteString("\"")
		if i < len(v)-1 {
			o.WriteString(", ")
		}
	}
	o.WriteString("]")
	return o.String()
}

func newTfMapConfig(m map[string]any) string {
	var o bytes.Buffer
	o.WriteString("{\n")

	for k, v := range m {
		o.WriteString("\"")
		o.WriteString(k)
		o.WriteString("\"")
		o.WriteString(" = ")

		switch v := v.(type) {
		case string:
			o.WriteString("\"")
			o.WriteString(v)
			o.WriteString("\"")
		default:
			o.WriteString(fmt.Sprintf("%v", v))
		}

		o.WriteString("\n")
	}

	o.WriteString("}")

	return o.String()
}
