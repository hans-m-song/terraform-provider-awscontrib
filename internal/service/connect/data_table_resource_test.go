package connect

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	dataTableTestInstanceID = "data-table-instance"
	dataTableTestID         = "data-table-id"
)

type fakeDataTableClient struct {
	createTable     func(context.Context, *awsconnect.CreateDataTableInput) (*awsconnect.CreateDataTableOutput, error)
	describeTable   func(context.Context, *awsconnect.DescribeDataTableInput) (*awsconnect.DescribeDataTableOutput, error)
	updateMetadata  func(context.Context, *awsconnect.UpdateDataTableMetadataInput) (*awsconnect.UpdateDataTableMetadataOutput, error)
	deleteTable     func(context.Context, *awsconnect.DeleteDataTableInput) (*awsconnect.DeleteDataTableOutput, error)
	createAttribute func(context.Context, *awsconnect.CreateDataTableAttributeInput) (*awsconnect.CreateDataTableAttributeOutput, error)
	updateAttribute func(context.Context, *awsconnect.UpdateDataTableAttributeInput) (*awsconnect.UpdateDataTableAttributeOutput, error)
	deleteAttribute func(context.Context, *awsconnect.DeleteDataTableAttributeInput) (*awsconnect.DeleteDataTableAttributeOutput, error)
	listAttributes  func(context.Context, *awsconnect.ListDataTableAttributesInput) (*awsconnect.ListDataTableAttributesOutput, error)
	createValues    func(context.Context, *awsconnect.BatchCreateDataTableValueInput) (*awsconnect.BatchCreateDataTableValueOutput, error)
	updateValues    func(context.Context, *awsconnect.BatchUpdateDataTableValueInput) (*awsconnect.BatchUpdateDataTableValueOutput, error)
	deleteValues    func(context.Context, *awsconnect.BatchDeleteDataTableValueInput) (*awsconnect.BatchDeleteDataTableValueOutput, error)
	listValues      func(context.Context, *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error)
}

func (f *fakeDataTableClient) CreateDataTable(ctx context.Context, input *awsconnect.CreateDataTableInput, _ ...func(*awsconnect.Options)) (*awsconnect.CreateDataTableOutput, error) {
	if f.createTable == nil {
		return &awsconnect.CreateDataTableOutput{Id: aws.String(dataTableTestID), Arn: aws.String("arn:test")}, nil
	}
	return f.createTable(ctx, input)
}

func (f *fakeDataTableClient) DescribeDataTable(ctx context.Context, input *awsconnect.DescribeDataTableInput, _ ...func(*awsconnect.Options)) (*awsconnect.DescribeDataTableOutput, error) {
	if f.describeTable == nil {
		return &awsconnect.DescribeDataTableOutput{DataTable: sampleRemoteDataTable()}, nil
	}
	return f.describeTable(ctx, input)
}

func (f *fakeDataTableClient) UpdateDataTableMetadata(ctx context.Context, input *awsconnect.UpdateDataTableMetadataInput, _ ...func(*awsconnect.Options)) (*awsconnect.UpdateDataTableMetadataOutput, error) {
	if f.updateMetadata == nil {
		return &awsconnect.UpdateDataTableMetadataOutput{}, nil
	}
	return f.updateMetadata(ctx, input)
}

func (f *fakeDataTableClient) DeleteDataTable(ctx context.Context, input *awsconnect.DeleteDataTableInput, _ ...func(*awsconnect.Options)) (*awsconnect.DeleteDataTableOutput, error) {
	if f.deleteTable == nil {
		return &awsconnect.DeleteDataTableOutput{}, nil
	}
	return f.deleteTable(ctx, input)
}

func (f *fakeDataTableClient) CreateDataTableAttribute(ctx context.Context, input *awsconnect.CreateDataTableAttributeInput, _ ...func(*awsconnect.Options)) (*awsconnect.CreateDataTableAttributeOutput, error) {
	if f.createAttribute == nil {
		return &awsconnect.CreateDataTableAttributeOutput{}, nil
	}
	return f.createAttribute(ctx, input)
}

func (f *fakeDataTableClient) UpdateDataTableAttribute(ctx context.Context, input *awsconnect.UpdateDataTableAttributeInput, _ ...func(*awsconnect.Options)) (*awsconnect.UpdateDataTableAttributeOutput, error) {
	if f.updateAttribute == nil {
		return &awsconnect.UpdateDataTableAttributeOutput{}, nil
	}
	return f.updateAttribute(ctx, input)
}

func (f *fakeDataTableClient) DeleteDataTableAttribute(ctx context.Context, input *awsconnect.DeleteDataTableAttributeInput, _ ...func(*awsconnect.Options)) (*awsconnect.DeleteDataTableAttributeOutput, error) {
	if f.deleteAttribute == nil {
		return &awsconnect.DeleteDataTableAttributeOutput{}, nil
	}
	return f.deleteAttribute(ctx, input)
}

func (f *fakeDataTableClient) ListDataTableAttributes(ctx context.Context, input *awsconnect.ListDataTableAttributesInput, _ ...func(*awsconnect.Options)) (*awsconnect.ListDataTableAttributesOutput, error) {
	if f.listAttributes == nil {
		return &awsconnect.ListDataTableAttributesOutput{}, nil
	}
	return f.listAttributes(ctx, input)
}

func (f *fakeDataTableClient) BatchCreateDataTableValue(ctx context.Context, input *awsconnect.BatchCreateDataTableValueInput, _ ...func(*awsconnect.Options)) (*awsconnect.BatchCreateDataTableValueOutput, error) {
	if f.createValues == nil {
		return &awsconnect.BatchCreateDataTableValueOutput{}, nil
	}
	return f.createValues(ctx, input)
}

func (f *fakeDataTableClient) BatchUpdateDataTableValue(ctx context.Context, input *awsconnect.BatchUpdateDataTableValueInput, _ ...func(*awsconnect.Options)) (*awsconnect.BatchUpdateDataTableValueOutput, error) {
	if f.updateValues == nil {
		return &awsconnect.BatchUpdateDataTableValueOutput{}, nil
	}
	return f.updateValues(ctx, input)
}

func (f *fakeDataTableClient) BatchDeleteDataTableValue(ctx context.Context, input *awsconnect.BatchDeleteDataTableValueInput, _ ...func(*awsconnect.Options)) (*awsconnect.BatchDeleteDataTableValueOutput, error) {
	if f.deleteValues == nil {
		return &awsconnect.BatchDeleteDataTableValueOutput{}, nil
	}
	return f.deleteValues(ctx, input)
}

func (f *fakeDataTableClient) ListDataTableValues(ctx context.Context, input *awsconnect.ListDataTableValuesInput, _ ...func(*awsconnect.Options)) (*awsconnect.ListDataTableValuesOutput, error) {
	if f.listValues == nil {
		return &awsconnect.ListDataTableValuesOutput{}, nil
	}
	return f.listValues(ctx, input)
}

