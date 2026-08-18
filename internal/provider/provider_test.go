package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hans-m-song/terraform-provider-awscontrib/internal/conns"
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

func TestProviderRegistersAssociationResource(t *testing.T) {
	p := New("test")()
	constructors := p.Resources(context.Background())
	if len(constructors) != 1 {
		t.Fatalf("expected one resource constructor, got %d", len(constructors))
	}

	response := &resource.MetadataResponse{}
	constructors[0]().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "awscontrib"}, response)
	if response.TypeName != "awscontrib_connect_queue_quick_connect_association" {
		t.Fatalf("unexpected resource type %q", response.TypeName)
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
