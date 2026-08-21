package connect

import (
	"context"
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

type fakeDataTableRecordClient struct {
	createValues      func(context.Context, *awsconnect.BatchCreateDataTableValueInput) (*awsconnect.BatchCreateDataTableValueOutput, error)
	updateValues      func(context.Context, *awsconnect.BatchUpdateDataTableValueInput) (*awsconnect.BatchUpdateDataTableValueOutput, error)
	deleteValues      func(context.Context, *awsconnect.BatchDeleteDataTableValueInput) (*awsconnect.BatchDeleteDataTableValueOutput, error)
	listPrimaryValues func(context.Context, *awsconnect.ListDataTablePrimaryValuesInput) (*awsconnect.ListDataTablePrimaryValuesOutput, error)
	listValues        func(context.Context, *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error)
}

func (f *fakeDataTableRecordClient) BatchCreateDataTableValue(ctx context.Context, input *awsconnect.BatchCreateDataTableValueInput, _ ...func(*awsconnect.Options)) (*awsconnect.BatchCreateDataTableValueOutput, error) {
	if f.createValues == nil {
		return &awsconnect.BatchCreateDataTableValueOutput{}, nil
	}
	return f.createValues(ctx, input)
}

func (f *fakeDataTableRecordClient) BatchUpdateDataTableValue(ctx context.Context, input *awsconnect.BatchUpdateDataTableValueInput, _ ...func(*awsconnect.Options)) (*awsconnect.BatchUpdateDataTableValueOutput, error) {
	if f.updateValues == nil {
		return &awsconnect.BatchUpdateDataTableValueOutput{}, nil
	}
	return f.updateValues(ctx, input)
}

func (f *fakeDataTableRecordClient) BatchDeleteDataTableValue(ctx context.Context, input *awsconnect.BatchDeleteDataTableValueInput, _ ...func(*awsconnect.Options)) (*awsconnect.BatchDeleteDataTableValueOutput, error) {
	if f.deleteValues == nil {
		return &awsconnect.BatchDeleteDataTableValueOutput{}, nil
	}
	return f.deleteValues(ctx, input)
}

func (f *fakeDataTableRecordClient) ListDataTablePrimaryValues(ctx context.Context, input *awsconnect.ListDataTablePrimaryValuesInput, _ ...func(*awsconnect.Options)) (*awsconnect.ListDataTablePrimaryValuesOutput, error) {
	if f.listPrimaryValues == nil {
		return &awsconnect.ListDataTablePrimaryValuesOutput{}, nil
	}
	return f.listPrimaryValues(ctx, input)
}

func (f *fakeDataTableRecordClient) ListDataTableValues(ctx context.Context, input *awsconnect.ListDataTableValuesInput, _ ...func(*awsconnect.Options)) (*awsconnect.ListDataTableValuesOutput, error) {
	if f.listValues == nil {
		return &awsconnect.ListDataTableValuesOutput{}, nil
	}
	return f.listValues(ctx, input)
}

func TestDataTableRecordSchemaFactoriesAndImport(t *testing.T) {
	implementation := NewDataTableRecordResource()
	metadata := &resource.MetadataResponse{}
	implementation.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "awscontrib"}, metadata)
	if metadata.TypeName != "awscontrib_connect_data_table_record" {
		t.Fatalf("unexpected type name %q", metadata.TypeName)
	}
	if _, importable := implementation.(resource.ResourceWithImportState); !importable {
		t.Fatal("expected data-table records to expose import")
	}
	response := &resource.SchemaResponse{}
	implementation.Schema(context.Background(), resource.SchemaRequest{}, response)
	for _, name := range []string{"instance_id", "data_table_id"} {
		attribute, ok := response.Schema.Attributes[name].(resourceschema.StringAttribute)
		if !ok || !attribute.Required || len(attribute.PlanModifiers) != 1 {
			t.Fatalf("expected required replacement-only %s, got %#v", name, response.Schema.Attributes[name])
		}
	}
	primary, ok := response.Schema.Attributes["primary_values"].(resourceschema.MapAttribute)
	if !ok || !primary.Required || len(primary.PlanModifiers) != 1 || !primary.ElementType.Equal(types.StringType) {
		t.Fatalf("unexpected primary_values schema %#v", response.Schema.Attributes["primary_values"])
	}
	values, ok := response.Schema.Attributes["values"].(resourceschema.MapAttribute)
	if !ok || !values.Required || len(values.PlanModifiers) != 0 || !values.ElementType.Equal(types.StringType) {
		t.Fatalf("unexpected values schema %#v", response.Schema.Attributes["values"])
	}
	recordID, ok := response.Schema.Attributes["record_id"].(resourceschema.StringAttribute)
	if !ok || !recordID.Computed {
		t.Fatalf("unexpected record_id schema %#v", response.Schema.Attributes["record_id"])
	}

	tableFactory, recordFactory := DataTableResourceFactories()
	table := mustDataTableResource(t, tableFactory())
	record := mustDataTableRecordResource(t, recordFactory())
	secondRecord := mustDataTableRecordResource(t, recordFactory())
	otherTableFactory, otherRecordFactory := DataTableResourceFactories()
	otherTable := mustDataTableResource(t, otherTableFactory())
	otherRecord := mustDataTableRecordResource(t, otherRecordFactory())
	if table.coordinator != record.coordinator || record.coordinator != secondRecord.coordinator {
		t.Fatal("table and records from one provider factory must share the table coordinator")
	}
	if otherTable.coordinator != otherRecord.coordinator {
		t.Fatal("second provider factory must share its own coordinator")
	}
	if record.coordinator == otherRecord.coordinator {
		t.Fatal("different provider factories must not share coordinators")
	}
}

func TestParseDataTableRecordImportIDValidatesComponents(t *testing.T) {
	identity, err := parseDataTableRecordImportID("import-instance:import-table:import-record")
	if err != nil {
		t.Fatalf("unexpected import error: %v", err)
	}
	if identity.instanceID != "import-instance" || identity.dataTableID != "import-table" || identity.recordID != "import-record" {
		t.Fatalf("unexpected parsed identity: %#v", identity)
	}

	for _, invalid := range []string{
		"",
		"instance",
		"instance:table",
		"instance::record",
		"instance:table:",
		":table:record",
		"instance:table:record:extra",
		"instance:table:DEFAULT",
	} {
		if _, err := parseDataTableRecordImportID(invalid); err == nil {
			t.Errorf("expected import error for %q", invalid)
		}
	}
}