func TestDataTableMetadataSchemaAndFactory(t *testing.T) {
	implementation := NewDataTableResource()
	metadata := &resource.MetadataResponse{}
	implementation.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "awscontrib"}, metadata)
	if metadata.TypeName != "awscontrib_connect_data_table" {
		t.Fatalf("unexpected type name %q", metadata.TypeName)
	}
	response := &resource.SchemaResponse{}
	implementation.Schema(context.Background(), resource.SchemaRequest{}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", response.Diagnostics)
	}
	for _, name := range []string{"instance_id", "status"} {
		attribute, ok := response.Schema.Attributes[name].(resourceschema.StringAttribute)
		if !ok || !attribute.Required || len(attribute.PlanModifiers) != 1 {
			t.Fatalf("expected replacement-only required %s, got %#v", name, response.Schema.Attributes[name])
		}
	}
	attributes, ok := response.Schema.Attributes["attributes"].(resourceschema.MapNestedAttribute)
	if !ok || !attributes.Required || attributes.Optional {
		t.Fatalf("expected required attribute map, got %#v", response.Schema.Attributes["attributes"])
	}
	primary, ok := attributes.NestedObject.Attributes["primary"].(resourceschema.BoolAttribute)
	if !ok || !primary.Optional || !primary.Computed || primary.Default == nil {
		t.Fatalf("expected defaulted primary flag, got %#v", attributes.NestedObject.Attributes["primary"])
	}
	defaults, ok := response.Schema.Attributes["default_values"].(resourceschema.MapAttribute)
	if !ok || !defaults.Optional || !defaults.ElementType.Equal(types.StringType) {
		t.Fatalf("expected optional string DEFAULT map, got %#v", response.Schema.Attributes["default_values"])
	}
	if diagnostics := response.Schema.ValidateImplementation(context.Background()); diagnostics.HasError() {
		t.Fatalf("data-table schema must pass Framework implementation validation used by schema export: %v", diagnostics)
	}

	factory := DataTableResourceFactory()
	first, ok := factory().(*dataTableResource)
	if !ok {
		t.Fatal("expected data-table resource")
	}
	second, ok := factory().(*dataTableResource)
	if !ok {
		t.Fatal("expected data-table resource")
	}
	other, ok := DataTableResourceFactory()().(*dataTableResource)
	if !ok {
		t.Fatal("expected data-table resource")
	}
	if first.coordinator != second.coordinator || first.coordinator == other.coordinator {
		t.Fatal("expected coordinator sharing only within one provider factory")
	}
}

func TestDataTableModifyPlanRequiresReplacementForPrimaryTrueToFalse(t *testing.T) {
	implementation, ok := NewDataTableResource().(resource.ResourceWithModifyPlan)
	if !ok {
		t.Fatal("expected resource with plan modification")
	}
	prior := sampleDataTableModel(map[string]attr.Value{"key": dataTableAttributeValue(true, types.StringNull())}, nil)
	planned := sampleDataTableModel(map[string]attr.Value{"key": dataTableAttributeValue(false, types.StringNull())}, nil)
	response := &resource.ModifyPlanResponse{}
	implementation.ModifyPlan(context.Background(), resource.ModifyPlanRequest{State: dataTableState(t, prior), Plan: dataTablePlan(t, planned)}, response)
	if response.Diagnostics.HasError() || len(response.RequiresReplace) != 1 {
		t.Fatalf("expected primary demotion replacement, paths=%v diagnostics=%v", response.RequiresReplace, response.Diagnostics)
	}

	response = &resource.ModifyPlanResponse{}
	implementation.ModifyPlan(context.Background(), resource.ModifyPlanRequest{State: dataTableState(t, planned), Plan: dataTablePlan(t, prior)}, response)
	if response.Diagnostics.HasError() || len(response.RequiresReplace) != 0 {
		t.Fatalf("primary promotion must remain in-place, paths=%v diagnostics=%v", response.RequiresReplace, response.Diagnostics)
	}

	unknownPrimary := types.ObjectValueMust(dataTableAttributeTypes, map[string]attr.Value{
		"value_type": types.StringValue("TEXT"), "description": types.StringNull(), "primary": types.BoolUnknown(),
	})
	unknownPlan := sampleDataTableModel(map[string]attr.Value{"key": unknownPrimary}, nil)
	response = &resource.ModifyPlanResponse{}
	implementation.ModifyPlan(context.Background(), resource.ModifyPlanRequest{State: dataTableState(t, prior), Plan: dataTablePlan(t, unknownPlan)}, response)
	if response.Diagnostics.HasError() || len(response.RequiresReplace) != 0 {
		t.Fatalf("unknown primary plan must be deferred until apply-time planning, paths=%v diagnostics=%v", response.RequiresReplace, response.Diagnostics)
	}
}

func TestDataTableMappingRejectsUnknownAndInvalidDefaults(t *testing.T) {
	model := sampleDataTableModel(map[string]attr.Value{
		"primary": dataTableAttributeValue(true, types.StringNull()),
		"value":   dataTableAttributeValue(false, types.StringNull()),
	}, map[string]attr.Value{"primary": types.StringValue("not-allowed"), "missing": types.StringValue("not-declared")})
	_, diagnostics := dataTableConfigurationFromTerraform(context.Background(), model, false)
	if len(diagnostics.Errors()) != 2 {
		t.Fatalf("expected primary and undeclared DEFAULT diagnostics, got %v", diagnostics)
	}

	model.Attributes = types.MapValueMust(types.ObjectType{AttrTypes: dataTableAttributeTypes}, map[string]attr.Value{"bad": types.ObjectUnknown(dataTableAttributeTypes)})
	_, diagnostics = dataTableConfigurationFromTerraform(context.Background(), model, false)
	if !diagnostics.HasError() {
		t.Fatal("expected unknown attribute object to be rejected at apply mapping")
	}
	model = sampleDataTableModel(map[string]attr.Value{"value": dataTableAttributeValue(false, types.StringNull())}, map[string]attr.Value{"value": types.StringUnknown()})
	_, diagnostics = dataTableConfigurationFromTerraform(context.Background(), model, false)
	if !diagnostics.HasError() {
		t.Fatal("expected unknown DEFAULT value to be rejected at apply mapping")
	}
}

