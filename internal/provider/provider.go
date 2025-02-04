// Copyright (c) HashiCorp, Inc.
// Copyright (c) Dmitry Kisler
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"cmp"
	"context"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	Namespace = "kislerdm"
	Name      = "neo4j"
)

// Ensure Provider satisfies various provider interfaces.
var _ provider.Provider = &Provider{}
var _ provider.ProviderWithFunctions = &Provider{}
var _ provider.ProviderWithEphemeralResources = &Provider{}

// Provider defines the provider implementation.
type Provider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// ModelProvider describes the provider data model.
type ModelProvider struct {
	DatabaseURI      types.String `tfsdk:"db_uri"`
	DatabaseName     types.String `tfsdk:"db_name"`
	DatabaseUser     types.String `tfsdk:"db_user"`
	DatabasePassword types.String `tfsdk:"db_password"`
}

func (p *Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = Name
	resp.Version = p.version
}

func (p *Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `Terraform provider to manage Neo4j resources.

!>**Warning** The minimal supported version of Neo4j is 5.24.`,
		Attributes: map[string]schema.Attribute{
			"db_uri": schema.StringAttribute{
				MarkdownDescription: "Database access URI. " +
					"Alternatively, set the environment variable `DB_URI`.",
				Optional: true,
			},
			"db_user": schema.StringAttribute{
				MarkdownDescription: "The admin username to authenticated with the database. " +
					"Alternatively, set the environment variable `DB_USER`.",
				Optional: true,
			},
			"db_password": schema.StringAttribute{
				MarkdownDescription: "The user password to authenticated with the database. " +
					"Alternatively, set the environment variable `DB_PASSWORD`.",
				Optional: true,
			},
			"db_name": schema.StringAttribute{
				MarkdownDescription: "The database name. " +
					"Alternatively, set the environment variable `DB_NAME`.",
				Optional: true,
			},
		},
	}
}

func (p *Provider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data ModelProvider

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if data.DatabaseURI.ValueString() == "" {
		data.DatabaseURI = types.StringValue(os.Getenv("DB_URI"))
	}
	if data.DatabaseUser.ValueString() == "" {
		data.DatabaseUser = types.StringValue(os.Getenv("DB_USER"))
	}
	if data.DatabasePassword.ValueString() == "" {
		data.DatabasePassword = types.StringValue(os.Getenv("DB_PASSWORD"))
	}
	if data.DatabaseName.ValueString() == "" {
		data.DatabaseName = types.StringValue(cmp.Or(os.Getenv("DB_NAME"), "neo4j"))
	}

	client, err := NewClient(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("failed to connect to database", err.Error())
		return
	}
	resp.ResourceData = client
}

func NewClient(ctx context.Context, cfg ModelProvider) (sess neo4j.SessionWithContext, err error) {
	driver, err := neo4j.NewDriverWithContext(cfg.DatabaseURI.ValueString(),
		neo4j.BasicAuth(cfg.DatabaseUser.ValueString(), cfg.DatabasePassword.ValueString(), ""),
	)
	var isConnected bool
	if err == nil {
		if !isConnected {
			if err = tryConnection(ctx, driver, 3); err == nil {
				isConnected = true
			}
		}
	}
	if isConnected {
		sess = driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: cfg.DatabaseName.ValueString()})
	}
	return sess, err
}

func tryConnection(ctx context.Context, driver neo4j.DriverWithContext, maxAttempts uint8) error {
	const (
		delay = 1 * time.Second
	)
	var attempt uint8
	var err error
	for attempt < maxAttempts {
		if err = driver.VerifyConnectivity(ctx); err == nil {
			break
		}
		time.Sleep(delay)
		attempt++
	}
	return err
}

func (p *Provider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewNodeResource,
	}
}

func (p *Provider) EphemeralResources(_ context.Context) []func() ephemeral.EphemeralResource {
	return []func() ephemeral.EphemeralResource{}
}

func (p *Provider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func (p *Provider) Functions(_ context.Context) []func() function.Function {
	return []func() function.Function{}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &Provider{
			version: version,
		}
	}
}