func TestDataTableRecordImportStateSetsIdentity(t *testing.T) {
	implementation, ok := NewDataTableRecordResource().(resource.ResourceWithImportState)
	if !ok {
		t.Fatal("expected importable data-table record resource")
	}
	model := sampleDataTableRecordModel(nil, nil)
	model.PrimaryValues = types.MapNull(types.StringType)
	model.Values = types.MapNull(types.StringType)
	model.RecordID = types.StringNull()
	response := &resource.ImportStateResponse{State: dataTableRecordState(t, model)}
	implementation.ImportState(context.Background(), resource.ImportStateRequest{ID: "import-instance:import-table:import-record"}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected import diagnostics: %v", response.Diagnostics)
	}
	var imported dataTableRecordModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &imported)...)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected imported state diagnostics: %v", response.Diagnostics)
	}
	if imported.InstanceID.ValueString() != "import-instance" || imported.DataTableID.ValueString() != "import-table" || imported.RecordID.ValueString() != "import-record" {
		t.Fatalf("unexpected imported identity: %#v", imported)
	}
	if !imported.PrimaryValues.IsNull() || !imported.Values.IsNull() {
		t.Fatalf("import should leave reconstructed maps unset: primary=%v values=%v", imported.PrimaryValues, imported.Values)
	}
}

func TestDataTableRecordCompositePrimaryValuesAreCanonical(t *testing.T) {
	primary := map[string]string{"zeta": "last", "alpha": "first"}
	values := dataTableRecordCreateValues(primary, map[string]string{"two": "2", "one": "1"})
	if got := []string{aws.ToString(values[0].AttributeName), aws.ToString(values[1].AttributeName)}; !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatalf("record values were not sorted: %v", got)
	}
	for _, value := range values {
		got := []string{aws.ToString(value.PrimaryValues[0].AttributeName), aws.ToString(value.PrimaryValues[1].AttributeName)}
		if !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
			t.Fatalf("primary values were not sorted: %v", got)
		}
	}
	filters := dataTableRecordPrimaryFilters(primary)
	if got := []string{aws.ToString(filters[0].AttributeName), aws.ToString(filters[1].AttributeName)}; !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("primary filters were not sorted: %v", got)
	}
}

func TestDataTableRecordMappingRejectsEmptyUnknownAndNullMaps(t *testing.T) {
	model := sampleDataTableRecordModel(map[string]attr.Value{}, map[string]attr.Value{})
	_, diagnostics := dataTableRecordConfigurationFromTerraform(model)
	if len(diagnostics.Errors()) != 2 {
		t.Fatalf("expected two empty-map diagnostics, got %v", diagnostics)
	}
	model = sampleDataTableRecordModel(map[string]attr.Value{"key": types.StringUnknown()}, map[string]attr.Value{"value": types.StringNull()})
	_, diagnostics = dataTableRecordConfigurationFromTerraform(model)
	if len(diagnostics.Errors()) != 2 {
		t.Fatalf("expected unknown and null element diagnostics, got %v", diagnostics)
	}
	model.PrimaryValues = types.MapUnknown(types.StringType)
	model.Values = types.MapNull(types.StringType)
	_, diagnostics = dataTableRecordConfigurationFromTerraform(model)
	if len(diagnostics.Errors()) != 2 {
		t.Fatalf("expected unknown and null map diagnostics, got %v", diagnostics)
	}
	model = sampleDataTableRecordModel(map[string]attr.Value{"same": types.StringValue("primary")}, map[string]attr.Value{"same": types.StringValue("cell")})
	_, diagnostics = dataTableRecordConfigurationFromTerraform(model)
	if len(diagnostics.Errors()) != 1 || !strings.Contains(diagnostics.Errors()[0].Summary(), "Primary Attribute") {
		t.Fatalf("expected overlapping primary/value diagnostic, got %v", diagnostics)
	}
}

func TestDataTableRecordConfigValidatorDefersUnknownsAndRejectsKnownInvalidMaps(t *testing.T) {
	validator := dataTableRecordConfigValidator{}
	model := sampleDataTableRecordModel(map[string]attr.Value{}, map[string]attr.Value{})
	response := &resource.ValidateConfigResponse{}
	validator.ValidateResource(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(dataTableRecordState(t, model))}, response)
	if len(response.Diagnostics.Errors()) != 2 {
		t.Fatalf("expected both empty-map diagnostics, got %v", response.Diagnostics)
	}

	model.PrimaryValues = types.MapUnknown(types.StringType)
	model.Values = types.MapValueMust(types.StringType, map[string]attr.Value{"cell": types.StringUnknown()})
	response = &resource.ValidateConfigResponse{}
	validator.ValidateResource(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(dataTableRecordState(t, model))}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unknown plan values must remain deferred: %v", response.Diagnostics)
	}
}

func TestDataTableRecordReadPaginatesExactMatchAndSurfacesWholeRecordDrift(t *testing.T) {
	primaryPage := 0
	valuePage := 0
	client := &fakeDataTableRecordClient{
		listPrimaryValues: func(_ context.Context, input *awsconnect.ListDataTablePrimaryValuesInput) (*awsconnect.ListDataTablePrimaryValuesOutput, error) {
			primaryPage++
			if input.MaxResults == nil || aws.ToInt32(input.MaxResults) != maxDataTablePrimaryValuesPerPage {
				t.Fatalf("expected primary-value page size %d, got %#v", maxDataTablePrimaryValuesPerPage, input.MaxResults)
			}
			if primaryPage == 1 {
				got := aws.ToString(input.PrimaryAttributeValues[0].AttributeName)
				if input.NextToken != nil || got != "first" {
					t.Fatalf("unexpected first primary request %#v", input)
				}
				return &awsconnect.ListDataTablePrimaryValuesOutput{
					PrimaryValuesList: []connecttypes.RecordPrimaryValue{{RecordId: aws.String("near"), PrimaryValues: []connecttypes.PrimaryValueResponse{{AttributeName: aws.String("first"), Value: aws.String("a")}}}},
					NextToken:         aws.String("primary-next"),
				}, nil
			}
			if aws.ToString(input.NextToken) != "primary-next" {
				t.Fatalf("pagination token not forwarded: %#v", input.NextToken)
			}
			return &awsconnect.ListDataTablePrimaryValuesOutput{PrimaryValuesList: []connecttypes.RecordPrimaryValue{{
				RecordId: aws.String("record-1"), PrimaryValues: []connecttypes.PrimaryValueResponse{
					{AttributeName: aws.String("second"), Value: aws.String("b")}, {AttributeName: aws.String("first"), Value: aws.String("a")},
				},
			}}}, nil
		},
		listValues: func(_ context.Context, input *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error) {
			valuePage++
			if input.MaxResults == nil || aws.ToInt32(input.MaxResults) != maxDataTableValuesPerPage {
				t.Fatalf("expected record-value page size %d, got %#v", maxDataTableValuesPerPage, input.MaxResults)
			}
			if !reflect.DeepEqual(input.RecordIds, []string{"record-1"}) {
				t.Fatalf("record filter not forwarded: %v", input.RecordIds)
			}
			if valuePage == 1 {
				return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{
					{RecordId: aws.String("record-1"), AttributeName: aws.String("managed"), Value: aws.String("remote")},
					{RecordId: aws.String("other"), AttributeName: aws.String("ignored"), Value: aws.String("ignored")},
				}, NextToken: aws.String("value-next")}, nil
			}
			if aws.ToString(input.NextToken) != "value-next" {
				t.Fatalf("value pagination token not forwarded: %#v", input.NextToken)
			}
			return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{
				{RecordId: aws.String("record-1"), AttributeName: aws.String("external"), Value: aws.String("drift")},
			}}, nil
		},
	}
	model := sampleDataTableRecordModel(
		map[string]attr.Value{"second": types.StringValue("b"), "first": types.StringValue("a")},
		map[string]attr.Value{"managed": types.StringValue("planned")},
	)
	implementation := &dataTableRecordResource{client: client, coordinator: newDataTableCoordinator()}
	response := &resource.ReadResponse{State: dataTableRecordState(t, model)}
	implementation.Read(context.Background(), resource.ReadRequest{State: dataTableRecordState(t, model)}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected read diagnostics: %v", response.Diagnostics)
	}
	var refreshed dataTableRecordModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &refreshed)...)
	if response.Diagnostics.HasError() || refreshed.RecordID.ValueString() != "record-1" || len(refreshed.Values.Elements()) != 2 {
		t.Fatalf("unexpected authoritative state %#v diagnostics=%v", refreshed, response.Diagnostics)
	}
	external, ok := refreshed.Values.Elements()["external"].(types.String)
	if !ok || external.ValueString() != "drift" {
		t.Fatalf("external remote cell was not surfaced: %#v", refreshed.Values)
	}
}

