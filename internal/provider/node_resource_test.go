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
)

func TestAccNodeResource(t *testing.T) {
	t.Setenv("DB_URI", testDbURI)
	t.Setenv("DB_USER", testDBUser)
	t.Cleanup(func() {
		t.Setenv("DB_URI", "")
		t.Setenv("DB_USER", "")
	})

	t.Run("labels and properties", func(t *testing.T) {
		wantProperties := map[string]any{
			"foo":  int64(100),
			"bar":  "qux",
			"quux": 1.2,
		}
		wantLabels := []string{"foo", "bar"}
		config := fmt.Sprintf(`resource "neo4j_node" "_" {
	labels 	   = %s
	properties = %s
}`, newTfListConfig(wantLabels), newTfMapConfig(wantProperties))
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: config,
					ConfigStateChecks: []statecheck.StateCheck{
						statecheck.ExpectKnownValue(
							"neo4j_node._",
							tfjsonpath.New("id"),
							knownvalue.StringRegexp(
								regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[0-9a-f]{4}-[0-9a-f]{12}$`),
							),
						),
						statecheck.ExpectKnownValue("neo4j_node._",
							tfjsonpath.New("labels"),
							knownvalue.ListExact(toListCheck(wantLabels)),
						),
						statecheck.ExpectKnownValue("neo4j_node._",
							tfjsonpath.New("properties"),
							knownvalue.MapExact(toMapCheck(wantProperties)),
						),
						checkNodeInDatabase{
							resourceAddress: "neo4j_node._",
							Labels:          wantLabels,
							Properties:      wantProperties,
						},
					},
				},
			},
		})
	})

	t.Run("labels without properties", func(t *testing.T) {
		wantProperties := map[string]any{}
		wantLabels := []string{"foo", "bar"}
		config := fmt.Sprintf(`resource "neo4j_node" "_" {
	labels = %s
}`, newTfListConfig(wantLabels))
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: config,
					ConfigStateChecks: []statecheck.StateCheck{
						statecheck.ExpectKnownValue(
							"neo4j_node._",
							tfjsonpath.New("id"),
							knownvalue.StringRegexp(
								regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[0-9a-f]{4}-[0-9a-f]{12}$`),
							),
						),
						statecheck.ExpectKnownValue("neo4j_node._",
							tfjsonpath.New("labels"),
							knownvalue.ListExact(toListCheck(wantLabels)),
						),
						statecheck.ExpectKnownValue("neo4j_node._",
							tfjsonpath.New("properties"),
							knownvalue.Null(),
						),
						checkNodeInDatabase{
							resourceAddress: "neo4j_node._",
							Labels:          wantLabels,
							Properties:      wantProperties,
						},
					},
				},
			},
		})
	})

	t.Run("properties without labels", func(t *testing.T) {
		wantProperties := map[string]any{
			"foo":  int64(100),
			"bar":  "qux",
			"quux": 1.2,
		}
		config := fmt.Sprintf(`resource "neo4j_node" "_" {
	properties = %s
}`, newTfMapConfig(wantProperties))
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: config,
					ConfigStateChecks: []statecheck.StateCheck{
						statecheck.ExpectKnownValue(
							"neo4j_node._",
							tfjsonpath.New("id"),
							knownvalue.StringRegexp(
								regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[0-9a-f]{4}-[0-9a-f]{12}$`),
							),
						),
						statecheck.ExpectKnownValue("neo4j_node._",
							tfjsonpath.New("labels"),
							knownvalue.Null(),
						),
						statecheck.ExpectKnownValue("neo4j_node._",
							tfjsonpath.New("properties"),
							knownvalue.MapExact(toMapCheck(wantProperties)),
						),
						checkNodeInDatabase{
							resourceAddress: "neo4j_node._",
							Properties:      wantProperties,
						},
					},
				},
			},
		})
	})
}

var _ statecheck.StateCheck = checkNodeInDatabase{}

type checkNodeInDatabase struct {
	resourceAddress string
	Labels          []string
	Properties      map[string]any
}

func (v checkNodeInDatabase) CheckState(ctx context.Context, req statecheck.CheckStateRequest,
	resp *statecheck.CheckStateResponse) {
	if req.State.Values == nil {
		resp.Error = fmt.Errorf("state does not contain any state values")
		return
	}
	if req.State.Values.RootModule == nil {
		resp.Error = fmt.Errorf("state does not contain a root module")
		return
	}

	c, err := NewClient(ctx, ModelProvider{
		DatabaseURI:      types.StringValue(testDbURI),
		DatabaseUser:     types.StringValue(testDBUser),
		DatabasePassword: types.StringValue(testDBPass),
	})
	if err != nil {
		resp.Error = err
		return
	}
	defer func() { _ = c.Close(ctx) }()

	var res *tfjson.StateResource
	for _, r := range req.State.Values.RootModule.Resources {
		if v.resourceAddress == r.Address {
			res = r
		}
	}
	if res == nil {
		resp.Error = fmt.Errorf("id attribute not found")
		return
	}
	id, err := tfjsonpath.Traverse(res.AttributeValues, tfjsonpath.New("id"))
	if err != nil {
		resp.Error = err
		return
	}

	r, err := c.Run(ctx, `MATCH (n{uuid:$uuid}) RETURN n`, map[string]any{"uuid": id.(string)})
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
		slices.Sort(v.Labels)
		if !slices.Equal(gotLabels, v.Labels) {
			er = fmt.Errorf("lables don't match, want = %v, got = %v", v.Labels, gotLabels)
		}

		gotProperties := node.GetProperties()
		// remove system attribute
		delete(gotProperties, "uuid")
		if !maps.Equal(v.Properties, gotProperties) {
			er = errors.Join(er,
				fmt.Errorf("properties don't match, want = %v, got = %v", v.Properties, gotProperties))
		}

		resp.Error = er

	} else {
		resp.Error = fmt.Errorf("no node with id %v found", id)
	}
}

func toMapCheck(m map[string]any) map[string]knownvalue.Check {
	var o = make(map[string]knownvalue.Check, len(m))
	for k, v := range m {
		o[k] = knownvalue.StringExact(fmt.Sprintf("%v", v))
	}
	return o
}

func toListCheck(v []string) []knownvalue.Check {
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

	o.WriteString("\n}")

	return o.String()
}
