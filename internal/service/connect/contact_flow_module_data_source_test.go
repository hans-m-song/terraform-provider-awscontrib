package connect

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const (
	contactFlowModuleDataSourceInstanceID = "00000000-0000-0000-0000-000000000030"
	contactFlowModuleDataSourceName       = "target-module"
)

type fakeContactFlowModuleClient struct {
	search func(context.Context, *awsconnect.SearchContactFlowModulesInput) (*awsconnect.SearchContactFlowModulesOutput, error)
}

func (f *fakeContactFlowModuleClient) SearchContactFlowModules(ctx context.Context, input *awsconnect.SearchContactFlowModulesInput, _ ...func(*awsconnect.Options)) (*awsconnect.SearchContactFlowModulesOutput, error) {
	if f.search == nil {
		return &awsconnect.SearchContactFlowModulesOutput{}, nil
	}
	return f.search(ctx, input)
}

type fakeContactFlowModuleClientFactory struct {
	client *awsconnect.Client
}

func (f fakeContactFlowModuleClientFactory) Connect() *awsconnect.Client {
	return f.client
}

func newTestContactFlowModuleDataSource(client contactFlowModuleClient) *contactFlowModuleDataSource {
	return &contactFlowModuleDataSource{client: client}
}

func TestContactFlowModuleDataSourceMetadataUsesTypeName(t *testing.T) {
	response := &datasource.MetadataResponse{}
	NewContactFlowModuleDataSource().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "awscontrib"}, response)
	if response.TypeName != "awscontrib_connect_contact_flow_module" {
		t.Fatalf("unexpected data source type %q", response.TypeName)
	}
}

func TestContactFlowModuleDataSourceSchema(t *testing.T) {
	response := &datasource.SchemaResponse{}
	NewContactFlowModuleDataSource().Schema(context.Background(), datasource.SchemaRequest{}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", response.Diagnostics)
	}

	for _, name := range []string{"instance_id", "name"} {
		attribute, ok := response.Schema.Attributes[name].(datasourceschema.StringAttribute)
		if !ok {
			t.Fatalf("expected %s to be a string attribute", name)
		}
		if !attribute.Required || attribute.Computed || attribute.Optional {
			t.Fatalf("expected %s to be required only: %#v", name, attribute)
		}
	}

	computedStringFields := []string{"id", "arn", "description", "content", "content_sha256", "settings", "state", "status", "version_description"}
	for _, name := range computedStringFields {
		attribute, ok := response.Schema.Attributes[name].(datasourceschema.StringAttribute)
		if !ok {
			t.Fatalf("expected %s to be a string attribute", name)
		}
		if !attribute.Computed || attribute.Required || attribute.Optional {
			t.Fatalf("expected %s to be computed only: %#v", name, attribute)
		}
	}

	version, ok := response.Schema.Attributes["version"].(datasourceschema.Int64Attribute)
	if !ok || !version.Computed || version.Required || version.Optional {
		t.Fatalf("expected version to be computed int64: %#v", response.Schema.Attributes["version"])
	}
	tags, ok := response.Schema.Attributes["tags"].(datasourceschema.MapAttribute)
	if !ok || !tags.Computed || tags.Required || tags.Optional || !tags.ElementType.Equal(types.StringType) {
		t.Fatalf("expected tags to be a computed string map: %#v", response.Schema.Attributes["tags"])
	}
	if _, ok := response.Schema.Attributes["external_invocation_configuration"]; ok {
		t.Fatal("external invocation configuration is not returned by the selected lookup contract")
	}
}

func TestContactFlowModuleDataSourceConfigure(t *testing.T) {
	implementation := NewContactFlowModuleDataSource()
	configurable, ok := implementation.(datasource.DataSourceWithConfigure)
	if !ok {
		t.Fatal("expected configurable data source")
	}

	response := &datasource.ConfigureResponse{}
	configurable.Configure(context.Background(), datasource.ConfigureRequest{}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected nil provider data diagnostic: %v", response.Diagnostics)
	}

	dataSource, ok := implementation.(*contactFlowModuleDataSource)
	if !ok {
		t.Fatal("expected contact flow module data source implementation")
	}
	response = &datasource.ConfigureResponse{}
	dataSource.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: fakeContactFlowModuleClientFactory{client: &awsconnect.Client{}}}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected typed provider data diagnostic: %v", response.Diagnostics)
	}
	if dataSource.client == nil {
		t.Fatal("expected configured Connect client")
	}

	response = &datasource.ConfigureResponse{}
	configurable.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: "unexpected"}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected unexpected provider data diagnostic")
	}
}

