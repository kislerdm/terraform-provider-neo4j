// Copyright (c) HashiCorp, Inc.
// Copyright (c) Dmitry Kisler
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testContainerNeo4j "github.com/testcontainers/testcontainers-go/modules/neo4j"
)

// testAccProtoV6ProviderFactories is used to instantiate a provider during acceptance testing.
// The factory function is called for each Terraform CLI command to create a provider
// server that the CLI can connect to and interact with.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"neo4j": providerserver.NewProtocol6WithError(New("test")()),
}

var testDbURI, testDBUser, testDBPass string

func init() {
	testDBUser = "neo4j"
	ctx := context.Background()
	c, err := testContainerNeo4j.Run(ctx,
		"neo4j:5.26.0-community-ubi9",
		testContainerNeo4j.WithLabsPlugin(testContainerNeo4j.Apoc),
	)
	if err != nil {
		log.Fatalf("failed to start a neo4j container: %v\n", err)
	}

	testDbURI, err = c.BoltUrl(ctx)
	if err != nil {
		log.Printf("failed to retrieve testDbURI: %v\n", err)
		return
	}

	if _, err = NewClient(context.Background(), ModelProvider{
		DatabaseURI:      types.StringValue(testDbURI),
		DatabaseUser:     types.StringValue(testDBUser),
		DatabasePassword: types.StringValue(testDBPass),
	}); err != nil {
		log.Printf("could not connect to db: %v\n", err)
		_ = c.Terminate(context.Background())
	}
}