func TestDataTableRecordImportedReadReconstructsPrimaryValuesAndRefreshesRecord(t *testing.T) {
	primaryCalls := 0
	valueCalls := 0
	client := &fakeDataTableRecordClient{
		listPrimaryValues: func(_ context.Context, input *awsconnect.ListDataTablePrimaryValuesInput) (*awsconnect.ListDataTablePrimaryValuesOutput, error) {
			primaryCalls++
			if input.MaxResults == nil || aws.ToInt32(input.MaxResults) != maxDataTablePrimaryValuesPerPage {
				t.Fatalf("expected imported primary-value page size %d, got %#v", maxDataTablePrimaryValuesPerPage, input.MaxResults)
			}
			if !reflect.DeepEqual(input.RecordIds, []string{"record-imported"}) || len(input.PrimaryAttributeValues) != 0 {
				t.Fatalf("unexpected imported primary-value request: %#v", input)
			}
			if primaryCalls == 1 {
				return &awsconnect.ListDataTablePrimaryValuesOutput{
					PrimaryValuesList: []connecttypes.RecordPrimaryValue{{RecordId: aws.String("other-record"), PrimaryValues: []connecttypes.PrimaryValueResponse{{AttributeName: aws.String("ignored"), Value: aws.String("ignored")}}}},
					NextToken:         aws.String("primary-next"),
				}, nil
			}
			if aws.ToString(input.NextToken) != "primary-next" {
				t.Fatalf("imported primary pagination token not forwarded: %v", input.NextToken)
			}
			return &awsconnect.ListDataTablePrimaryValuesOutput{PrimaryValuesList: []connecttypes.RecordPrimaryValue{{
				RecordId: aws.String("record-imported"), PrimaryValues: []connecttypes.PrimaryValueResponse{
					{AttributeName: aws.String("second"), Value: aws.String("b")}, {AttributeName: aws.String("first"), Value: aws.String("a")},
				},
			}}}, nil
		},
		listValues: func(_ context.Context, input *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error) {
			valueCalls++
			if input.MaxResults == nil || aws.ToInt32(input.MaxResults) != maxDataTableValuesPerPage {
				t.Fatalf("expected imported record-value page size %d, got %#v", maxDataTableValuesPerPage, input.MaxResults)
			}
			if !reflect.DeepEqual(input.RecordIds, []string{"record-imported"}) {
				t.Fatalf("imported record filter not forwarded: %v", input.RecordIds)
			}
			if valueCalls == 1 {
				return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{{
					RecordId: aws.String("record-imported"), AttributeName: aws.String("first-value"), Value: aws.String("one"),
				}}, NextToken: aws.String("value-next")}, nil
			}
			if aws.ToString(input.NextToken) != "value-next" {
				t.Fatalf("imported value pagination token not forwarded: %v", input.NextToken)
			}
			return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{
				{RecordId: aws.String("record-imported"), AttributeName: aws.String("second-value"), Value: aws.String("two")},
				{RecordId: aws.String("other-record"), AttributeName: aws.String("ignored"), Value: aws.String("ignored")},
			}}, nil
		},
	}
	implementation := &dataTableRecordResource{client: client, coordinator: newDataTableCoordinator()}
	model := sampleDataTableRecordModel(nil, nil)
	model.PrimaryValues = types.MapNull(types.StringType)
	model.Values = types.MapNull(types.StringType)
	model.RecordID = types.StringNull()
	importResponse := &resource.ImportStateResponse{State: dataTableRecordState(t, model)}
	implementation.ImportState(context.Background(), resource.ImportStateRequest{ID: "import-instance:import-table:record-imported"}, importResponse)
	if importResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected import diagnostics: %v", importResponse.Diagnostics)
	}
	readResponse := &resource.ReadResponse{State: importResponse.State}
	implementation.Read(context.Background(), resource.ReadRequest{State: importResponse.State}, readResponse)
	if readResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected imported read diagnostics: %v", readResponse.Diagnostics)
	}
	var refreshed dataTableRecordModel
	readResponse.Diagnostics.Append(readResponse.State.Get(context.Background(), &refreshed)...)
	if readResponse.Diagnostics.HasError() || primaryCalls != 2 || valueCalls != 2 {
		t.Fatalf("unexpected imported read state or pagination: %#v calls=%d/%d diagnostics=%v", refreshed, primaryCalls, valueCalls, readResponse.Diagnostics)
	}
	if refreshed.InstanceID.ValueString() != "import-instance" || refreshed.DataTableID.ValueString() != "import-table" || refreshed.RecordID.ValueString() != "record-imported" {
		t.Fatalf("imported identity was not retained: %#v", refreshed)
	}
	primary, ok := refreshed.PrimaryValues.Elements()["first"].(types.String)
	if !ok || primary.ValueString() != "a" {
		t.Fatalf("primary values were not reconstructed: %#v", refreshed.PrimaryValues)
	}
	value, ok := refreshed.Values.Elements()["second-value"].(types.String)
	if !ok || value.ValueString() != "two" || len(refreshed.Values.Elements()) != 2 {
		t.Fatalf("authoritative values were not refreshed: %#v", refreshed.Values)
	}
}