func TestContactFlowModuleSearchCriteriaUsesValidContainsBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		expectSent bool
	}{
		{name: "x", expectSent: false},
		{name: strings.Repeat("x", contactFlowModuleSearchNameMinLength), expectSent: true},
		{name: strings.Repeat("x", contactFlowModuleSearchNameMaxLength), expectSent: true},
		{name: strings.Repeat("x", contactFlowModuleSearchNameMaxLength+1), expectSent: false},
		{name: "éé", expectSent: true},
	}

	for _, test := range tests {
		criteria := contactFlowModuleSearchCriteria(test.name)
		if !test.expectSent {
			if criteria != nil {
				t.Fatalf("expected no server criterion for name %q, got %#v", test.name, criteria)
			}
			continue
		}
		if criteria == nil || criteria.StringCondition == nil {
			t.Fatalf("expected server criterion for name %q", test.name)
		}
		condition := criteria.StringCondition
		if condition.ComparisonType != connecttypes.StringComparisonTypeContains || aws.ToString(condition.FieldName) != "name" || aws.ToString(condition.Value) != test.name {
			t.Fatalf("unexpected criterion for name %q: %#v", test.name, condition)
		}
	}
}

func TestContactFlowModuleDataSourceReadPaginatesExactMatchesAndMapsFields(t *testing.T) {
	var tokens []string
	module := connecttypes.ContactFlowModule{
		Arn:                     aws.String("arn:aws:connect:ap-southeast-2:123456789012:instance/module/target"),
		Content:                 aws.String(`{"Version":"2019-10-30"}`),
		Description:             aws.String("module description"),
		FlowModuleContentSha256: aws.String("hash"),
		Id:                      aws.String("module-id"),
		Name:                    aws.String(contactFlowModuleDataSourceName),
		Settings:                aws.String(`{"timeout":30}`),
		State:                   connecttypes.ContactFlowModuleStateActive,
		Status:                  connecttypes.ContactFlowModuleStatusPublished,
		Tags:                    map[string]string{"environment": "test"},
		Version:                 aws.Int64(7),
		VersionDescription:      aws.String("version description"),
	}
	client := &fakeContactFlowModuleClient{
		search: func(_ context.Context, input *awsconnect.SearchContactFlowModulesInput) (*awsconnect.SearchContactFlowModulesOutput, error) {
			if input.MaxResults == nil || aws.ToInt32(input.MaxResults) != maxContactFlowModulesPerPage {
				t.Fatalf("expected contact-flow-module page size %d, got %#v", maxContactFlowModulesPerPage, input.MaxResults)
			}
			tokens = append(tokens, aws.ToString(input.NextToken))
			if aws.ToString(input.InstanceId) != contactFlowModuleDataSourceInstanceID {
				t.Fatalf("unexpected instance ID %q", aws.ToString(input.InstanceId))
			}
			if input.SearchCriteria == nil || input.SearchCriteria.StringCondition == nil {
				t.Fatal("expected valid name contains criterion")
			}
			if aws.ToString(input.SearchCriteria.StringCondition.Value) != contactFlowModuleDataSourceName {
				t.Fatalf("unexpected criterion value %q", aws.ToString(input.SearchCriteria.StringCondition.Value))
			}
			if input.NextToken == nil {
				return &awsconnect.SearchContactFlowModulesOutput{
					ContactFlowModules: []connecttypes.ContactFlowModule{{Name: aws.String("target-module-extra")}},
					NextToken:          aws.String("page-2"),
				}, nil
			}
			return &awsconnect.SearchContactFlowModulesOutput{ContactFlowModules: []connecttypes.ContactFlowModule{module}}, nil
		},
	}

	response := &datasource.ReadResponse{State: contactFlowModuleState(t)}
	newTestContactFlowModuleDataSource(client).Read(context.Background(), datasource.ReadRequest{Config: contactFlowModuleConfig(t)}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected read diagnostics: %v", response.Diagnostics)
	}
	if !reflect.DeepEqual(tokens, []string{"", "page-2"}) {
		t.Fatalf("expected complete pagination, got tokens %v", tokens)
	}

	var data contactFlowModuleDataSourceModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &data)...)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected state decode diagnostics: %v", response.Diagnostics)
	}
	if data.InstanceID.ValueString() != contactFlowModuleDataSourceInstanceID || data.Name.ValueString() != contactFlowModuleDataSourceName {
		t.Fatalf("expected configured lookup values in state: %#v", data)
	}
	if data.ID.ValueString() != "module-id" || data.ARN.ValueString() == "" || data.Description.ValueString() != "module description" || data.Content.ValueString() == "" || data.ContentSHA256.ValueString() != "hash" || data.Settings.ValueString() == "" || data.State.ValueString() != "ACTIVE" || data.Status.ValueString() != "PUBLISHED" || data.Version.ValueInt64() != 7 || data.VersionDescription.ValueString() != "version description" {
		t.Fatalf("unexpected mapped state: %#v", data)
	}
	var tags map[string]string
	response.Diagnostics.Append(data.Tags.ElementsAs(context.Background(), &tags, false)...)
	if response.Diagnostics.HasError() || !reflect.DeepEqual(tags, map[string]string{"environment": "test"}) {
		t.Fatalf("unexpected mapped tags: %#v (diagnostics: %v)", tags, response.Diagnostics)
	}
}