func TestDataTableCreateOrdersAttributesAndOmitsDefaultPrimaryValues(t *testing.T) {
	var operations []string
	var createInput *awsconnect.BatchCreateDataTableValueInput
	client := &fakeDataTableClient{
		createAttribute: func(_ context.Context, input *awsconnect.CreateDataTableAttributeInput) (*awsconnect.CreateDataTableAttributeOutput, error) {
			operations = append(operations, aws.ToString(input.Name))
			return &awsconnect.CreateDataTableAttributeOutput{}, nil
		},
		createValues: func(_ context.Context, input *awsconnect.BatchCreateDataTableValueInput) (*awsconnect.BatchCreateDataTableValueOutput, error) {
			createInput = input
			return &awsconnect.BatchCreateDataTableValueOutput{}, nil
		},
	}
	resource := &dataTableResource{client: client, coordinator: newDataTableCoordinator()}
	key := dataTableKey{instanceID: dataTableTestInstanceID, dataTableID: dataTableTestID}
	attributes := map[string]dataTableAttributeConfiguration{
		"z_nonprimary": {valueType: "TEXT"},
		"b_primary":    {valueType: "TEXT", primary: true},
		"a_primary":    {valueType: "TEXT", primary: true},
		"a_nonprimary": {valueType: "TEXT"},
	}
	if err := resource.createAttributes(context.Background(), key, attributes); err != nil {
		t.Fatalf("unexpected attribute create error: %v", err)
	}
	if !reflect.DeepEqual(operations, []string{"a_primary", "b_primary", "a_nonprimary", "z_nonprimary"}) {
		t.Fatalf("unexpected deterministic create order %v", operations)
	}
	if err := resource.createDefaultValues(context.Background(), key, map[string]string{"z_nonprimary": "z", "a_nonprimary": "a"}); err != nil {
		t.Fatalf("unexpected DEFAULT create error: %v", err)
	}
	if createInput == nil || len(createInput.Values) != 2 || aws.ToString(createInput.Values[0].AttributeName) != "a_nonprimary" {
		t.Fatalf("unexpected deterministic DEFAULT input %#v", createInput)
	}
	for _, value := range createInput.Values {
		if value.PrimaryValues != nil {
			t.Fatalf("DEFAULT create must omit PrimaryValues, got %#v", value.PrimaryValues)
		}
	}
}

func TestDataTableCreateRefreshesAuthoritativeState(t *testing.T) {
	var operations []string
	client := &fakeDataTableClient{
		createTable: func(_ context.Context, input *awsconnect.CreateDataTableInput) (*awsconnect.CreateDataTableOutput, error) {
			operations = append(operations, "table")
			if input.Description != nil {
				t.Fatalf("omitted description must remain nil on create, got %q", aws.ToString(input.Description))
			}
			return &awsconnect.CreateDataTableOutput{Id: aws.String(dataTableTestID), Arn: aws.String("arn:test")}, nil
		},
		createAttribute: func(_ context.Context, input *awsconnect.CreateDataTableAttributeInput) (*awsconnect.CreateDataTableAttributeOutput, error) {
			operations = append(operations, "attribute:"+aws.ToString(input.Name))
			if input.Description != nil {
				t.Fatalf("omitted attribute description must remain nil on create, got %q", aws.ToString(input.Description))
			}
			return &awsconnect.CreateDataTableAttributeOutput{}, nil
		},
		createValues: func(_ context.Context, input *awsconnect.BatchCreateDataTableValueInput) (*awsconnect.BatchCreateDataTableValueOutput, error) {
			operations = append(operations, "default:"+aws.ToString(input.Values[0].AttributeName))
			return &awsconnect.BatchCreateDataTableValueOutput{}, nil
		},
		listAttributes: func(context.Context, *awsconnect.ListDataTableAttributesInput) (*awsconnect.ListDataTableAttributesOutput, error) {
			return &awsconnect.ListDataTableAttributesOutput{Attributes: []connecttypes.DataTableAttribute{
				{Name: aws.String("key"), ValueType: connecttypes.DataTableAttributeValueTypeText, Primary: true},
				{Name: aws.String("value"), ValueType: connecttypes.DataTableAttributeValueTypeText},
			}}, nil
		},
		listValues: func(context.Context, *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error) {
			return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{{AttributeName: aws.String("value"), RecordId: aws.String(defaultDataTableRecordID), Value: aws.String("default")}}}, nil
		},
	}
	implementation := &dataTableResource{client: client, coordinator: newDataTableCoordinator()}
	planned := sampleDataTableModel(map[string]attr.Value{
		"key":   dataTableAttributeValue(true, types.StringNull()),
		"value": dataTableAttributeValue(false, types.StringNull()),
	}, map[string]attr.Value{"value": types.StringValue("default")})
	response := &resource.CreateResponse{State: dataTableSchemaOnlyState(t)}
	implementation.Create(context.Background(), resource.CreateRequest{Plan: dataTablePlan(t, planned)}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected create diagnostics: %v", response.Diagnostics)
	}
	if !reflect.DeepEqual(operations, []string{"table", "attribute:key", "attribute:value", "default:value"}) {
		t.Fatalf("unexpected create lifecycle %v", operations)
	}
	var state dataTableModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &state)...)
	defaultValue, ok := state.DefaultValues.Elements()["value"].(types.String)
	if response.Diagnostics.HasError() || !ok || state.ID.ValueString() != dataTableTestID || len(state.Attributes.Elements()) != 2 || defaultValue.ValueString() != "default" {
		t.Fatalf("unexpected authoritative create state %#v diagnostics=%v", state, response.Diagnostics)
	}
}

func TestDataTableUpdateBatchFailuresAreActionableAndKeepPriorState(t *testing.T) {
	for _, testCase := range []struct {
		name              string
		plannedDefaults   map[string]attr.Value
		configureFailure  func(*fakeDataTableClient)
		expectedOperation string
		expectedMessage   string
	}{
		{
			name:              "update",
			plannedDefaults:   map[string]attr.Value{"value": types.StringValue("new")},
			expectedOperation: "update",
			expectedMessage:   "update rejected",
			configureFailure: func(client *fakeDataTableClient) {
				client.updateValues = func(context.Context, *awsconnect.BatchUpdateDataTableValueInput) (*awsconnect.BatchUpdateDataTableValueOutput, error) {
					return &awsconnect.BatchUpdateDataTableValueOutput{Failed: []connecttypes.BatchUpdateDataTableValueFailureResult{{AttributeName: aws.String("value"), Message: aws.String("update rejected")}}}, nil
				}
			},
		},
		{
			name:              "delete",
			plannedDefaults:   nil,
			expectedOperation: "delete",
			expectedMessage:   "delete rejected",
			configureFailure: func(client *fakeDataTableClient) {
				client.deleteValues = func(context.Context, *awsconnect.BatchDeleteDataTableValueInput) (*awsconnect.BatchDeleteDataTableValueOutput, error) {
					return &awsconnect.BatchDeleteDataTableValueOutput{Failed: []connecttypes.BatchDeleteDataTableValueFailureResult{{AttributeName: aws.String("value"), Message: aws.String("delete rejected")}}}, nil
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			lockVersion := &connecttypes.DataTableLockVersion{Value: aws.String(testCase.name + "-lock")}
			client := &fakeDataTableClient{
				listAttributes: func(context.Context, *awsconnect.ListDataTableAttributesInput) (*awsconnect.ListDataTableAttributesOutput, error) {
					return &awsconnect.ListDataTableAttributesOutput{Attributes: []connecttypes.DataTableAttribute{{Name: aws.String("value"), ValueType: connecttypes.DataTableAttributeValueTypeText}}}, nil
				},
				listValues: func(context.Context, *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error) {
					return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{{AttributeName: aws.String("value"), RecordId: aws.String(defaultDataTableRecordID), Value: aws.String("old"), LockVersion: lockVersion}}}, nil
				},
			}
			testCase.configureFailure(client)
			prior := sampleDataTableModel(map[string]attr.Value{"value": dataTableAttributeValue(false, types.StringNull())}, map[string]attr.Value{"value": types.StringValue("old")})
			planned := sampleDataTableModel(map[string]attr.Value{"value": dataTableAttributeValue(false, types.StringNull())}, testCase.plannedDefaults)
			implementation := &dataTableResource{client: client, coordinator: newDataTableCoordinator()}
			response := &resource.UpdateResponse{State: dataTableState(t, prior)}
			implementation.Update(context.Background(), resource.UpdateRequest{State: dataTableState(t, prior), Plan: dataTablePlan(t, planned)}, response)
			if !response.Diagnostics.HasError() {
				t.Fatalf("expected nonempty Batch%s Failed diagnostic", testCase.expectedOperation)
			}
			detail := response.Diagnostics.Errors()[0].Detail()
			if !strings.Contains(detail, "value") || !strings.Contains(detail, testCase.expectedMessage) {
				t.Fatalf("expected actionable attribute and message in diagnostic, got %q", detail)
			}
			var retained dataTableModel
			response.Diagnostics.Append(response.State.Get(context.Background(), &retained)...)
			retainedValue, ok := retained.DefaultValues.Elements()["value"].(types.String)
			if !ok || retainedValue.ValueString() != "old" {
				t.Fatalf("failed batch must retain prior state instead of planned state, defaults=%#v diagnostics=%v", retained.DefaultValues, response.Diagnostics)
			}
		})
	}
}

