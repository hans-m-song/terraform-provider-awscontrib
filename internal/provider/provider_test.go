package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hans-m-song/terraform-provider-awscontrib/internal/conns"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
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

func TestProviderSchemaTagConfiguration(t *testing.T) {
	response := &provider.SchemaResponse{}
	(&AWSContribProvider{}).Schema(context.Background(), provider.SchemaRequest{}, response)

	defaultTags, ok := response.Schema.Blocks["default_tags"].(providerschema.SingleNestedBlock)
	if !ok {
		t.Fatal("expected default_tags single nested block")
	}
	defaultTagsAttribute, ok := defaultTags.Attributes["tags"].(providerschema.MapAttribute)
	if !ok {
		t.Fatal("expected default_tags.tags map attribute")
	}
	if !defaultTagsAttribute.Optional {
		t.Fatal("expected default_tags.tags to be optional")
	}
	if !strings.Contains(defaultTags.MarkdownDescription, "awscontrib_connect_data_table") {
		t.Fatalf("expected default_tags block description to document supported resource, got %q", defaultTags.MarkdownDescription)
	}

	ignoreTags, ok := response.Schema.Blocks["ignore_tags"].(providerschema.SingleNestedBlock)
	if !ok {
		t.Fatal("expected ignore_tags single nested block")
	}
	for _, name := range []string{"keys", "key_prefixes"} {
		attribute, ok := ignoreTags.Attributes[name].(providerschema.SetAttribute)
		if !ok {
			t.Fatalf("expected ignore_tags.%s set attribute", name)
		}
		if !attribute.Optional {
			t.Fatalf("expected ignore_tags.%s to be optional", name)
		}
	}
	if !strings.Contains(ignoreTags.MarkdownDescription, "awscontrib_connect_data_table") || !strings.Contains(ignoreTags.MarkdownDescription, "Do not also configure ignored keys") {
		t.Fatalf("expected ignore_tags block description to document scope and caveat, got %q", ignoreTags.MarkdownDescription)
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
	if len(constructors) != 3 {
		t.Fatalf("expected three data source constructors, got %d", len(constructors))
	}

	expectedTypeNames := []string{
		"awscontrib_connect_phone_number",
		"awscontrib_connect_contact_flow_module",
		"awscontrib_connect_data_table",
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
	if expectedClient.TagConfig.DefaultTags == nil || len(expectedClient.TagConfig.DefaultTags) != 0 {
		t.Fatalf("expected omitted default tags to produce an empty map, got %#v", expectedClient.TagConfig.DefaultTags)
	}
	if len(expectedClient.TagConfig.IgnoreKeys) != 0 || len(expectedClient.TagConfig.IgnoreKeyPrefixes) != 0 {
		t.Fatalf("expected omitted ignored tags to produce empty slices, got %#v", expectedClient.TagConfig)
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

func TestProviderConfigureMapsTagConfiguration(t *testing.T) {
	expectedClient := &conns.Client{}
	p := &AWSContribProvider{
		loadClient: func(context.Context, conns.Config) (*conns.Client, error) {
			return expectedClient, nil
		},
	}
	response := &provider.ConfigureResponse{}

	p.Configure(context.Background(), provider.ConfigureRequest{
		Config: providerConfigWithBlocks(
			t,
			"engineering",
			"ap-southeast-2",
			defaultTagsBlock(tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{
				"environment": tftypes.NewValue(tftypes.String, "test"),
				"owner":       tftypes.NewValue(tftypes.String, "platform"),
			})),
			ignoreTagsBlock(
				tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, []tftypes.Value{
					tftypes.NewValue(tftypes.String, "external"),
					tftypes.NewValue(tftypes.String, "managed"),
				}),
				tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, []tftypes.Value{
					tftypes.NewValue(tftypes.String, "aws:"),
				})),
		),
	}, response)

	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected configuration diagnostics: %v", response.Diagnostics)
	}
	if expectedClient.TagConfig.DefaultTags["environment"] != "test" || expectedClient.TagConfig.DefaultTags["owner"] != "platform" {
		t.Fatalf("unexpected default tags %#v", expectedClient.TagConfig.DefaultTags)
	}
	if strings.Join(expectedClient.TagConfig.IgnoreKeys, ",") != "external,managed" {
		t.Fatalf("expected sorted ignored keys, got %#v", expectedClient.TagConfig.IgnoreKeys)
	}
	if strings.Join(expectedClient.TagConfig.IgnoreKeyPrefixes, ",") != "aws:" {
		t.Fatalf("unexpected ignored key prefixes %#v", expectedClient.TagConfig.IgnoreKeyPrefixes)
	}
}

func TestProviderConfigureAcceptsKnownEmptyTagBlocks(t *testing.T) {
	expectedClient := &conns.Client{}
	p := &AWSContribProvider{
		loadClient: func(context.Context, conns.Config) (*conns.Client, error) {
			return expectedClient, nil
		},
	}
	response := &provider.ConfigureResponse{}

	p.Configure(context.Background(), provider.ConfigureRequest{
		Config: providerConfigWithBlocks(
			t,
			"engineering",
			"ap-southeast-2",
			defaultTagsBlock(tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{})),
			ignoreTagsBlock(
				tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, []tftypes.Value{}),
				tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, []tftypes.Value{}),
			),
		),
	}, response)

	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected configuration diagnostics: %v", response.Diagnostics)
	}
	if expectedClient.TagConfig.DefaultTags == nil || len(expectedClient.TagConfig.DefaultTags) != 0 {
		t.Fatalf("expected empty default tag map, got %#v", expectedClient.TagConfig.DefaultTags)
	}
	if len(expectedClient.TagConfig.IgnoreKeys) != 0 || len(expectedClient.TagConfig.IgnoreKeyPrefixes) != 0 {
		t.Fatalf("expected empty ignored tag sets, got %#v", expectedClient.TagConfig)
	}
}