func TestDataTableRecordImportedReadRemovesMissingAndRejectsDuplicatePrimaryResponses(t *testing.T) {
	model := sampleDataTableRecordModel(nil, nil)
	model.PrimaryValues = types.MapNull(types.StringType)
	model.Values = types.MapNull(types.StringType)
	model.RecordID = types.StringNull()

	missing := &dataTableRecordResource{client: &fakeDataTableRecordClient{}, coordinator: newDataTableCoordinator()}
	missingImport := &resource.ImportStateResponse{State: dataTableRecordState(t, model)}
	missing.ImportState(context.Background(), resource.ImportStateRequest{ID: "import-instance:import-table:missing-record"}, missingImport)
	missingRead := &resource.ReadResponse{State: missingImport.State}
	missing.Read(context.Background(), resource.ReadRequest{State: missingImport.State}, missingRead)
	if missingRead.Diagnostics.HasError() || !missingRead.State.Raw.IsNull() {
		t.Fatalf("missing imported record was not removed: raw=%v diagnostics=%v", missingRead.State.Raw, missingRead.Diagnostics)
	}

	duplicate := &dataTableRecordResource{client: &fakeDataTableRecordClient{listPrimaryValues: func(_ context.Context, input *awsconnect.ListDataTablePrimaryValuesInput) (*awsconnect.ListDataTablePrimaryValuesOutput, error) {
		if !reflect.DeepEqual(input.RecordIds, []string{"duplicate-record"}) {
			t.Fatalf("duplicate test did not use record filter: %v", input.RecordIds)
		}
		primary := []connecttypes.PrimaryValueResponse{{AttributeName: aws.String("key"), Value: aws.String("value")}}
		return &awsconnect.ListDataTablePrimaryValuesOutput{PrimaryValuesList: []connecttypes.RecordPrimaryValue{
			{RecordId: aws.String("duplicate-record"), PrimaryValues: primary}, {RecordId: aws.String("duplicate-record"), PrimaryValues: primary},
		}}, nil
	}}, coordinator: newDataTableCoordinator()}
	duplicateImport := &resource.ImportStateResponse{State: dataTableRecordState(t, model)}
	duplicate.ImportState(context.Background(), resource.ImportStateRequest{ID: "import-instance:import-table:duplicate-record"}, duplicateImport)
	duplicateRead := &resource.ReadResponse{State: duplicateImport.State}
	duplicate.Read(context.Background(), resource.ReadRequest{State: duplicateImport.State}, duplicateRead)
	if !duplicateRead.Diagnostics.HasError() || !strings.Contains(duplicateRead.Diagnostics.Errors()[0].Detail(), "duplicate primary-value responses") {
		t.Fatalf("expected duplicate imported primary response diagnostic, got %v", duplicateRead.Diagnostics)
	}
}

func TestDataTableRecordReadRemovesAbsentAndRejectsMultipleExactMatches(t *testing.T) {
	model := sampleDataTableRecordModel(map[string]attr.Value{"key": types.StringValue("value")}, map[string]attr.Value{"cell": types.StringValue("value")})
	absent := &dataTableRecordResource{client: &fakeDataTableRecordClient{}, coordinator: newDataTableCoordinator()}
	response := &resource.ReadResponse{State: dataTableRecordState(t, model)}
	absent.Read(context.Background(), resource.ReadRequest{State: dataTableRecordState(t, model)}, response)
	if response.Diagnostics.HasError() || !response.State.Raw.IsNull() {
		t.Fatalf("absent record was not removed: raw=%v diagnostics=%v", response.State.Raw, response.Diagnostics)
	}

	duplicate := &dataTableRecordResource{client: &fakeDataTableRecordClient{listPrimaryValues: func(context.Context, *awsconnect.ListDataTablePrimaryValuesInput) (*awsconnect.ListDataTablePrimaryValuesOutput, error) {
		primary := []connecttypes.PrimaryValueResponse{{AttributeName: aws.String("key"), Value: aws.String("value")}}
		return &awsconnect.ListDataTablePrimaryValuesOutput{PrimaryValuesList: []connecttypes.RecordPrimaryValue{
			{RecordId: aws.String("record-a"), PrimaryValues: primary}, {RecordId: aws.String("record-b"), PrimaryValues: primary},
		}}, nil
	}}, coordinator: newDataTableCoordinator()}
	response = &resource.ReadResponse{State: dataTableRecordState(t, model)}
	duplicate.Read(context.Background(), resource.ReadRequest{State: dataTableRecordState(t, model)}, response)
	if !response.Diagnostics.HasError() || !strings.Contains(response.Diagnostics.Errors()[0].Detail(), "multiple records") {
		t.Fatalf("expected multiple exact-match diagnostic, got %v", response.Diagnostics)
	}
}

func TestDataTableRecordCreatePersistsRecoverablePartialIdentity(t *testing.T) {
	client := &fakeDataTableRecordClient{createValues: func(_ context.Context, input *awsconnect.BatchCreateDataTableValueInput) (*awsconnect.BatchCreateDataTableValueOutput, error) {
		if got := []string{aws.ToString(input.Values[0].AttributeName), aws.ToString(input.Values[1].AttributeName)}; !reflect.DeepEqual(got, []string{"first", "second"}) {
			t.Fatalf("create values were not deterministic: %v", got)
		}
		return &awsconnect.BatchCreateDataTableValueOutput{
			Successful: []connecttypes.BatchCreateDataTableValueSuccessResult{{AttributeName: aws.String("first"), RecordId: aws.String("record-partial"), PrimaryValues: input.Values[0].PrimaryValues}},
			Failed:     []connecttypes.BatchCreateDataTableValueFailureResult{{AttributeName: aws.String("second"), Message: aws.String("invalid")}},
		}, nil
	}}
	model := sampleDataTableRecordModel(map[string]attr.Value{"key": types.StringValue("value")}, map[string]attr.Value{
		"second": types.StringValue("2"), "first": types.StringValue("1"),
	})
	implementation := &dataTableRecordResource{client: client, coordinator: newDataTableCoordinator()}
	response := &resource.CreateResponse{State: dataTableRecordState(t, model)}
	implementation.Create(context.Background(), resource.CreateRequest{Plan: dataTableRecordPlan(t, model)}, response)
	if !response.Diagnostics.HasError() || !strings.Contains(response.Diagnostics.Errors()[0].Detail(), "second: invalid") {
		t.Fatalf("expected actionable partial failure, got %v", response.Diagnostics)
	}
	var retained dataTableRecordModel
	response.Diagnostics = nil
	response.Diagnostics.Append(response.State.Get(context.Background(), &retained)...)
	if response.Diagnostics.HasError() || retained.RecordID.ValueString() != "record-partial" || len(retained.Values.Elements()) != 1 {
		t.Fatalf("partial identity was not recoverable: %#v diagnostics=%v", retained, response.Diagnostics)
	}
}