func TestDataTableCreateChildFailurePreservesRecoverableIdentityState(t *testing.T) {
	childFailure := errors.New("attribute create failed")
	client := &fakeDataTableClient{
		createAttribute: func(context.Context, *awsconnect.CreateDataTableAttributeInput) (*awsconnect.CreateDataTableAttributeOutput, error) {
			return nil, childFailure
		},
	}
	implementation := &dataTableResource{client: client, coordinator: newDataTableCoordinator()}
	planned := sampleDataTableModel(map[string]attr.Value{"value": dataTableAttributeValue(false, types.StringNull())}, nil)
	planned.ID = types.StringUnknown()
	planned.ARN = types.StringUnknown()
	response := &resource.CreateResponse{State: dataTableState(t, planned)}
	implementation.Create(context.Background(), resource.CreateRequest{Plan: dataTablePlan(t, planned)}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected child mutation diagnostic")
	}
	var state dataTableModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &state)...)
	if state.InstanceID.ValueString() != dataTableTestInstanceID || state.ID.ValueString() != dataTableTestID || state.ARN.ValueString() != "arn:test" {
		t.Fatalf("expected recoverable table identity after child failure, got instance=%v id=%v arn=%v diagnostics=%v", state.InstanceID, state.ID, state.ARN, response.Diagnostics)
	}
}

func TestDataTableReadPaginatesAndFiltersDefaultRecord(t *testing.T) {
	attributePages := 0
	valuePages := 0
	client := &fakeDataTableClient{
		listAttributes: func(_ context.Context, input *awsconnect.ListDataTableAttributesInput) (*awsconnect.ListDataTableAttributesOutput, error) {
			attributePages++
			if input.MaxResults == nil || aws.ToInt32(input.MaxResults) != maxDataTableAttributesPerPage {
				t.Fatalf("expected data-table attribute page size %d, got %#v", maxDataTableAttributesPerPage, input.MaxResults)
			}
			if input.NextToken == nil {
				return &awsconnect.ListDataTableAttributesOutput{Attributes: []connecttypes.DataTableAttribute{{Name: aws.String("key"), ValueType: connecttypes.DataTableAttributeValueTypeText, Primary: true}}, NextToken: aws.String("next")}, nil
			}
			return &awsconnect.ListDataTableAttributesOutput{Attributes: []connecttypes.DataTableAttribute{{Name: aws.String("answer"), ValueType: connecttypes.DataTableAttributeValueTypeNumber, Description: aws.String("description")}}}, nil
		},
		listValues: func(_ context.Context, input *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error) {
			valuePages++
			if input.MaxResults == nil || aws.ToInt32(input.MaxResults) != maxDataTableValuesPerPage {
				t.Fatalf("expected data-table value page size %d, got %#v", maxDataTableValuesPerPage, input.MaxResults)
			}
			if !reflect.DeepEqual(input.RecordIds, []string{defaultDataTableRecordID}) {
				t.Fatalf("expected DEFAULT record filter on every page, got %v", input.RecordIds)
			}
			if input.NextToken == nil {
				return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{{AttributeName: aws.String("answer"), RecordId: aws.String("ordinary"), Value: aws.String("ignored")}}, NextToken: aws.String("next")}, nil
			}
			return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{{AttributeName: aws.String("answer"), RecordId: aws.String(defaultDataTableRecordID), Value: aws.String("42"), LockVersion: &connecttypes.DataTableLockVersion{Value: aws.String("lock")}}}}, nil
		},
	}
	resource := &dataTableResource{client: client}
	model, err := resource.readRemote(context.Background(), dataTableKey{instanceID: dataTableTestInstanceID, dataTableID: dataTableTestID})
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if attributePages != 2 || valuePages != 2 || len(model.Attributes.Elements()) != 2 {
		t.Fatalf("expected complete pagination, attribute_pages=%d value_pages=%d attributes=%v", attributePages, valuePages, model.Attributes)
	}
	answer, ok := model.DefaultValues.Elements()["answer"].(types.String)
	if !ok {
		t.Fatalf("expected string DEFAULT value, got %#v", model.DefaultValues.Elements()["answer"])
	}
	if got := answer.ValueString(); got != "42" {
		t.Fatalf("unexpected reconstructed DEFAULT %q", got)
	}
}

func TestDataTableUpdateSkipsUnchangedMetadata(t *testing.T) {
	remote := sampleRemoteDataTable()
	remote.Description = aws.String("")
	metadataCalls := 0
	describeCalls := 0
	client := &fakeDataTableClient{
		describeTable: func(context.Context, *awsconnect.DescribeDataTableInput) (*awsconnect.DescribeDataTableOutput, error) {
			describeCalls++
			return &awsconnect.DescribeDataTableOutput{DataTable: remote}, nil
		},
		updateMetadata: func(context.Context, *awsconnect.UpdateDataTableMetadataInput) (*awsconnect.UpdateDataTableMetadataOutput, error) {
			metadataCalls++
			return &awsconnect.UpdateDataTableMetadataOutput{}, nil
		},
		listAttributes: func(_ context.Context, input *awsconnect.ListDataTableAttributesInput) (*awsconnect.ListDataTableAttributesOutput, error) {
			if input.MaxResults == nil || aws.ToInt32(input.MaxResults) != maxDataTableAttributesPerPage {
				t.Fatalf("expected data-table attribute page size %d, got %#v", maxDataTableAttributesPerPage, input.MaxResults)
			}
			return &awsconnect.ListDataTableAttributesOutput{}, nil
		},
		listValues: func(_ context.Context, input *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error) {
			if input.MaxResults == nil || aws.ToInt32(input.MaxResults) != maxDataTableValuesPerPage || !reflect.DeepEqual(input.RecordIds, []string{defaultDataTableRecordID}) {
				t.Fatalf("unexpected DEFAULT value list request: %#v", input)
			}
			return &awsconnect.ListDataTableValuesOutput{}, nil
		},
	}
	prior := sampleDataTableModel(nil, nil)
	planned := sampleDataTableModel(nil, nil)
	response := &resource.UpdateResponse{State: dataTableState(t, prior)}
	implementation := &dataTableResource{client: client, coordinator: newDataTableCoordinator()}
	implementation.Update(context.Background(), resource.UpdateRequest{State: dataTableState(t, prior), Plan: dataTablePlan(t, planned)}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected unchanged-metadata update diagnostics: %v", response.Diagnostics)
	}
	if metadataCalls != 0 {
		t.Fatalf("expected unchanged metadata to avoid UpdateDataTableMetadata, calls=%d", metadataCalls)
	}
	if describeCalls != 2 {
		t.Fatalf("expected unchanged DEFAULT values to avoid lock refresh, describe_calls=%d", describeCalls)
	}
}