func TestProviderConfigureRejectsUnknownTagValuesBeforeLoading(t *testing.T) {
	unknownValue := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	config := providerConfigWithBlocks(
		t,
		"engineering",
		"ap-southeast-2",
		defaultTagsBlock(tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{
			"environment": unknownValue,
		})),
		ignoreTagsBlock(
			tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, []tftypes.Value{}),
			tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, nil),
		),
	)
	loaderCalled := false
	p := &AWSContribProvider{
		loadClient: func(context.Context, conns.Config) (*conns.Client, error) {
			loaderCalled = true
			return &conns.Client{}, nil
		},
	}
	response := &provider.ConfigureResponse{}

	p.Configure(context.Background(), provider.ConfigureRequest{Config: config}, response)

	if !response.Diagnostics.HasError() {
		t.Fatal("expected unknown tag values to produce diagnostics")
	}
	if loaderCalled {
		t.Fatal("expected unknown tag values to be rejected before client loading")
	}
	if response.ResourceData != nil || response.DataSourceData != nil {
		t.Fatalf("expected provider data to remain unset, got resource %T and data source %T", response.ResourceData, response.DataSourceData)
	}
}

func TestProviderConfigureRejectsUnknownIgnoredTagValuesBeforeLoading(t *testing.T) {
	unknownValue := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	config := providerConfigWithBlocks(
		t,
		"engineering",
		"ap-southeast-2",
		defaultTagsBlock(tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{})),
		ignoreTagsBlock(
			tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, []tftypes.Value{unknownValue}),
			tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, []tftypes.Value{}),
		),
	)
	loaderCalled := false
	p := &AWSContribProvider{
		loadClient: func(context.Context, conns.Config) (*conns.Client, error) {
			loaderCalled = true
			return &conns.Client{}, nil
		},
	}
	response := &provider.ConfigureResponse{}

	p.Configure(context.Background(), provider.ConfigureRequest{Config: config}, response)

	if !response.Diagnostics.HasError() {
		t.Fatal("expected unknown ignored tag value to produce diagnostics")
	}
	if loaderCalled {
		t.Fatal("expected unknown ignored tag value to be rejected before client loading")
	}
}

func TestProviderConfigureRejectsNullDefaultTagValuesBeforeLoading(t *testing.T) {
	config := providerConfigWithBlocks(
		t,
		"engineering",
		"ap-southeast-2",
		defaultTagsBlock(tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{
			"environment": tftypes.NewValue(tftypes.String, nil),
		})),
		ignoreTagsBlock(
			tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, nil),
			tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, nil),
		),
	)
	loaderCalled := false
	p := &AWSContribProvider{
		loadClient: func(context.Context, conns.Config) (*conns.Client, error) {
			loaderCalled = true
			return &conns.Client{}, nil
		},
	}
	response := &provider.ConfigureResponse{}

	p.Configure(context.Background(), provider.ConfigureRequest{Config: config}, response)

	if !response.Diagnostics.HasError() {
		t.Fatal("expected null default tag value to produce diagnostics")
	}
	if loaderCalled {
		t.Fatal("expected null default tag value to be rejected before client loading")
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
	defaultTags := tftypes.NewValue(providerDefaultTagsType(), nil)
	ignoreTags := tftypes.NewValue(providerIgnoreTagsType(), nil)

	return providerConfigWithBlocks(t, profile, region, defaultTags, ignoreTags)
}

func providerConfigWithBlocks(t *testing.T, profile, region string, defaultTags, ignoreTags tftypes.Value) tfsdk.Config {
	t.Helper()

	providerImplementation := &AWSContribProvider{}
	schemaResponse := &provider.SchemaResponse{}
	providerImplementation.Schema(context.Background(), provider.SchemaRequest{}, schemaResponse)

	defaultTagsType := providerDefaultTagsType()
	ignoreTagsType := providerIgnoreTagsType()
	return tfsdk.Config{
		Raw: tftypes.NewValue(
			tftypes.Object{AttributeTypes: map[string]tftypes.Type{
				"profile":      tftypes.String,
				"region":       tftypes.String,
				"default_tags": defaultTagsType,
				"ignore_tags":  ignoreTagsType,
			}},
			map[string]tftypes.Value{
				"profile":      tftypes.NewValue(tftypes.String, profile),
				"region":       tftypes.NewValue(tftypes.String, region),
				"default_tags": defaultTags,
				"ignore_tags":  ignoreTags,
			},
		),
		Schema: schemaResponse.Schema,
	}
}

func providerDefaultTagsType() tftypes.Object {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"tags": tftypes.Map{ElementType: tftypes.String},
	}}
}

func providerIgnoreTagsType() tftypes.Object {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"keys":         tftypes.Set{ElementType: tftypes.String},
		"key_prefixes": tftypes.Set{ElementType: tftypes.String},
	}}
}

func defaultTagsBlock(tags tftypes.Value) tftypes.Value {
	return tftypes.NewValue(providerDefaultTagsType(), map[string]tftypes.Value{"tags": tags})
}

func ignoreTagsBlock(keys, keyPrefixes tftypes.Value) tftypes.Value {
	return tftypes.NewValue(providerIgnoreTagsType(), map[string]tftypes.Value{
		"keys":         keys,
		"key_prefixes": keyPrefixes,
	})
}