func TestContactFlowModuleDataSourceReadOmitsInvalidServerCriterion(t *testing.T) {
	for _, name := range []string{"x", strings.Repeat("x", contactFlowModuleSearchNameMaxLength+1)} {
		client := &fakeContactFlowModuleClient{
			search: func(_ context.Context, input *awsconnect.SearchContactFlowModulesInput) (*awsconnect.SearchContactFlowModulesOutput, error) {
				if input.SearchCriteria != nil {
					t.Fatalf("expected invalid-length name criterion to be omitted, got %#v", input.SearchCriteria)
				}
				return &awsconnect.SearchContactFlowModulesOutput{ContactFlowModules: []connecttypes.ContactFlowModule{{Name: aws.String(name)}}, NextToken: nil}, nil
			},
		}
		modules, err := newTestContactFlowModuleDataSource(client).searchContactFlowModules(context.Background(), contactFlowModuleDataSourceInstanceID, name)
		if err != nil || len(modules) != 1 {
			t.Fatalf("unexpected invalid-length lookup result for %q: modules=%v err=%v", name, modules, err)
		}
	}
}

func TestContactFlowModuleDataSourceReadRejectsZeroExactMatches(t *testing.T) {
	client := &fakeContactFlowModuleClient{
		search: func(context.Context, *awsconnect.SearchContactFlowModulesInput) (*awsconnect.SearchContactFlowModulesOutput, error) {
			return &awsconnect.SearchContactFlowModulesOutput{ContactFlowModules: []connecttypes.ContactFlowModule{{Name: aws.String("different")}}}, nil
		},
	}
	response := &datasource.ReadResponse{State: contactFlowModuleState(t)}
	newTestContactFlowModuleDataSource(client).Read(context.Background(), datasource.ReadRequest{Config: contactFlowModuleConfig(t)}, response)
	if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Contact Flow Module Not Found" {
		t.Fatalf("expected not-found diagnostic, got %v", response.Diagnostics)
	}
}

func TestContactFlowModuleDataSourceReadRejectsMultipleExactMatches(t *testing.T) {
	client := &fakeContactFlowModuleClient{
		search: func(context.Context, *awsconnect.SearchContactFlowModulesInput) (*awsconnect.SearchContactFlowModulesOutput, error) {
			return &awsconnect.SearchContactFlowModulesOutput{ContactFlowModules: []connecttypes.ContactFlowModule{
				{Name: aws.String(contactFlowModuleDataSourceName), Id: aws.String("one")},
				{Name: aws.String(contactFlowModuleDataSourceName), Id: aws.String("two")},
			}}, nil
		},
	}
	response := &datasource.ReadResponse{State: contactFlowModuleState(t)}
	newTestContactFlowModuleDataSource(client).Read(context.Background(), datasource.ReadRequest{Config: contactFlowModuleConfig(t)}, response)
	if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Multiple Contact Flow Modules Found" {
		t.Fatalf("expected ambiguity diagnostic, got %v", response.Diagnostics)
	}
}

func TestContactFlowModuleDataSourceReadReportsAPIError(t *testing.T) {
	expectedError := errors.New("search failed")
	client := &fakeContactFlowModuleClient{
		search: func(context.Context, *awsconnect.SearchContactFlowModulesInput) (*awsconnect.SearchContactFlowModulesOutput, error) {
			return nil, expectedError
		},
	}
	response := &datasource.ReadResponse{State: contactFlowModuleState(t)}
	newTestContactFlowModuleDataSource(client).Read(context.Background(), datasource.ReadRequest{Config: contactFlowModuleConfig(t)}, response)
	if !response.Diagnostics.HasError() || !strings.Contains(response.Diagnostics[0].Detail(), expectedError.Error()) {
		t.Fatalf("expected API error diagnostic, got %v", response.Diagnostics)
	}
}