func TestDataTableReadRejectsRepeatedPaginationTokensIndependently(t *testing.T) {
	t.Run("attributes", func(t *testing.T) {
		attributeCalls := 0
		client := &fakeDataTableClient{listAttributes: func(context.Context, *awsconnect.ListDataTableAttributesInput) (*awsconnect.ListDataTableAttributesOutput, error) {
			attributeCalls++
			return &awsconnect.ListDataTableAttributesOutput{NextToken: aws.String("repeated-attribute-token")}, nil
		}}
		_, err := (&dataTableResource{client: client}).readRemoteSnapshot(context.Background(), dataTableKey{instanceID: "i", dataTableID: "t"})
		if err == nil || !strings.Contains(err.Error(), "repeated data-table attribute pagination token") || attributeCalls != 2 {
			t.Fatalf("expected bounded attribute pagination cycle error, calls=%d err=%v", attributeCalls, err)
		}
	})

	t.Run("values", func(t *testing.T) {
		valueCalls := 0
		client := &fakeDataTableClient{listValues: func(context.Context, *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error) {
			valueCalls++
			return &awsconnect.ListDataTableValuesOutput{NextToken: aws.String("repeated-value-token")}, nil
		}}
		_, err := (&dataTableResource{client: client}).readRemoteSnapshot(context.Background(), dataTableKey{instanceID: "i", dataTableID: "t"})
		if err == nil || !strings.Contains(err.Error(), "repeated data-table value pagination token") || valueCalls != 2 {
			t.Fatalf("expected bounded value pagination cycle error, calls=%d err=%v", valueCalls, err)
		}
	})
}

func TestDataTableReadRejectsUnsupportedRemoteStatus(t *testing.T) {
	remote := sampleRemoteDataTable()
	remote.Status = connecttypes.DataTableStatus("SAVED")
	client := &fakeDataTableClient{describeTable: func(context.Context, *awsconnect.DescribeDataTableInput) (*awsconnect.DescribeDataTableOutput, error) {
		return &awsconnect.DescribeDataTableOutput{DataTable: remote}, nil
	}}
	_, err := (&dataTableResource{client: client}).readRemote(context.Background(), dataTableKey{instanceID: "i", dataTableID: "t"})
	if err == nil || !strings.Contains(err.Error(), `unsupported remote data-table status "SAVED"`) || !strings.Contains(err.Error(), "supports only PUBLISHED") {
		t.Fatalf("expected actionable unsupported status error, got %v", err)
	}
}

func TestDataTableDefaultReconciliationUsesLocksAndOrdersOperations(t *testing.T) {
	var operations []string
	oldLock := &connecttypes.DataTableLockVersion{Value: aws.String("old-lock")}
	deleteLock := &connecttypes.DataTableLockVersion{Value: aws.String("delete-lock")}
	client := &fakeDataTableClient{
		createValues: func(_ context.Context, input *awsconnect.BatchCreateDataTableValueInput) (*awsconnect.BatchCreateDataTableValueOutput, error) {
			operations = append(operations, "create:"+aws.ToString(input.Values[0].AttributeName))
			return &awsconnect.BatchCreateDataTableValueOutput{}, nil
		},
		updateValues: func(_ context.Context, input *awsconnect.BatchUpdateDataTableValueInput) (*awsconnect.BatchUpdateDataTableValueOutput, error) {
			operations = append(operations, "update:"+aws.ToString(input.Values[0].AttributeName))
			if input.Values[0].LockVersion != oldLock || input.Values[0].PrimaryValues != nil {
				t.Fatalf("update did not preserve fresh DEFAULT lock or omitted primary values: %#v", input.Values[0])
			}
			return &awsconnect.BatchUpdateDataTableValueOutput{}, nil
		},
		deleteValues: func(_ context.Context, input *awsconnect.BatchDeleteDataTableValueInput) (*awsconnect.BatchDeleteDataTableValueOutput, error) {
			operations = append(operations, "delete:"+aws.ToString(input.Values[0].AttributeName))
			if input.Values[0].LockVersion != deleteLock || input.Values[0].PrimaryValues != nil {
				t.Fatalf("delete did not preserve fresh DEFAULT lock or omitted primary values: %#v", input.Values[0])
			}
			return &awsconnect.BatchDeleteDataTableValueOutput{}, nil
		},
	}
	resource := &dataTableResource{client: client}
	remote := map[string]dataTableRemoteDefault{"change": {value: "old", lockVersion: oldLock}, "remove": {value: "old", lockVersion: deleteLock}, "same": {value: "same", lockVersion: oldLock}}
	desired := map[string]string{"add": "new", "change": "new", "same": "same"}
	if err := resource.reconcileDefaultValues(context.Background(), dataTableKey{instanceID: "i", dataTableID: "t"}, remote, desired); err != nil {
		t.Fatalf("unexpected DEFAULT reconciliation error: %v", err)
	}
	if !reflect.DeepEqual(operations, []string{"create:add", "update:change", "delete:remove"}) {
		t.Fatalf("unexpected operation order %v", operations)
	}
}

func TestDataTablePartialBatchFailureIsErrorAndDeterministic(t *testing.T) {
	client := &fakeDataTableClient{createValues: func(context.Context, *awsconnect.BatchCreateDataTableValueInput) (*awsconnect.BatchCreateDataTableValueOutput, error) {
		return &awsconnect.BatchCreateDataTableValueOutput{Failed: []connecttypes.BatchCreateDataTableValueFailureResult{
			{AttributeName: aws.String("z"), Message: aws.String("last")},
			{AttributeName: aws.String("a"), Message: aws.String("first")},
		}}, nil
	}}
	resource := &dataTableResource{client: client}
	err := resource.createDefaultValues(context.Background(), dataTableKey{instanceID: "i", dataTableID: "t"}, map[string]string{"a": "1", "z": "2"})
	if err == nil || err.Error() != "batch create DEFAULT values failed: a: first; z: last" {
		t.Fatalf("unexpected partial failure %v", err)
	}
}