func TestDataTableRecordCreateRefreshesSuccessfulRecord(t *testing.T) {
	client := &fakeDataTableRecordClient{
		createValues: func(_ context.Context, input *awsconnect.BatchCreateDataTableValueInput) (*awsconnect.BatchCreateDataTableValueOutput, error) {
			return &awsconnect.BatchCreateDataTableValueOutput{Successful: []connecttypes.BatchCreateDataTableValueSuccessResult{
				{AttributeName: input.Values[0].AttributeName, RecordId: aws.String("record-created"), PrimaryValues: input.Values[0].PrimaryValues},
				{AttributeName: input.Values[1].AttributeName, RecordId: aws.String("record-created"), PrimaryValues: input.Values[1].PrimaryValues},
			}}, nil
		},
		listValues: func(_ context.Context, input *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error) {
			if input.MaxResults == nil || aws.ToInt32(input.MaxResults) != maxDataTableValuesPerPage {
				t.Fatalf("expected created record-value page size %d, got %#v", maxDataTableValuesPerPage, input.MaxResults)
			}
			if !reflect.DeepEqual(input.RecordIds, []string{"record-created"}) {
				t.Fatalf("create refresh used wrong record filter: %v", input.RecordIds)
			}
			return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{
				{RecordId: aws.String("record-created"), AttributeName: aws.String("first"), Value: aws.String("remote-one")},
				{RecordId: aws.String("record-created"), AttributeName: aws.String("second"), Value: aws.String("remote-two")},
			}}, nil
		},
	}
	model := sampleDataTableRecordModel(map[string]attr.Value{"key": types.StringValue("value")}, map[string]attr.Value{
		"first": types.StringValue("planned-one"), "second": types.StringValue("planned-two"),
	})
	implementation := &dataTableRecordResource{client: client, coordinator: newDataTableCoordinator()}
	response := &resource.CreateResponse{State: dataTableRecordState(t, model)}
	implementation.Create(context.Background(), resource.CreateRequest{Plan: dataTableRecordPlan(t, model)}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected create diagnostics: %v", response.Diagnostics)
	}
	var refreshed dataTableRecordModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &refreshed)...)
	first, ok := refreshed.Values.Elements()["first"].(types.String)
	if response.Diagnostics.HasError() || refreshed.RecordID.ValueString() != "record-created" || !ok || first.ValueString() != "remote-one" {
		t.Fatalf("create did not persist authoritative refresh: %#v diagnostics=%v", refreshed, response.Diagnostics)
	}
}

func TestDataTableRecordRejectsDefaultAndInconsistentCreateIdentity(t *testing.T) {
	primary := map[string]string{"key": "value"}
	primaryAPI := dataTableRecordPrimaryValues(primary)
	requested := map[string]string{"first": "1", "second": "2"}
	_, _, err := dataTableRecordCreateIdentity([]connecttypes.BatchCreateDataTableValueSuccessResult{{AttributeName: aws.String("first"), RecordId: aws.String(defaultDataTableRecordID), PrimaryValues: primaryAPI}}, primary, requested)
	if err == nil || !strings.Contains(err.Error(), "invalid non-default") {
		t.Fatalf("expected DEFAULT identity rejection, got %v", err)
	}
	recordID, _, err := dataTableRecordCreateIdentity([]connecttypes.BatchCreateDataTableValueSuccessResult{
		{AttributeName: aws.String("first"), RecordId: aws.String("one"), PrimaryValues: primaryAPI},
		{AttributeName: aws.String("second"), RecordId: aws.String("two"), PrimaryValues: primaryAPI},
	}, primary, requested)
	if err == nil || !strings.Contains(err.Error(), "inconsistent") || recordID != "" {
		t.Fatalf("expected inconsistent identity rejection without a recoverable ID, id=%q error=%v", recordID, err)
	}
}

func TestDataTableRecordCreateDoesNotPersistInconsistentRecordID(t *testing.T) {
	client := &fakeDataTableRecordClient{createValues: func(_ context.Context, input *awsconnect.BatchCreateDataTableValueInput) (*awsconnect.BatchCreateDataTableValueOutput, error) {
		return &awsconnect.BatchCreateDataTableValueOutput{Successful: []connecttypes.BatchCreateDataTableValueSuccessResult{
			{AttributeName: input.Values[0].AttributeName, RecordId: aws.String("record-one"), PrimaryValues: input.Values[0].PrimaryValues},
			{AttributeName: input.Values[1].AttributeName, RecordId: aws.String("record-two"), PrimaryValues: input.Values[1].PrimaryValues},
		}}, nil
	}}
	model := sampleDataTableRecordModel(map[string]attr.Value{"key": types.StringValue("value")}, map[string]attr.Value{
		"first": types.StringValue("1"), "second": types.StringValue("2"),
	})
	initial := model
	initial.RecordID = types.StringNull()
	implementation := &dataTableRecordResource{client: client, coordinator: newDataTableCoordinator()}
	response := &resource.CreateResponse{State: dataTableRecordState(t, initial)}
	implementation.Create(context.Background(), resource.CreateRequest{Plan: dataTableRecordPlan(t, model)}, response)
	if !response.Diagnostics.HasError() || !strings.Contains(response.Diagnostics.Errors()[0].Detail(), "inconsistent record identifiers") {
		t.Fatalf("expected inconsistent identity diagnostic, got %v", response.Diagnostics)
	}
	var retained dataTableRecordModel
	response.Diagnostics = nil
	response.Diagnostics.Append(response.State.Get(context.Background(), &retained)...)
	if response.Diagnostics.HasError() || !retained.RecordID.IsNull() {
		t.Fatalf("inconsistent create persisted an arbitrary identity: %#v diagnostics=%v", retained.RecordID, response.Diagnostics)
	}
	if !retained.PrimaryValues.Equal(model.PrimaryValues) {
		t.Fatalf("configured primary values were not retained: %#v", retained.PrimaryValues)
	}
}

func TestDataTableRecordCreateRejectsUnclassifiedRequestedValue(t *testing.T) {
	client := &fakeDataTableRecordClient{createValues: func(_ context.Context, input *awsconnect.BatchCreateDataTableValueInput) (*awsconnect.BatchCreateDataTableValueOutput, error) {
		return &awsconnect.BatchCreateDataTableValueOutput{Successful: []connecttypes.BatchCreateDataTableValueSuccessResult{{
			AttributeName: input.Values[0].AttributeName, RecordId: aws.String("record-partial"), PrimaryValues: input.Values[0].PrimaryValues,
		}}}, nil
	}}
	model := sampleDataTableRecordModel(map[string]attr.Value{"key": types.StringValue("value")}, map[string]attr.Value{
		"first": types.StringValue("1"), "second": types.StringValue("2"),
	})
	implementation := &dataTableRecordResource{client: client, coordinator: newDataTableCoordinator()}
	response := &resource.CreateResponse{State: dataTableRecordState(t, model)}
	implementation.Create(context.Background(), resource.CreateRequest{Plan: dataTableRecordPlan(t, model)}, response)
	if !response.Diagnostics.HasError() || !strings.Contains(response.Diagnostics.Errors()[0].Detail(), "1 successful record values for 2 requested values") {
		t.Fatalf("expected unclassified create result diagnostic, got %v", response.Diagnostics)
	}
}

