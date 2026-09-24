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
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

type fakeDataTableLookupClient struct {
	describe func(*awsconnect.DescribeDataTableInput) (*awsconnect.DescribeDataTableOutput, error)
	search   func(*awsconnect.SearchDataTablesInput) (*awsconnect.SearchDataTablesOutput, error)
}

func (f *fakeDataTableLookupClient) DescribeDataTable(_ context.Context, input *awsconnect.DescribeDataTableInput, _ ...func(*awsconnect.Options)) (*awsconnect.DescribeDataTableOutput, error) {
	if f.describe == nil {
		return nil, errors.New("unexpected DescribeDataTable call")
	}
	return f.describe(input)
}

func (f *fakeDataTableLookupClient) SearchDataTables(_ context.Context, input *awsconnect.SearchDataTablesInput, _ ...func(*awsconnect.Options)) (*awsconnect.SearchDataTablesOutput, error) {
	if f.search == nil {
		return nil, errors.New("unexpected SearchDataTables call")
	}
	return f.search(input)
}

func testDataTableLookupSchema(t *testing.T) datasource.SchemaResponse {
	t.Helper()
	response := datasource.SchemaResponse{}
	NewDataTableDataSource().Schema(context.Background(), datasource.SchemaRequest{}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", response.Diagnostics)
	}
	return response
}

func testDataTableLookupRaw(dataTableID, name any) tftypes.Value {
	objectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"instance_id": tftypes.String, "data_table_id": tftypes.String, "name": tftypes.String,
		"id": tftypes.String, "arn": tftypes.String, "description": tftypes.String,
		"time_zone": tftypes.String, "status": tftypes.String, "value_lock_level": tftypes.String,
		"tags": tftypes.Map{ElementType: tftypes.String},
	}}
	return tftypes.NewValue(objectType, map[string]tftypes.Value{
		"instance_id":      tftypes.NewValue(tftypes.String, "instance-id"),
		"data_table_id":    tftypes.NewValue(tftypes.String, dataTableID),
		"name":             tftypes.NewValue(tftypes.String, name),
		"id":               tftypes.NewValue(tftypes.String, nil),
		"arn":              tftypes.NewValue(tftypes.String, nil),
		"description":      tftypes.NewValue(tftypes.String, nil),
		"time_zone":        tftypes.NewValue(tftypes.String, nil),
		"status":           tftypes.NewValue(tftypes.String, nil),
		"value_lock_level": tftypes.NewValue(tftypes.String, nil),
		"tags":             tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil),
	})
}

func testDataTableLookupRequest(t *testing.T, dataTableID, name any) (datasource.ReadRequest, *datasource.ReadResponse) {
	t.Helper()
	schema := testDataTableLookupSchema(t).Schema
	raw := testDataTableLookupRaw(dataTableID, name)
	return datasource.ReadRequest{Config: tfsdk.Config{Raw: raw, Schema: schema}}, &datasource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: schema}}
}

func testDataTableLookupResult() *awsconnect.DescribeDataTableOutput {
	return &awsconnect.DescribeDataTableOutput{DataTable: &connecttypes.DataTable{
		Id: aws.String("table-id"), Arn: aws.String("arn:aws:connect:region:000000000000:instance/instance-id/data-table/table-id"),
		Name: aws.String("target"), Description: aws.String("description"), TimeZone: aws.String("UTC"),
		Status: connecttypes.DataTableStatusPublished, ValueLockLevel: connecttypes.DataTableLockLevelNone,
		Tags: map[string]string{"team": "test"},
	}}
}