func TestDataTableAttributeReconciliationCreateUpdateDeleteOrdering(t *testing.T) {
	var operations []string
	client := &fakeDataTableClient{
		createAttribute: func(_ context.Context, input *awsconnect.CreateDataTableAttributeInput) (*awsconnect.CreateDataTableAttributeOutput, error) {
			operations = append(operations, "create:"+aws.ToString(input.Name))
			return &awsconnect.CreateDataTableAttributeOutput{}, nil
		},
		updateAttribute: func(_ context.Context, input *awsconnect.UpdateDataTableAttributeInput) (*awsconnect.UpdateDataTableAttributeOutput, error) {
			operations = append(operations, "update:"+aws.ToString(input.AttributeName))
			return &awsconnect.UpdateDataTableAttributeOutput{}, nil
		},
		deleteAttribute: func(_ context.Context, input *awsconnect.DeleteDataTableAttributeInput) (*awsconnect.DeleteDataTableAttributeOutput, error) {
			operations = append(operations, "delete:"+aws.ToString(input.AttributeName))
			return &awsconnect.DeleteDataTableAttributeOutput{}, nil
		},
	}
	resource := &dataTableResource{client: client}
	key := dataTableKey{instanceID: "i", dataTableID: "t"}
	remote := map[string]dataTableAttributeConfiguration{"change": {valueType: "TEXT"}, "remove": {valueType: "TEXT"}, "same": {valueType: "TEXT"}}
	desired := map[string]dataTableAttributeConfiguration{"add": {valueType: "NUMBER"}, "change": {valueType: "NUMBER"}, "same": {valueType: "TEXT"}}
	if err := resource.reconcileAttributesBeforeDefaults(context.Background(), key, remote, desired); err != nil {
		t.Fatal(err)
	}
	if err := resource.deleteRemovedAttributes(context.Background(), key, remote, desired); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(operations, []string{"create:add", "update:change", "delete:remove"}) {
		t.Fatalf("unexpected attribute lifecycle %v", operations)
	}
}

func TestDataTableUpdateReconcilesFullLifecycleWithFreshLocks(t *testing.T) {
	var operations []string
	describeCount := 0
	updateLock := &connecttypes.DataTableLockVersion{Value: aws.String("update-lock")}
	deleteLock := &connecttypes.DataTableLockVersion{Value: aws.String("delete-lock")}
	client := &fakeDataTableClient{
		describeTable: func(context.Context, *awsconnect.DescribeDataTableInput) (*awsconnect.DescribeDataTableOutput, error) {
			describeCount++
			return &awsconnect.DescribeDataTableOutput{DataTable: sampleRemoteDataTable()}, nil
		},
		listAttributes: func(context.Context, *awsconnect.ListDataTableAttributesInput) (*awsconnect.ListDataTableAttributesOutput, error) {
			switch describeCount {
			case 1:
				return &awsconnect.ListDataTableAttributesOutput{Attributes: []connecttypes.DataTableAttribute{
					{Name: aws.String("change"), ValueType: connecttypes.DataTableAttributeValueTypeText},
					{Name: aws.String("remove"), ValueType: connecttypes.DataTableAttributeValueTypeText},
					{Name: aws.String("same"), ValueType: connecttypes.DataTableAttributeValueTypeText},
				}}, nil
			case 2:
				return &awsconnect.ListDataTableAttributesOutput{Attributes: []connecttypes.DataTableAttribute{
					{Name: aws.String("add"), ValueType: connecttypes.DataTableAttributeValueTypeNumber},
					{Name: aws.String("change"), ValueType: connecttypes.DataTableAttributeValueTypeNumber},
					{Name: aws.String("remove"), ValueType: connecttypes.DataTableAttributeValueTypeText},
					{Name: aws.String("same"), ValueType: connecttypes.DataTableAttributeValueTypeText},
				}}, nil
			default:
				return &awsconnect.ListDataTableAttributesOutput{Attributes: []connecttypes.DataTableAttribute{
					{Name: aws.String("add"), ValueType: connecttypes.DataTableAttributeValueTypeNumber},
					{Name: aws.String("change"), ValueType: connecttypes.DataTableAttributeValueTypeNumber},
					{Name: aws.String("same"), ValueType: connecttypes.DataTableAttributeValueTypeText},
				}}, nil
			}
		},
		listValues: func(context.Context, *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error) {
			if describeCount < 3 {
				return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{
					{AttributeName: aws.String("change"), RecordId: aws.String(defaultDataTableRecordID), Value: aws.String("old"), LockVersion: updateLock},
					{AttributeName: aws.String("remove"), RecordId: aws.String(defaultDataTableRecordID), Value: aws.String("old"), LockVersion: deleteLock},
				}}, nil
			}
			return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{
				{AttributeName: aws.String("add"), RecordId: aws.String(defaultDataTableRecordID), Value: aws.String("new")},
				{AttributeName: aws.String("change"), RecordId: aws.String(defaultDataTableRecordID), Value: aws.String("new")},
			}}, nil
		},
		updateMetadata: func(_ context.Context, input *awsconnect.UpdateDataTableMetadataInput) (*awsconnect.UpdateDataTableMetadataOutput, error) {
			operations = append(operations, "metadata")
			if input.Description == nil || aws.ToString(input.Description) != "" {
				t.Fatalf("description removal must send an explicit empty string, got %#v", input.Description)
			}
			return &awsconnect.UpdateDataTableMetadataOutput{}, nil
		},
		createAttribute: func(_ context.Context, input *awsconnect.CreateDataTableAttributeInput) (*awsconnect.CreateDataTableAttributeOutput, error) {
			operations = append(operations, "create-attribute:"+aws.ToString(input.Name))
			return &awsconnect.CreateDataTableAttributeOutput{}, nil
		},
		updateAttribute: func(_ context.Context, input *awsconnect.UpdateDataTableAttributeInput) (*awsconnect.UpdateDataTableAttributeOutput, error) {
			operations = append(operations, "update-attribute:"+aws.ToString(input.AttributeName))
			return &awsconnect.UpdateDataTableAttributeOutput{}, nil
		},
		createValues: func(_ context.Context, input *awsconnect.BatchCreateDataTableValueInput) (*awsconnect.BatchCreateDataTableValueOutput, error) {
			operations = append(operations, "create-default:"+aws.ToString(input.Values[0].AttributeName))
			return &awsconnect.BatchCreateDataTableValueOutput{}, nil
		},
		updateValues: func(_ context.Context, input *awsconnect.BatchUpdateDataTableValueInput) (*awsconnect.BatchUpdateDataTableValueOutput, error) {
			operations = append(operations, "update-default:"+aws.ToString(input.Values[0].AttributeName))
			if input.Values[0].LockVersion != updateLock {
				t.Fatalf("expected refreshed update lock, got %#v", input.Values[0].LockVersion)
			}
			return &awsconnect.BatchUpdateDataTableValueOutput{}, nil
		},
		deleteValues: func(_ context.Context, input *awsconnect.BatchDeleteDataTableValueInput) (*awsconnect.BatchDeleteDataTableValueOutput, error) {
			operations = append(operations, "delete-default:"+aws.ToString(input.Values[0].AttributeName))
			if input.Values[0].LockVersion != deleteLock {
				t.Fatalf("expected refreshed delete lock, got %#v", input.Values[0].LockVersion)
			}
			return &awsconnect.BatchDeleteDataTableValueOutput{}, nil
		},
		deleteAttribute: func(_ context.Context, input *awsconnect.DeleteDataTableAttributeInput) (*awsconnect.DeleteDataTableAttributeOutput, error) {
			operations = append(operations, "delete-attribute:"+aws.ToString(input.AttributeName))
			return &awsconnect.DeleteDataTableAttributeOutput{}, nil
		},
	}
	prior := sampleDataTableModel(map[string]attr.Value{
		"change": dataTableAttributeValue(false, types.StringNull()),
		"remove": dataTableAttributeValue(false, types.StringNull()),
		"same":   dataTableAttributeValue(false, types.StringNull()),
	}, map[string]attr.Value{"change": types.StringValue("old"), "remove": types.StringValue("old")})
	planned := sampleDataTableModel(map[string]attr.Value{
		"add":    dataTableAttributeValueWithType("NUMBER", false),
		"change": dataTableAttributeValueWithType("NUMBER", false),
		"same":   dataTableAttributeValue(false, types.StringNull()),
	}, map[string]attr.Value{"add": types.StringValue("new"), "change": types.StringValue("new")})
	implementation := &dataTableResource{client: client, coordinator: newDataTableCoordinator()}
	response := &resource.UpdateResponse{State: dataTableState(t, prior)}
	implementation.Update(context.Background(), resource.UpdateRequest{State: dataTableState(t, prior), Plan: dataTablePlan(t, planned)}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected update diagnostics: %v", response.Diagnostics)
	}
	wantOperations := []string{"metadata", "create-attribute:add", "update-attribute:change", "create-default:add", "update-default:change", "delete-default:remove", "delete-attribute:remove"}
	if !reflect.DeepEqual(operations, wantOperations) || describeCount != 3 {
		t.Fatalf("unexpected update lifecycle operations=%v describe_count=%d", operations, describeCount)
	}
	var state dataTableModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &state)...)
	if response.Diagnostics.HasError() || len(state.Attributes.Elements()) != 3 || len(state.DefaultValues.Elements()) != 2 {
		t.Fatalf("unexpected authoritative update state %#v diagnostics=%v", state, response.Diagnostics)
	}
}