func TestDataTableRecordRejectsRepeatedPaginationTokens(t *testing.T) {
	primaryCalls := 0
	primaryClient := &fakeDataTableRecordClient{listPrimaryValues: func(context.Context, *awsconnect.ListDataTablePrimaryValuesInput) (*awsconnect.ListDataTablePrimaryValuesOutput, error) {
		primaryCalls++
		return &awsconnect.ListDataTablePrimaryValuesOutput{NextToken: aws.String("repeated")}, nil
	}}
	implementation := &dataTableRecordResource{client: primaryClient, coordinator: newDataTableCoordinator()}
	_, err := implementation.findRecordID(context.Background(), dataTableKey{instanceID: "instance", dataTableID: "table"}, map[string]string{"key": "value"})
	if err == nil || !strings.Contains(err.Error(), "repeated data-table primary-value pagination token") || primaryCalls != 2 {
		t.Fatalf("expected repeated primary token rejection after two calls, calls=%d error=%v", primaryCalls, err)
	}

	valueCalls := 0
	valueClient := &fakeDataTableRecordClient{listValues: func(context.Context, *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error) {
		valueCalls++
		return &awsconnect.ListDataTableValuesOutput{
			Values:    []connecttypes.DataTableValueSummary{{RecordId: aws.String("other-record"), AttributeName: aws.String("ignored"), Value: aws.String("ignored")}},
			NextToken: aws.String("repeated"),
		}, nil
	}}
	implementation.client = valueClient
	_, err = implementation.readRemoteRecordByID(context.Background(), dataTableKey{instanceID: "instance", dataTableID: "table"}, "record")
	if err == nil || !strings.Contains(err.Error(), "repeated data-table value pagination token") || valueCalls != 2 {
		t.Fatalf("expected repeated value token rejection after two calls, calls=%d error=%v", valueCalls, err)
	}

	primaryByIDCalls := 0
	implementation.client = &fakeDataTableRecordClient{listPrimaryValues: func(_ context.Context, input *awsconnect.ListDataTablePrimaryValuesInput) (*awsconnect.ListDataTablePrimaryValuesOutput, error) {
		primaryByIDCalls++
		if !reflect.DeepEqual(input.RecordIds, []string{"record"}) {
			t.Fatalf("record ID filter was not forwarded: %v", input.RecordIds)
		}
		return &awsconnect.ListDataTablePrimaryValuesOutput{NextToken: aws.String("repeated")}, nil
	}}
	_, err = implementation.findPrimaryValuesByID(context.Background(), dataTableKey{instanceID: "instance", dataTableID: "table"}, "record")
	if err == nil || !strings.Contains(err.Error(), "repeated data-table primary-value pagination token") || primaryByIDCalls != 2 {
		t.Fatalf("expected repeated imported primary token rejection after two calls, calls=%d error=%v", primaryByIDCalls, err)
	}
}

func TestDataTableRecordUpdateSuccessValidatesPrimaryValuesWithoutRecordIDMetadata(t *testing.T) {
	primary := map[string]string{"first": "a", "second": "b"}
	requested := []connecttypes.DataTableValue{{AttributeName: aws.String("cell")}}
	valid := []connecttypes.BatchUpdateDataTableValueSuccessResult{{
		AttributeName: aws.String("cell"), PrimaryValues: dataTableRecordPrimaryValues(primary),
	}}
	if err := validateDataTableRecordUpdateSuccesses(valid, primary, requested); err != nil {
		t.Fatalf("expected matching update success to pass: %v", err)
	}
	mismatched := []connecttypes.BatchUpdateDataTableValueSuccessResult{{
		AttributeName: aws.String("cell"), PrimaryValues: dataTableRecordPrimaryValues(map[string]string{"first": "a", "second": "other"}),
	}}
	if err := validateDataTableRecordUpdateSuccesses(mismatched, primary, requested); err == nil || !strings.Contains(err.Error(), "different primary values") {
		t.Fatalf("expected mismatched update identity rejection, got %v", err)
	}
	if err := validateDataTableRecordUpdateSuccesses(nil, primary, requested); err == nil || !strings.Contains(err.Error(), "0 successful record updates for 1 requested values") {
		t.Fatalf("expected incomplete update response rejection, got %v", err)
	}
	duplicate := append(valid, valid[0])
	if err := validateDataTableRecordUpdateSuccesses(duplicate, primary, requested); err == nil || !strings.Contains(err.Error(), "duplicate update success") {
		t.Fatalf("expected duplicate update response rejection, got %v", err)
	}
	unexpected := []connecttypes.BatchUpdateDataTableValueSuccessResult{{AttributeName: aws.String("other"), PrimaryValues: dataTableRecordPrimaryValues(primary)}}
	if err := validateDataTableRecordUpdateSuccesses(unexpected, primary, requested); err == nil || !strings.Contains(err.Error(), "unexpected successful update") {
		t.Fatalf("expected unexpected update response rejection, got %v", err)
	}
}

func TestDataTableRecordDeleteSuccessRequiresCompleteMatchingResults(t *testing.T) {
	primary := map[string]string{"key": "value"}
	requested := []connecttypes.DataTableDeleteValueIdentifier{
		{AttributeName: aws.String("first")}, {AttributeName: aws.String("second")},
	}
	valid := []connecttypes.BatchDeleteDataTableValueSuccessResult{
		{AttributeName: aws.String("first"), PrimaryValues: dataTableRecordPrimaryValues(primary)},
		{AttributeName: aws.String("second"), PrimaryValues: dataTableRecordPrimaryValues(primary)},
	}
	if err := validateDataTableRecordDeleteSuccesses(valid, primary, requested); err != nil {
		t.Fatalf("expected complete delete response to pass: %v", err)
	}
	if err := validateDataTableRecordDeleteSuccesses(valid[:1], primary, requested); err == nil || !strings.Contains(err.Error(), "1 successful record deletions for 2 requested values") {
		t.Fatalf("expected incomplete delete response rejection, got %v", err)
	}
	duplicate := []connecttypes.BatchDeleteDataTableValueSuccessResult{valid[0], valid[0]}
	if err := validateDataTableRecordDeleteSuccesses(duplicate, primary, requested); err == nil || !strings.Contains(err.Error(), "duplicate deletion success") {
		t.Fatalf("expected duplicate delete response rejection, got %v", err)
	}
}

