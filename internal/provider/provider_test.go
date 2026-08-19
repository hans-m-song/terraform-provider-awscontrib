package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hans-m-song/terraform-provider-awscontrib/internal/conns"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestProviderMetadata(t *testing.T) {
	p := New("test")()
	response := &provider.MetadataResponse{}
	p.Metadata(context.Background(), provider.MetadataRequest{}, response)

	if response.TypeName != "awscontrib" {
		t.Fatalf("expected provider type awscontrib, got %q", response.TypeName)
	}
	if response.Version != "test" {
		t.Fatalf("expected provider version test, got %q", response.Version)
	}
}

func TestProviderRegistersConnectResources(t *testing.T) {
	p := New("test")()
	constructors := p.Resources(context.Background())
	if len(constructors) != 4 {
		t.Fatalf("expected four resource constructors, got %d", len(constructors))
	}

	expectedTypeNames := []string{
		"awscontrib_connect_queue_quick_connect_associations",
		"awscontrib_connect_hours_of_operation_override",
		"awscontrib_connect_data_table",
		"awscontrib_connect_data_table_record",
	}
	for index, constructor := range constructors {
		if constructor == nil {
			t.Fatalf("resource constructor %d is nil", index)
		}

		first := constructor()
		second := constructor()
		if first == nil || second == nil {
			t.Fatalf("resource constructor %d returned nil", index)
		}
		if first == second {
			t.Fatalf("resource constructor %d reused its instance", index)
		}

		response := &resource.MetadataResponse{}
		first.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "awscontrib"}, response)
		if response.TypeName != expectedTypeNames[index] {
			t.Fatalf("unexpected resource type %q at index %d", response.TypeName, index)
		}
	}
}

func TestProviderRegistersConnectDataSources(t *testing.T) {
	p := New("test")()
	constructors := p.DataSources(context.Background())
	if len(constructors) != 2 {
		t.Fatalf("expected two data source constructors, got %d", len(constructors))
	}

	expectedTypeNames := []string{
		"awscontrib_connect_phone_number",
		"awscontrib_connect_contact_flow_module",
	}
	for index, constructor := range constructors {
		if constructor == nil {
			t.Fatalf("data source constructor %d is nil", index)
		}

		first := constructor()
		second := constructor()
		if first == nil || second == nil {
			t.Fatalf("data source constructor %d returned nil", index)
		}
		if first == second {
			t.Fatalf("data source constructor %d reused its instance", index)
		}

		response := &datasource.MetadataResponse{}
		first.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "awscontrib"}, response)
		if response.TypeName != expectedTypeNames[index] {
			t.Fatalf("unexpected data source type %q at index %d", response.TypeName, index)
		}
	}
}

func TestProviderConfigureMapsAWSConfigurationAndPropagatesClient(t *testing.T) {
	expectedClient := &conns.Client{}
	var configured conns.Config

	p := &AWSContribProvider{
		loadClient: func(_ context.Context, config conns.Config) (*conns.Client, error) {
			configured = config
			return expectedClient, nil
		},
	}
	response := &provider.ConfigureResponse{}

	p.Configure(context.Background(), provider.ConfigureRequest{
		Config: providerConfig(t, "engineering", "ap-southeast-2"),
	}, response)

	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected configuration diagnostics: %v", response.Diagnostics)
	}
	if configured.Profile != "engineering" {
		t.Fatalf("expected profile engineering, got %q", configured.Profile)
	}
	if configured.Region != "ap-southeast-2" {
		t.Fatalf("expected region ap-southeast-2, got %q", configured.Region)
	}
	if response.ResourceData != expectedClient {
		t.Fatalf("expected ResourceData to contain loaded client, got %T", response.ResourceData)
	}
	if response.DataSourceData != expectedClient {
		t.Fatalf("expected DataSourceData to contain loaded client, got %T", response.DataSourceData)
	}

	for index, constructor := range New("test")().DataSources(context.Background()) {
		dataSource, ok := constructor().(datasource.DataSourceWithConfigure)
		if !ok {
			t.Fatalf("registered data source %d does not support configuration", index)
		}

		dataSourceResponse := &datasource.ConfigureResponse{}
		dataSource.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: response.DataSourceData}, dataSourceResponse)
		if dataSourceResponse.Diagnostics.HasError() {
			t.Fatalf("registered data source %d rejected configured provider data: %v", index, dataSourceResponse.Diagnostics)
		}
	}

	for index, constructor := range New("test")().Resources(context.Background()) {
		resourceImplementation, ok := constructor().(resource.ResourceWithConfigure)
		if !ok {
			t.Fatalf("registered resource %d does not support configuration", index)
		}

		resourceResponse := &resource.ConfigureResponse{}
		resourceImplementation.Configure(context.Background(), resource.ConfigureRequest{ProviderData: response.ResourceData}, resourceResponse)
		if resourceResponse.Diagnostics.HasError() {
			t.Fatalf("registered resource %d rejected configured provider data: %v", index, resourceResponse.Diagnostics)
		}
	}
}

func TestProviderConfigureReportsLoaderError(t *testing.T) {
	expectedError := errors.New("configuration failed")
	p := &AWSContribProvider{
		loadClient: func(context.Context, conns.Config) (*conns.Client, error) {
			return nil, expectedError
		},
	}
	response := &provider.ConfigureResponse{}

	p.Configure(context.Background(), provider.ConfigureRequest{
		Config: providerConfig(t, "engineering", "ap-southeast-2"),
	}, response)

	if !response.Diagnostics.HasError() {
		t.Fatal("expected configuration diagnostic")
	}
	if len(response.Diagnostics) != 1 {
		t.Fatalf("expected one configuration diagnostic, got %d", len(response.Diagnostics))
	}
	diagnostic := response.Diagnostics[0]
	if diagnostic.Summary() != "Unable to Configure AWS Client" {
		t.Fatalf("unexpected diagnostic summary %q", diagnostic.Summary())
	}
	if !strings.Contains(diagnostic.Detail(), expectedError.Error()) {
		t.Fatalf("expected diagnostic detail to contain %q, got %q", expectedError, diagnostic.Detail())
	}
	if response.ResourceData != nil {
		t.Fatalf("expected ResourceData to remain unset, got %T", response.ResourceData)
	}
	if response.DataSourceData != nil {
		t.Fatalf("expected DataSourceData to remain unset, got %T", response.DataSourceData)
	}
}

func providerConfig(t *testing.T, profile, region string) tfsdk.Config {
	t.Helper()

	providerImplementation := &AWSContribProvider{}
	schemaResponse := &provider.SchemaResponse{}
	providerImplementation.Schema(context.Background(), provider.SchemaRequest{}, schemaResponse)

	return tfsdk.Config{
		Raw: tftypes.NewValue(
			tftypes.Object{AttributeTypes: map[string]tftypes.Type{
				"profile": tftypes.String,
				"region":  tftypes.String,
			}},
			map[string]tftypes.Value{
				"profile": tftypes.NewValue(tftypes.String, profile),
				"region":  tftypes.NewValue(tftypes.String, region),
			},
		),
		Schema: schemaResponse.Schema,
	}
}