func TestDataTableUpdateAndDeleteAPIFailuresPreserveDiagnostics(t *testing.T) {
	apiFailure := errors.New("api failed")
	client := &fakeDataTableClient{
		updateMetadata: func(context.Context, *awsconnect.UpdateDataTableMetadataInput) (*awsconnect.UpdateDataTableMetadataOutput, error) {
			return nil, apiFailure
		},
		deleteTable: func(context.Context, *awsconnect.DeleteDataTableInput) (*awsconnect.DeleteDataTableOutput, error) {
			return nil, apiFailure
		},
	}
	model := sampleDataTableModel(nil, nil)
	implementation := &dataTableResource{client: client, coordinator: newDataTableCoordinator()}
	updateResponse := &resource.UpdateResponse{State: dataTableState(t, model)}
	implementation.Update(context.Background(), resource.UpdateRequest{State: dataTableState(t, model), Plan: dataTablePlan(t, model)}, updateResponse)
	if !updateResponse.Diagnostics.HasError() || !strings.Contains(updateResponse.Diagnostics.Errors()[0].Detail(), "could not update data-table metadata") {
		t.Fatalf("expected metadata API diagnostic, got %v", updateResponse.Diagnostics)
	}
	var retained dataTableModel
	updateResponse.Diagnostics.Append(updateResponse.State.Get(context.Background(), &retained)...)
	if retained.ID.ValueString() != dataTableTestID {
		t.Fatalf("expected prior identity after failed update, got %#v", retained.ID)
	}

	deleteResponse := &resource.DeleteResponse{State: dataTableState(t, model)}
	implementation.Delete(context.Background(), resource.DeleteRequest{State: dataTableState(t, model)}, deleteResponse)
	if !deleteResponse.Diagnostics.HasError() || !strings.Contains(deleteResponse.Diagnostics.Errors()[0].Detail(), apiFailure.Error()) {
		t.Fatalf("expected delete API diagnostic, got %v", deleteResponse.Diagnostics)
	}
}

func TestDataTableConfigValidatorCrossReferencesAndCollectionElements(t *testing.T) {
	validator := dataTableConfigValidator{}
	invalid := sampleDataTableModel(map[string]attr.Value{
		"primary": dataTableAttributeValue(true, types.StringNull()),
	}, map[string]attr.Value{"primary": types.StringValue("invalid"), "missing": types.StringValue("invalid")})
	response := &resource.ValidateConfigResponse{}
	validator.ValidateResource(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(dataTableState(t, invalid))}, response)
	if len(response.Diagnostics.Errors()) != 2 {
		t.Fatalf("expected primary and undeclared DEFAULT diagnostics, got %v", response.Diagnostics)
	}

	nullElements := sampleDataTableModel(nil, map[string]attr.Value{"value": types.StringNull()})
	nullElements.Attributes = types.MapValueMust(types.ObjectType{AttrTypes: dataTableAttributeTypes}, map[string]attr.Value{"value": types.ObjectNull(dataTableAttributeTypes)})
	response = &resource.ValidateConfigResponse{}
	validator.ValidateResource(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(dataTableState(t, nullElements))}, response)
	if len(response.Diagnostics.Errors()) != 2 {
		t.Fatalf("expected null attribute and DEFAULT diagnostics, got %v", response.Diagnostics)
	}

	unknownElements := sampleDataTableModel(nil, map[string]attr.Value{"value": types.StringUnknown()})
	unknownElements.Attributes = types.MapValueMust(types.ObjectType{AttrTypes: dataTableAttributeTypes}, map[string]attr.Value{"value": types.ObjectUnknown(dataTableAttributeTypes)})
	response = &resource.ValidateConfigResponse{}
	validator.ValidateResource(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(dataTableState(t, unknownElements))}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unknown plan-time elements must remain deferred: %v", response.Diagnostics)
	}
}

func TestDataTableNotFoundImportAndCoordinator(t *testing.T) {
	if !isDataTableNotFound(&connecttypes.ResourceNotFoundException{}) || isDataTableNotFound(errors.New("other")) {
		t.Fatal("unexpected not-found classification")
	}
	implementation, ok := NewDataTableResource().(resource.ResourceWithImportState)
	if !ok {
		t.Fatal("expected importable data-table resource")
	}
	response := &resource.ImportStateResponse{State: dataTableState(t, sampleDataTableModel(nil, nil))}
	implementation.ImportState(context.Background(), resource.ImportStateRequest{ID: dataTableTestInstanceID + ":" + dataTableTestID}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected import diagnostic: %v", response.Diagnostics)
	}
	var imported dataTableModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &imported)...)
	if response.Diagnostics.HasError() || imported.InstanceID.ValueString() != dataTableTestInstanceID || imported.ID.ValueString() != dataTableTestID {
		t.Fatalf("unexpected imported identity %#v diagnostics=%v", imported, response.Diagnostics)
	}
	invalid := &resource.ImportStateResponse{State: dataTableState(t, sampleDataTableModel(nil, nil))}
	implementation.ImportState(context.Background(), resource.ImportStateRequest{ID: "invalid"}, invalid)
	if !invalid.Diagnostics.HasError() {
		t.Fatal("expected invalid import diagnostic")
	}

	coordinator := newDataTableCoordinator()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan struct{}, 2)
	operation := func() error { entered <- struct{}{}; <-release; return context.Canceled }
	key := dataTableKey{instanceID: "i", dataTableID: "same"}
	go func() { _ = coordinator.withLock(key, operation); done <- struct{}{} }()
	go func() { _ = coordinator.withLock(key, operation); done <- struct{}{} }()
	awaitDataTableSignal(t, entered, "first operation")
	select {
	case <-entered:
		t.Fatal("same-table operations overlapped")
	case <-time.After(40 * time.Millisecond):
	}
	release <- struct{}{}
	awaitDataTableSignal(t, entered, "second operation")
	release <- struct{}{}
	awaitDataTableSignal(t, done, "first completion")
	awaitDataTableSignal(t, done, "second completion")
}