func TestDataTableRecordUpdateReconcilesAuthoritativelyWithLocks(t *testing.T) {
	updateLock := &connecttypes.DataTableLockVersion{Value: aws.String("update-lock")}
	deleteLock := &connecttypes.DataTableLockVersion{Value: aws.String("delete-lock")}
	var operations []string
	readCount := 0
	client := &fakeDataTableRecordClient{
		listPrimaryValues: exactPrimaryRecord("record-1", map[string]string{"key": "value"}),
		listValues: func(context.Context, *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error) {
			readCount++
			if readCount == 1 {
				return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{
					{RecordId: aws.String("record-1"), AttributeName: aws.String("change"), Value: aws.String("old"), LockVersion: updateLock},
					{RecordId: aws.String("record-1"), AttributeName: aws.String("remove"), Value: aws.String("old"), LockVersion: deleteLock},
					{RecordId: aws.String("record-1"), AttributeName: aws.String("same"), Value: aws.String("same")},
				}}, nil
			}
			return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{
				{RecordId: aws.String("record-1"), AttributeName: aws.String("add"), Value: aws.String("new")},
				{RecordId: aws.String("record-1"), AttributeName: aws.String("change"), Value: aws.String("new")},
				{RecordId: aws.String("record-1"), AttributeName: aws.String("same"), Value: aws.String("same")},
			}}, nil
		},
		createValues: func(_ context.Context, input *awsconnect.BatchCreateDataTableValueInput) (*awsconnect.BatchCreateDataTableValueOutput, error) {
			operations = append(operations, "create:"+aws.ToString(input.Values[0].AttributeName))
			assertPrimaryValues(t, input.Values[0].PrimaryValues, "key", "value")
			return &awsconnect.BatchCreateDataTableValueOutput{Successful: []connecttypes.BatchCreateDataTableValueSuccessResult{{
				AttributeName: input.Values[0].AttributeName, RecordId: aws.String("record-1"), PrimaryValues: input.Values[0].PrimaryValues,
			}}}, nil
		},
		updateValues: func(_ context.Context, input *awsconnect.BatchUpdateDataTableValueInput) (*awsconnect.BatchUpdateDataTableValueOutput, error) {
			operations = append(operations, "update:"+aws.ToString(input.Values[0].AttributeName))
			if input.Values[0].LockVersion != updateLock {
				t.Fatalf("update did not use refreshed lock: %#v", input.Values[0].LockVersion)
			}
			return &awsconnect.BatchUpdateDataTableValueOutput{Successful: []connecttypes.BatchUpdateDataTableValueSuccessResult{{
				AttributeName: input.Values[0].AttributeName, PrimaryValues: input.Values[0].PrimaryValues,
			}}}, nil
		},
		deleteValues: func(_ context.Context, input *awsconnect.BatchDeleteDataTableValueInput) (*awsconnect.BatchDeleteDataTableValueOutput, error) {
			operations = append(operations, "delete:"+aws.ToString(input.Values[0].AttributeName))
			if input.Values[0].LockVersion != deleteLock {
				t.Fatalf("delete did not use refreshed lock: %#v", input.Values[0].LockVersion)
			}
			return &awsconnect.BatchDeleteDataTableValueOutput{Successful: []connecttypes.BatchDeleteDataTableValueSuccessResult{{
				AttributeName: input.Values[0].AttributeName, PrimaryValues: input.Values[0].PrimaryValues,
			}}}, nil
		},
	}
	prior := sampleDataTableRecordModel(map[string]attr.Value{"key": types.StringValue("value")}, map[string]attr.Value{
		"change": types.StringValue("old"), "remove": types.StringValue("old"), "same": types.StringValue("same"),
	})
	planned := sampleDataTableRecordModel(map[string]attr.Value{"key": types.StringValue("value")}, map[string]attr.Value{
		"add": types.StringValue("new"), "change": types.StringValue("new"), "same": types.StringValue("same"),
	})
	implementation := &dataTableRecordResource{client: client, coordinator: newDataTableCoordinator()}
	response := &resource.UpdateResponse{State: dataTableRecordState(t, prior)}
	implementation.Update(context.Background(), resource.UpdateRequest{State: dataTableRecordState(t, prior), Plan: dataTableRecordPlan(t, planned)}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected update diagnostics: %v", response.Diagnostics)
	}
	if !reflect.DeepEqual(operations, []string{"create:add", "update:change", "delete:remove"}) || readCount != 2 {
		t.Fatalf("unexpected reconciliation operations=%v reads=%d", operations, readCount)
	}
}

func TestDataTableRecordBatchFailuresDoNotAdvancePlannedState(t *testing.T) {
	client := &fakeDataTableRecordClient{
		listPrimaryValues: exactPrimaryRecord("record-1", map[string]string{"key": "value"}),
		listValues: func(context.Context, *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error) {
			return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{
				{RecordId: aws.String("record-1"), AttributeName: aws.String("old"), Value: aws.String("old")},
			}}, nil
		},
		createValues: func(context.Context, *awsconnect.BatchCreateDataTableValueInput) (*awsconnect.BatchCreateDataTableValueOutput, error) {
			return &awsconnect.BatchCreateDataTableValueOutput{Failed: []connecttypes.BatchCreateDataTableValueFailureResult{{AttributeName: aws.String("new"), Message: aws.String("denied")}}}, nil
		},
	}
	prior := sampleDataTableRecordModel(map[string]attr.Value{"key": types.StringValue("value")}, map[string]attr.Value{"old": types.StringValue("old")})
	planned := sampleDataTableRecordModel(map[string]attr.Value{"key": types.StringValue("value")}, map[string]attr.Value{"old": types.StringValue("old"), "new": types.StringValue("new")})
	implementation := &dataTableRecordResource{client: client, coordinator: newDataTableCoordinator()}
	response := &resource.UpdateResponse{State: dataTableRecordState(t, prior)}
	implementation.Update(context.Background(), resource.UpdateRequest{State: dataTableRecordState(t, prior), Plan: dataTableRecordPlan(t, planned)}, response)
	if !response.Diagnostics.HasError() || !strings.Contains(response.Diagnostics.Errors()[0].Detail(), "new: denied") {
		t.Fatalf("expected actionable batch failure, got %v", response.Diagnostics)
	}
	var retained dataTableRecordModel
	response.Diagnostics = nil
	response.Diagnostics.Append(response.State.Get(context.Background(), &retained)...)
	if response.Diagnostics.HasError() || len(retained.Values.Elements()) != 1 {
		t.Fatalf("planned state advanced after failed update: %#v diagnostics=%v", retained, response.Diagnostics)
	}
}