func TestContactFlowModuleDataSourceSearchRejectsDuplicatePaginationToken(t *testing.T) {
	client := &fakeContactFlowModuleClient{
		search: func(_ context.Context, input *awsconnect.SearchContactFlowModulesInput) (*awsconnect.SearchContactFlowModulesOutput, error) {
			return &awsconnect.SearchContactFlowModulesOutput{NextToken: aws.String("page-2")}, nil
		},
	}
	_, err := newTestContactFlowModuleDataSource(client).searchContactFlowModules(context.Background(), contactFlowModuleDataSourceInstanceID, contactFlowModuleDataSourceName)
	if err == nil || !strings.Contains(err.Error(), "duplicate contact-flow-module pagination token") {
		t.Fatalf("expected duplicate pagination token error, got %v", err)
	}
}

func TestContactFlowModuleDataSourceSearchRejectsNilResponse(t *testing.T) {
	client := &fakeContactFlowModuleClient{
		search: func(context.Context, *awsconnect.SearchContactFlowModulesInput) (*awsconnect.SearchContactFlowModulesOutput, error) {
			return nil, nil
		},
	}
	_, err := newTestContactFlowModuleDataSource(client).searchContactFlowModules(context.Background(), contactFlowModuleDataSourceInstanceID, contactFlowModuleDataSourceName)
	if err == nil || !strings.Contains(err.Error(), "SearchContactFlowModules returned a nil response") {
		t.Fatalf("expected nil response error, got %v", err)
	}
}

func TestContactFlowModuleDataSourceReadRequiresConfiguredClient(t *testing.T) {
	response := &datasource.ReadResponse{State: contactFlowModuleState(t)}
	newTestContactFlowModuleDataSource(nil).Read(context.Background(), datasource.ReadRequest{Config: contactFlowModuleConfig(t)}, response)
	if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Amazon Connect Client Not Configured" {
		t.Fatalf("expected missing-client diagnostic, got %v", response.Diagnostics)
	}
}

func contactFlowModuleSchema(t *testing.T) datasourceschema.Schema {
	t.Helper()
	response := &datasource.SchemaResponse{}
	NewContactFlowModuleDataSource().Schema(context.Background(), datasource.SchemaRequest{}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", response.Diagnostics)
	}
	return response.Schema
}

func contactFlowModuleConfig(t *testing.T) tfsdk.Config {
	t.Helper()
	schema := contactFlowModuleSchema(t)
	return tfsdk.Config{Raw: contactFlowModuleRawValue(contactFlowModuleDataSourceInstanceID, contactFlowModuleDataSourceName), Schema: schema}
}

func contactFlowModuleState(t *testing.T) tfsdk.State {
	t.Helper()
	schema := contactFlowModuleSchema(t)
	return tfsdk.State{Raw: contactFlowModuleRawValue(contactFlowModuleDataSourceInstanceID, contactFlowModuleDataSourceName), Schema: schema}
}

func contactFlowModuleRawValue(instanceID, name string) tftypes.Value {
	objectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"instance_id":         tftypes.String,
		"name":                tftypes.String,
		"id":                  tftypes.String,
		"arn":                 tftypes.String,
		"description":         tftypes.String,
		"content":             tftypes.String,
		"content_sha256":      tftypes.String,
		"settings":            tftypes.String,
		"state":               tftypes.String,
		"status":              tftypes.String,
		"version":             tftypes.Number,
		"version_description": tftypes.String,
		"tags":                tftypes.Map{ElementType: tftypes.String},
	}}
	return tftypes.NewValue(objectType, map[string]tftypes.Value{
		"instance_id":         tftypes.NewValue(tftypes.String, instanceID),
		"name":                tftypes.NewValue(tftypes.String, name),
		"id":                  tftypes.NewValue(tftypes.String, nil),
		"arn":                 tftypes.NewValue(tftypes.String, nil),
		"description":         tftypes.NewValue(tftypes.String, nil),
		"content":             tftypes.NewValue(tftypes.String, nil),
		"content_sha256":      tftypes.NewValue(tftypes.String, nil),
		"settings":            tftypes.NewValue(tftypes.String, nil),
		"state":               tftypes.NewValue(tftypes.String, nil),
		"status":              tftypes.NewValue(tftypes.String, nil),
		"version":             tftypes.NewValue(tftypes.Number, nil),
		"version_description": tftypes.NewValue(tftypes.String, nil),
		"tags":                tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil),
	})
}