func TestDataTableReadAndDeleteTolerateNotFound(t *testing.T) {
	notFound := &connecttypes.ResourceNotFoundException{Message: aws.String("gone")}
	client := &fakeDataTableClient{
		describeTable: func(context.Context, *awsconnect.DescribeDataTableInput) (*awsconnect.DescribeDataTableOutput, error) {
			return nil, notFound
		},
		deleteTable: func(context.Context, *awsconnect.DeleteDataTableInput) (*awsconnect.DeleteDataTableOutput, error) {
			return nil, notFound
		},
	}
	implementation := &dataTableResource{client: client, coordinator: newDataTableCoordinator()}
	state := dataTableState(t, sampleDataTableModel(nil, nil))
	readResponse := &resource.ReadResponse{State: state}
	implementation.Read(context.Background(), resource.ReadRequest{State: state}, readResponse)
	if readResponse.Diagnostics.HasError() || !readResponse.State.Raw.IsNull() {
		t.Fatalf("expected absent table to be removed from state, raw=%v diagnostics=%v", readResponse.State.Raw, readResponse.Diagnostics)
	}
	deleteResponse := &resource.DeleteResponse{State: state}
	implementation.Delete(context.Background(), resource.DeleteRequest{State: state}, deleteResponse)
	if deleteResponse.Diagnostics.HasError() {
		t.Fatalf("delete must tolerate absent table: %v", deleteResponse.Diagnostics)
	}
}

func TestDataTableDescriptionRemovalSendsEmptyString(t *testing.T) {
	model := sampleDataTableModel(nil, nil)
	model.Description = types.StringNull()
	configuration, diagnostics := dataTableConfigurationFromTerraform(context.Background(), model, true)
	if diagnostics.HasError() || configuration.description == nil || aws.ToString(configuration.description) != "" {
		t.Fatalf("expected explicit empty update description, value=%v diagnostics=%v", configuration.description, diagnostics)
	}
}

func TestDataTableCoordinatorAllowsIndependentTables(t *testing.T) {
	coordinator := newDataTableCoordinator()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(2)
	operation := func() error { entered <- struct{}{}; <-release; return context.Canceled }
	go func() {
		defer wait.Done()
		_ = coordinator.withLock(dataTableKey{instanceID: "i", dataTableID: "one"}, operation)
	}()
	go func() {
		defer wait.Done()
		_ = coordinator.withLock(dataTableKey{instanceID: "i", dataTableID: "two"}, operation)
	}()
	awaitDataTableSignal(t, entered, "first independent operation")
	awaitDataTableSignal(t, entered, "second independent operation")
	release <- struct{}{}
	release <- struct{}{}
	wait.Wait()
}

func sampleRemoteDataTable() *connecttypes.DataTable {
	return &connecttypes.DataTable{
		Id: aws.String(dataTableTestID), Arn: aws.String("arn:test"), Name: aws.String("table"), Description: aws.String("description"), TimeZone: aws.String("Australia/Brisbane"),
		ValueLockLevel: connecttypes.DataTableLockLevelValue, Status: connecttypes.DataTableStatusPublished, LastModifiedTime: aws.Time(time.Unix(1, 0)),
	}
}

func dataTableAttributeValue(primary bool, description types.String) types.Object {
	return types.ObjectValueMust(dataTableAttributeTypes, map[string]attr.Value{
		"value_type": types.StringValue("TEXT"), "description": description, "primary": types.BoolValue(primary),
	})
}

func dataTableAttributeValueWithType(valueType string, primary bool) types.Object {
	return types.ObjectValueMust(dataTableAttributeTypes, map[string]attr.Value{
		"value_type": types.StringValue(valueType), "description": types.StringNull(), "primary": types.BoolValue(primary),
	})
}

func sampleDataTableModel(attributes map[string]attr.Value, defaults map[string]attr.Value) dataTableModel {
	if attributes == nil {
		attributes = map[string]attr.Value{}
	}
	attributeMap := types.MapValueMust(types.ObjectType{AttrTypes: dataTableAttributeTypes}, attributes)
	defaultMap := types.MapNull(types.StringType)
	if defaults != nil {
		defaultMap = types.MapValueMust(types.StringType, defaults)
	}
	return dataTableModel{
		InstanceID: types.StringValue(dataTableTestInstanceID), ID: types.StringValue(dataTableTestID), ARN: types.StringValue("arn:test"), Name: types.StringValue("table"),
		Description: types.StringNull(), TimeZone: types.StringValue("Australia/Brisbane"), ValueLockLevel: types.StringValue("VALUE"), Status: types.StringValue("PUBLISHED"),
		Attributes: attributeMap, DefaultValues: defaultMap,
	}
}

func dataTableState(t *testing.T, model dataTableModel) tfsdk.State {
	t.Helper()
	response := &resource.SchemaResponse{}
	NewDataTableResource().Schema(context.Background(), resource.SchemaRequest{}, response)
	rootAttributes := make(map[string]attr.Type, len(response.Schema.Attributes))
	for name, attribute := range response.Schema.Attributes {
		rootAttributes[name] = attribute.GetType()
	}
	var value attr.Value
	diagnostics := tfsdk.ValueFrom(context.Background(), model, types.ObjectType{AttrTypes: rootAttributes}, &value)
	if diagnostics.HasError() {
		t.Fatalf("unexpected model conversion diagnostics: %v", diagnostics)
	}
	raw, err := value.ToTerraformValue(context.Background())
	if err != nil {
		t.Fatalf("unexpected Terraform conversion error: %v", err)
	}
	return tfsdk.State{Raw: raw, Schema: response.Schema}
}

func dataTableSchemaOnlyState(t *testing.T) tfsdk.State {
	t.Helper()
	response := &resource.SchemaResponse{}
	NewDataTableResource().Schema(context.Background(), resource.SchemaRequest{}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", response.Diagnostics)
	}
	return tfsdk.State{Schema: response.Schema}
}

func dataTablePlan(t *testing.T, model dataTableModel) tfsdk.Plan {
	return tfsdk.Plan(dataTableState(t, model))
}

func awaitDataTableSignal(t *testing.T, channel <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