func TestDataTableDataSourceMetadataAndSelectors(t *testing.T) {
	response := datasource.MetadataResponse{}
	NewDataTableDataSource().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "awscontrib"}, &response)
	if response.TypeName != "awscontrib_connect_data_table" {
		t.Fatalf("unexpected type name %q", response.TypeName)
	}

	for _, tc := range []struct {
		id, name any
		valid    bool
	}{
		{id: "table-id", name: nil, valid: true},
		{id: nil, name: "target", valid: true},
		{id: nil, name: nil, valid: false},
		{id: "table-id", name: "target", valid: false},
	} {
		schema := testDataTableLookupSchema(t).Schema
		response := datasource.ValidateConfigResponse{}
		validatable, ok := NewDataTableDataSource().(datasource.DataSourceWithValidateConfig)
		if !ok {
			t.Fatal("expected data source configuration validation")
		}
		validatable.ValidateConfig(context.Background(), datasource.ValidateConfigRequest{
			Config: tfsdk.Config{Raw: testDataTableLookupRaw(tc.id, tc.name), Schema: schema},
		}, &response)
		if response.Diagnostics.HasError() == tc.valid {
			t.Fatalf("unexpected selector diagnostics for id=%v name=%v: %v", tc.id, tc.name, response.Diagnostics)
		}
	}
}

func TestDataTableDataSourceReadByID(t *testing.T) {
	client := &fakeDataTableLookupClient{describe: func(input *awsconnect.DescribeDataTableInput) (*awsconnect.DescribeDataTableOutput, error) {
		if aws.ToString(input.InstanceId) != "instance-id" || aws.ToString(input.DataTableId) != "table-id" {
			t.Fatalf("unexpected describe input: %#v", input)
		}
		return testDataTableLookupResult(), nil
	}}
	request, response := testDataTableLookupRequest(t, "table-id", nil)
	(&dataTableDataSource{client: client}).Read(context.Background(), request, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected read diagnostics: %v", response.Diagnostics)
	}
	var state dataTableDataSourceModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &state)...)
	if response.Diagnostics.HasError() || state.ID.ValueString() != "table-id" || state.Name.ValueString() != "target" || state.ARN.IsNull() || state.Description.ValueString() != "description" || state.TimeZone.ValueString() != "UTC" || state.Status.ValueString() != "PUBLISHED" || state.ValueLockLevel.ValueString() != "NONE" || state.DataTableID.ValueString() != "table-id" {
		t.Fatalf("unexpected state: %#v (%v)", state, response.Diagnostics)
	}
	var tags map[string]string
	response.Diagnostics.Append(state.Tags.ElementsAs(context.Background(), &tags, false)...)
	if response.Diagnostics.HasError() || !reflect.DeepEqual(tags, map[string]string{"team": "test"}) {
		t.Fatalf("unexpected tags: %v (%v)", tags, response.Diagnostics)
	}
}

func TestDataTableDataSourceReadByNamePaginatesAndMatchesExactly(t *testing.T) {
	var tokens []string
	client := &fakeDataTableLookupClient{
		search: func(input *awsconnect.SearchDataTablesInput) (*awsconnect.SearchDataTablesOutput, error) {
			tokens = append(tokens, aws.ToString(input.NextToken))
			condition := input.SearchCriteria.StringCondition
			if aws.ToString(input.InstanceId) != "instance-id" || aws.ToInt32(input.MaxResults) != maxDataTablesPerPage || condition.ComparisonType != connecttypes.StringComparisonTypeExact || aws.ToString(condition.FieldName) != "name" || aws.ToString(condition.Value) != "target" {
				t.Fatalf("unexpected search input: %#v", input)
			}
			if input.NextToken == nil {
				return &awsconnect.SearchDataTablesOutput{DataTables: []connecttypes.DataTable{{Name: aws.String("target-extra"), Id: aws.String("wrong")}}, NextToken: aws.String("next")}, nil
			}
			return &awsconnect.SearchDataTablesOutput{DataTables: []connecttypes.DataTable{{Name: aws.String("target"), Id: aws.String("table-id")}}}, nil
		},
		describe: func(input *awsconnect.DescribeDataTableInput) (*awsconnect.DescribeDataTableOutput, error) {
			if aws.ToString(input.DataTableId) != "table-id" {
				t.Fatalf("unexpected described ID %q", aws.ToString(input.DataTableId))
			}
			return testDataTableLookupResult(), nil
		},
	}
	request, response := testDataTableLookupRequest(t, nil, "target")
	(&dataTableDataSource{client: client}).Read(context.Background(), request, response)
	if response.Diagnostics.HasError() || !reflect.DeepEqual(tokens, []string{"", "next"}) {
		t.Fatalf("unexpected lookup: tokens=%v diagnostics=%v", tokens, response.Diagnostics)
	}
	var state dataTableDataSourceModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &state)...)
	if response.Diagnostics.HasError() || state.ID.ValueString() != "table-id" || state.Name.ValueString() != "target" || !state.DataTableID.IsNull() {
		t.Fatalf("unexpected state: %#v (%v)", state, response.Diagnostics)
	}
}

func TestDataTableDataSourceSearchFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		search func(*awsconnect.SearchDataTablesInput) (*awsconnect.SearchDataTablesOutput, error)
	}{
		{name: "missing", search: func(*awsconnect.SearchDataTablesInput) (*awsconnect.SearchDataTablesOutput, error) {
			return &awsconnect.SearchDataTablesOutput{}, nil
		}},
		{name: "ambiguous", search: func(*awsconnect.SearchDataTablesInput) (*awsconnect.SearchDataTablesOutput, error) {
			return &awsconnect.SearchDataTablesOutput{DataTables: []connecttypes.DataTable{{Name: aws.String("target"), Id: aws.String("one")}, {Name: aws.String("target"), Id: aws.String("two")}}}, nil
		}},
		{name: "api error", search: func(*awsconnect.SearchDataTablesInput) (*awsconnect.SearchDataTablesOutput, error) {
			return nil, errors.New("search failed")
		}},
		{name: "nil response", search: func(*awsconnect.SearchDataTablesInput) (*awsconnect.SearchDataTablesOutput, error) { return nil, nil }},
		{name: "repeated token", search: func(*awsconnect.SearchDataTablesInput) (*awsconnect.SearchDataTablesOutput, error) {
			return &awsconnect.SearchDataTablesOutput{NextToken: aws.String("repeat")}, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, response := testDataTableLookupRequest(t, nil, "target")
			(&dataTableDataSource{client: &fakeDataTableLookupClient{search: tc.search}}).Read(context.Background(), request, response)
			if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Unable to Find Data Table" {
				t.Fatalf("expected search diagnostic, got %v", response.Diagnostics)
			}
		})
	}
}

func TestDataTableDataSourceDescribeFailures(t *testing.T) {
	for _, tc := range []struct {
		name, summary string
		output        *awsconnect.DescribeDataTableOutput
		err           error
	}{
		{name: "missing", summary: "Data Table Not Found", err: &connecttypes.ResourceNotFoundException{}},
		{name: "api error", summary: "Unable to Describe Data Table", err: errors.New("describe failed")},
		{name: "nil response", summary: "Incomplete Data Table Response"},
		{name: "incomplete response", summary: "Incomplete Data Table Response", output: &awsconnect.DescribeDataTableOutput{DataTable: &connecttypes.DataTable{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, response := testDataTableLookupRequest(t, "table-id", nil)
			client := &fakeDataTableLookupClient{describe: func(*awsconnect.DescribeDataTableInput) (*awsconnect.DescribeDataTableOutput, error) {
				return tc.output, tc.err
			}}
			(&dataTableDataSource{client: client}).Read(context.Background(), request, response)
			if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != tc.summary {
				t.Fatalf("expected %q diagnostic, got %v", tc.summary, response.Diagnostics)
			}
		})
	}
}

func TestDataTableDataSourceRequiresConfiguredClient(t *testing.T) {
	request, response := testDataTableLookupRequest(t, "table-id", nil)
	(&dataTableDataSource{}).Read(context.Background(), request, response)
	if !response.Diagnostics.HasError() || !strings.Contains(response.Diagnostics[0].Summary(), "Client Not Configured") {
		t.Fatalf("expected missing-client diagnostic, got %v", response.Diagnostics)
	}
}

func TestDataTableDataSourceNilConfigure(t *testing.T) {
	response := datasource.ConfigureResponse{}
	configurable, ok := NewDataTableDataSource().(datasource.DataSourceWithConfigure)
	if !ok {
		t.Fatal("expected configurable data source")
	}
	configurable.Configure(context.Background(), datasource.ConfigureRequest{}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected nil configuration diagnostic: %v", response.Diagnostics)
	}
}