func TestDataTableRecordDeleteUsesAllRemoteCellsAndToleratesAbsence(t *testing.T) {
	lockA := &connecttypes.DataTableLockVersion{Value: aws.String("a")}
	lockB := &connecttypes.DataTableLockVersion{Value: aws.String("b")}
	deleted := false
	client := &fakeDataTableRecordClient{
		listPrimaryValues: exactPrimaryRecord("record-1", map[string]string{"key": "value"}),
		listValues: func(context.Context, *awsconnect.ListDataTableValuesInput) (*awsconnect.ListDataTableValuesOutput, error) {
			return &awsconnect.ListDataTableValuesOutput{Values: []connecttypes.DataTableValueSummary{
				{RecordId: aws.String("record-1"), AttributeName: aws.String("zeta"), Value: aws.String("z"), LockVersion: lockB},
				{RecordId: aws.String("record-1"), AttributeName: aws.String("alpha"), Value: aws.String("a"), LockVersion: lockA},
			}}, nil
		},
		deleteValues: func(_ context.Context, input *awsconnect.BatchDeleteDataTableValueInput) (*awsconnect.BatchDeleteDataTableValueOutput, error) {
			deleted = true
			if got := []string{aws.ToString(input.Values[0].AttributeName), aws.ToString(input.Values[1].AttributeName)}; !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
				t.Fatalf("delete was not deterministic: %v", got)
			}
			if input.Values[0].LockVersion != lockA || input.Values[1].LockVersion != lockB {
				t.Fatal("delete did not preserve per-cell locks")
			}
			return nil, &connecttypes.ResourceNotFoundException{}
		},
	}
	model := sampleDataTableRecordModel(map[string]attr.Value{"key": types.StringValue("value")}, map[string]attr.Value{"alpha": types.StringValue("a")})
	implementation := &dataTableRecordResource{client: client, coordinator: newDataTableCoordinator()}
	response := &resource.DeleteResponse{State: dataTableRecordState(t, model)}
	implementation.Delete(context.Background(), resource.DeleteRequest{State: dataTableRecordState(t, model)}, response)
	if response.Diagnostics.HasError() || !deleted {
		t.Fatalf("delete did not tolerate disappearance: deleted=%t diagnostics=%v", deleted, response.Diagnostics)
	}

	absent := &dataTableRecordResource{client: &fakeDataTableRecordClient{}, coordinator: newDataTableCoordinator()}
	response = &resource.DeleteResponse{State: dataTableRecordState(t, model)}
	absent.Delete(context.Background(), resource.DeleteRequest{State: dataTableRecordState(t, model)}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("absent record delete must be tolerated: %v", response.Diagnostics)
	}
}

func TestDataTableCoordinatorSerializesTableAndRecords(t *testing.T) {
	tableFactory, recordFactory := DataTableResourceFactories()
	coordinator := mustDataTableResource(t, tableFactory()).coordinator
	if coordinator != mustDataTableRecordResource(t, recordFactory()).coordinator {
		t.Fatal("factories did not share coordinator")
	}
	entered := make(chan string, 3)
	release := make(chan struct{})
	done := make(chan struct{}, 3)
	sameKey := dataTableKey{instanceID: "instance", dataTableID: "table"}
	operation := func(name string, key dataTableKey) {
		_ = coordinator.withLock(key, func() error {
			entered <- name
			<-release
			return nil
		})
		done <- struct{}{}
	}
	go operation("table", sameKey)
	awaitDataTableRecordString(t, entered, "first same-table operation")
	go operation("record-one", sameKey)
	go operation("record-two", sameKey)
	select {
	case name := <-entered:
		t.Fatalf("same-table operation %q overlapped", name)
	case <-time.After(40 * time.Millisecond):
	}
	release <- struct{}{}
	awaitDataTableRecordString(t, entered, "second same-table operation")
	release <- struct{}{}
	awaitDataTableRecordString(t, entered, "third same-table operation")
	release <- struct{}{}
	for range 3 {
		awaitDataTableRecordSignal(t, done, "same-table completion")
	}

	var wait sync.WaitGroup
	wait.Add(2)
	go func() { defer wait.Done(); operation("one", dataTableKey{instanceID: "instance", dataTableID: "one"}) }()
	go func() { defer wait.Done(); operation("two", dataTableKey{instanceID: "instance", dataTableID: "two"}) }()
	awaitDataTableRecordString(t, entered, "first independent operation")
	awaitDataTableRecordString(t, entered, "second independent operation")
	release <- struct{}{}
	release <- struct{}{}
	awaitDataTableRecordSignal(t, done, "first independent completion")
	awaitDataTableRecordSignal(t, done, "second independent completion")
	wait.Wait()
}

func exactPrimaryRecord(recordID string, primaryValues map[string]string) func(context.Context, *awsconnect.ListDataTablePrimaryValuesInput) (*awsconnect.ListDataTablePrimaryValuesOutput, error) {
	return func(context.Context, *awsconnect.ListDataTablePrimaryValuesInput) (*awsconnect.ListDataTablePrimaryValuesOutput, error) {
		response := make([]connecttypes.PrimaryValueResponse, 0, len(primaryValues))
		for _, name := range sortedStringMapKeys(primaryValues) {
			response = append(response, connecttypes.PrimaryValueResponse{AttributeName: aws.String(name), Value: aws.String(primaryValues[name])})
		}
		return &awsconnect.ListDataTablePrimaryValuesOutput{PrimaryValuesList: []connecttypes.RecordPrimaryValue{{RecordId: aws.String(recordID), PrimaryValues: response}}}, nil
	}
}

func mustDataTableResource(t *testing.T, implementation resource.Resource) *dataTableResource {
	t.Helper()
	result, ok := implementation.(*dataTableResource)
	if !ok {
		t.Fatalf("expected data-table resource, got %T", implementation)
	}
	return result
}

func mustDataTableRecordResource(t *testing.T, implementation resource.Resource) *dataTableRecordResource {
	t.Helper()
	result, ok := implementation.(*dataTableRecordResource)
	if !ok {
		t.Fatalf("expected data-table record resource, got %T", implementation)
	}
	return result
}

func assertPrimaryValues(t *testing.T, values []connecttypes.PrimaryValue, name, value string) {
	t.Helper()
	if len(values) != 1 || aws.ToString(values[0].AttributeName) != name || aws.ToString(values[0].Value) != value {
		t.Fatalf("unexpected primary values %#v", values)
	}
}

func sampleDataTableRecordModel(primaryValues, values map[string]attr.Value) dataTableRecordModel {
	if primaryValues == nil {
		primaryValues = map[string]attr.Value{}
	}
	if values == nil {
		values = map[string]attr.Value{}
	}
	return dataTableRecordModel{
		InstanceID: types.StringValue(dataTableTestInstanceID), DataTableID: types.StringValue(dataTableTestID),
		PrimaryValues: types.MapValueMust(types.StringType, primaryValues), Values: types.MapValueMust(types.StringType, values),
		RecordID: types.StringValue("record-1"),
	}
}

func dataTableRecordState(t *testing.T, model dataTableRecordModel) tfsdk.State {
	t.Helper()
	response := &resource.SchemaResponse{}
	NewDataTableRecordResource().Schema(context.Background(), resource.SchemaRequest{}, response)
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

func dataTableRecordPlan(t *testing.T, model dataTableRecordModel) tfsdk.Plan {
	return tfsdk.Plan(dataTableRecordState(t, model))
}

func awaitDataTableRecordString(t *testing.T, channel <-chan string, description string) string {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return ""
	}
}

func awaitDataTableRecordSignal(t *testing.T, channel <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
