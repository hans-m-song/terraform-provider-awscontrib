package connect

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/defaults"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	overrideInstanceID       = "instance-for-hours-override"
	overrideHoursID          = "hours-of-operation-id"
	overrideID               = "hours-of-operation-override-id"
	overrideName             = "holiday closure"
	overrideDescription      = "holiday description"
	overrideEffectiveFrom    = "2026-12-24"
	overrideEffectiveTill    = "2026-12-26"
	overrideResourceProvider = "awscontrib"
)

type fakeHoursOfOperationOverrideClient struct {
	create   func(context.Context, *awsconnect.CreateHoursOfOperationOverrideInput) (*awsconnect.CreateHoursOfOperationOverrideOutput, error)
	describe func(context.Context, *awsconnect.DescribeHoursOfOperationOverrideInput) (*awsconnect.DescribeHoursOfOperationOverrideOutput, error)
	update   func(context.Context, *awsconnect.UpdateHoursOfOperationOverrideInput) (*awsconnect.UpdateHoursOfOperationOverrideOutput, error)
	delete   func(context.Context, *awsconnect.DeleteHoursOfOperationOverrideInput) (*awsconnect.DeleteHoursOfOperationOverrideOutput, error)
}

func (f *fakeHoursOfOperationOverrideClient) CreateHoursOfOperationOverride(ctx context.Context, input *awsconnect.CreateHoursOfOperationOverrideInput, _ ...func(*awsconnect.Options)) (*awsconnect.CreateHoursOfOperationOverrideOutput, error) {
	if f.create == nil {
		return &awsconnect.CreateHoursOfOperationOverrideOutput{HoursOfOperationOverrideId: aws.String(overrideID)}, nil
	}
	return f.create(ctx, input)
}

func (f *fakeHoursOfOperationOverrideClient) DescribeHoursOfOperationOverride(ctx context.Context, input *awsconnect.DescribeHoursOfOperationOverrideInput, _ ...func(*awsconnect.Options)) (*awsconnect.DescribeHoursOfOperationOverrideOutput, error) {
	if f.describe == nil {
		return &awsconnect.DescribeHoursOfOperationOverrideOutput{HoursOfOperationOverride: &connecttypes.HoursOfOperationOverride{}}, nil
	}
	return f.describe(ctx, input)
}

func (f *fakeHoursOfOperationOverrideClient) UpdateHoursOfOperationOverride(ctx context.Context, input *awsconnect.UpdateHoursOfOperationOverrideInput, _ ...func(*awsconnect.Options)) (*awsconnect.UpdateHoursOfOperationOverrideOutput, error) {
	if f.update == nil {
		return &awsconnect.UpdateHoursOfOperationOverrideOutput{}, nil
	}
	return f.update(ctx, input)
}

func (f *fakeHoursOfOperationOverrideClient) DeleteHoursOfOperationOverride(ctx context.Context, input *awsconnect.DeleteHoursOfOperationOverrideInput, _ ...func(*awsconnect.Options)) (*awsconnect.DeleteHoursOfOperationOverrideOutput, error) {
	if f.delete == nil {
		return &awsconnect.DeleteHoursOfOperationOverrideOutput{}, nil
	}
	return f.delete(ctx, input)
}

type fakeHoursOfOperationOverrideClientFactory struct{}

func (fakeHoursOfOperationOverrideClientFactory) Connect() *awsconnect.Client {
	return &awsconnect.Client{}
}

func TestHoursOfOperationOverrideMetadataAndSchema(t *testing.T) {
	implementation := NewHoursOfOperationOverrideResource()
	metadata := &resource.MetadataResponse{}
	implementation.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: overrideResourceProvider}, metadata)
	if metadata.TypeName != overrideResourceProvider+"_connect_hours_of_operation_override" {
		t.Fatalf("unexpected resource type name %q", metadata.TypeName)
	}

	schemaResponse := &resource.SchemaResponse{}
	implementation.Schema(context.Background(), resource.SchemaRequest{}, schemaResponse)
	if schemaResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", schemaResponse.Diagnostics)
	}

	for _, name := range []string{"instance_id", "hours_of_operation_id"} {
		attribute, ok := schemaResponse.Schema.Attributes[name].(resourceschema.StringAttribute)
		if !ok || !attribute.Required || len(attribute.PlanModifiers) != 1 {
			t.Fatalf("expected replacement-only required string %s, got %#v", name, schemaResponse.Schema.Attributes[name])
		}
		if name == "instance_id" && len(attribute.Validators) != 1 {
			t.Fatalf("expected %s length validator, got %#v", name, attribute.Validators)
		}
	}
	overrideIDAttribute, ok := schemaResponse.Schema.Attributes["override_id"].(resourceschema.StringAttribute)
	if !ok || !overrideIDAttribute.Computed || overrideIDAttribute.Required || overrideIDAttribute.Optional {
		t.Fatalf("expected computed override_id, got %#v", schemaResponse.Schema.Attributes["override_id"])
	}
	timeWindows, ok := schemaResponse.Schema.Attributes["time_windows"].(resourceschema.SetNestedAttribute)
	if !ok || !timeWindows.Optional || !timeWindows.Computed || timeWindows.Required || timeWindows.Default == nil {
		t.Fatalf("expected optional+computed time_windows set nested attribute with default, got %#v", schemaResponse.Schema.Attributes["time_windows"])
	}
	if len(timeWindows.NestedObject.Attributes) != 3 {
		t.Fatalf("expected exactly day, opens, and closes attributes, got %#v", timeWindows.NestedObject.Attributes)
	}
	for _, name := range []string{"day", "opens", "closes"} {
		attribute, ok := timeWindows.NestedObject.Attributes[name].(resourceschema.StringAttribute)
		if !ok || !attribute.Required || len(attribute.Validators) != 1 {
			t.Fatalf("expected required validated %s string, got %#v", name, timeWindows.NestedObject.Attributes[name])
		}
	}
	defaultResponse := &defaults.SetResponse{}
	timeWindows.Default.DefaultSet(context.Background(), defaults.SetRequest{}, defaultResponse)
	if defaultResponse.PlanValue.IsNull() || defaultResponse.PlanValue.IsUnknown() || len(defaultResponse.PlanValue.Elements()) != 0 {
		t.Fatalf("expected static empty time_windows default, got %#v", defaultResponse.PlanValue)
	}
	recurrence, ok := schemaResponse.Schema.Attributes["recurrence"].(resourceschema.SingleNestedAttribute)
	if !ok || !recurrence.Optional || recurrence.Required || len(recurrence.PlanModifiers) != 1 {
		t.Fatalf("expected optional recurrence with removal modifier, got %#v", schemaResponse.Schema.Attributes["recurrence"])
	}
	description, ok := schemaResponse.Schema.Attributes["description"].(resourceschema.StringAttribute)
	if !ok || !description.Optional || len(description.PlanModifiers) != 1 || len(description.Validators) != 1 {
		t.Fatalf("expected optional description with removal modifier, got %#v", schemaResponse.Schema.Attributes["description"])
	}
	nameAttribute, ok := schemaResponse.Schema.Attributes["name"].(resourceschema.StringAttribute)
	if !ok || !nameAttribute.Required || len(nameAttribute.Validators) != 1 {
		t.Fatalf("expected required name validator, got %#v", schemaResponse.Schema.Attributes["name"])
	}
}

func TestHoursOfOperationOverrideConfigureAcceptsNilAndTypedProviderData(t *testing.T) {
	implementation := NewHoursOfOperationOverrideResource()
	configurable, ok := implementation.(resource.ResourceWithConfigure)
	if !ok {
		t.Fatal("expected configurable resource")
	}
	response := &resource.ConfigureResponse{}
	configurable.Configure(context.Background(), resource.ConfigureRequest{}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected nil provider data diagnostic: %v", response.Diagnostics)
	}
	concrete, ok := implementation.(*hoursOfOperationOverrideResource)
	if !ok {
		t.Fatal("expected hours override implementation")
	}
	response = &resource.ConfigureResponse{}
	configurable.Configure(context.Background(), resource.ConfigureRequest{ProviderData: fakeHoursOfOperationOverrideClientFactory{}}, response)
	if response.Diagnostics.HasError() || concrete.client == nil {
		t.Fatalf("expected typed provider data to configure client, diagnostics=%v client=%v", response.Diagnostics, concrete.client)
	}
	response = &resource.ConfigureResponse{}
	configurable.Configure(context.Background(), resource.ConfigureRequest{ProviderData: "unexpected"}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected unexpected provider data diagnostic")
	}
}

func TestHoursOfOperationOverrideValidators(t *testing.T) {
	validDate := &validator.StringResponse{}
	dateValidator{}.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("effective_from"), ConfigValue: types.StringValue("2026-02-28")}, validDate)
	if validDate.Diagnostics.HasError() {
		t.Fatalf("unexpected valid date diagnostic: %v", validDate.Diagnostics)
	}
	for _, value := range []string{"2026-2-28", "2026-02-30", "not-a-date"} {
		response := &validator.StringResponse{}
		dateValidator{}.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("effective_from"), ConfigValue: types.StringValue(value)}, response)
		if !response.Diagnostics.HasError() {
			t.Errorf("expected invalid date diagnostic for %q", value)
		}
	}

	for _, value := range []int32{0, 23} {
		response := &validator.Int32Response{}
		(int32RangeValidator{attributeName: "hours", min: 0, max: 23}).ValidateInt32(context.Background(), validator.Int32Request{Path: path.Root("hours"), ConfigValue: types.Int32Value(value)}, response)
		if response.Diagnostics.HasError() {
			t.Errorf("unexpected hours diagnostic for %d: %v", value, response.Diagnostics)
		}
	}
	for _, value := range []int32{0, 32} {
		response := &validator.Int32Response{}
		(int32RangeValidator{attributeName: "by_month_day", min: -1, max: 31, excludeZero: true}).ValidateInt32(context.Background(), validator.Int32Request{Path: path.Root("by_month_day"), ConfigValue: types.Int32Value(value)}, response)
		if !response.Diagnostics.HasError() {
			t.Errorf("expected by_month_day diagnostic for %d", value)
		}
	}

	for _, value := range []string{"STANDARD", "OPEN", "CLOSED"} {
		response := &validator.StringResponse{}
		(stringEnumValidator{attributeName: "override_type", allowed: []string{"STANDARD", "OPEN", "CLOSED"}}).ValidateString(context.Background(), validator.StringRequest{Path: path.Root("override_type"), ConfigValue: types.StringValue(value)}, response)
		if response.Diagnostics.HasError() {
			t.Errorf("unexpected enum diagnostic for %q: %v", value, response.Diagnostics)
		}
	}
	response := &validator.StringResponse{}
	(stringEnumValidator{attributeName: "override_type", allowed: []string{"STANDARD", "OPEN", "CLOSED"}}).ValidateString(context.Background(), validator.StringRequest{Path: path.Root("override_type"), ConfigValue: types.StringValue("INVALID")}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected invalid enum diagnostic")
	}

	recurrence := types.ObjectValueMust(hoursOfOperationOverrideRecurrenceAttributeTypes, map[string]attr.Value{
		"frequency":             types.StringValue("MONTHLY"),
		"interval":              types.Int32Value(1),
		"by_month":              types.Int32Null(),
		"by_month_day":          types.Int32Value(1),
		"by_weekday_occurrence": types.Int32Value(2),
	})
	recurrenceResponse := &validator.ObjectResponse{}
	recurrenceMutualExclusionValidator{}.ValidateObject(context.Background(), validator.ObjectRequest{Path: path.Root("recurrence"), ConfigValue: recurrence}, recurrenceResponse)
	if !recurrenceResponse.Diagnostics.HasError() {
		t.Fatal("expected recurrence mutual-exclusion diagnostic")
	}
}

func TestHoursOfOperationOverrideTimeWindowStringValidator(t *testing.T) {
	timeValidator := timeWindowStringValidator{attributeName: "opens"}
	for _, value := range []string{"00:00", "09:05", "23:59"} {
		response := &validator.StringResponse{}
		timeValidator.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("time_windows"), ConfigValue: types.StringValue(value)}, response)
		if response.Diagnostics.HasError() {
			t.Errorf("unexpected diagnostic for valid time %q: %v", value, response.Diagnostics)
		}
	}
	for _, value := range []string{"", "0:00", "00:0", "00:000", "24:00", "23:60", "12:3a", "１２:００", "12：00"} {
		response := &validator.StringResponse{}
		timeValidator.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("time_windows"), ConfigValue: types.StringValue(value)}, response)
		if !response.Diagnostics.HasError() {
			t.Errorf("expected diagnostic for invalid time %q", value)
		}
	}
	for name, value := range map[string]types.String{"null": types.StringNull(), "unknown": types.StringUnknown()} {
		response := &validator.StringResponse{}
		timeValidator.ValidateString(context.Background(), validator.StringRequest{ConfigValue: value}, response)
		if response.Diagnostics.HasError() {
			t.Errorf("unexpected %s time diagnostic: %v", name, response.Diagnostics)
		}
	}
}

func TestHoursOfOperationOverrideTimeWindowCrossFieldValidation(t *testing.T) {
	configValidator := hoursOfOperationOverrideConfigValidator{}
	for _, overrideType := range []string{"STANDARD", "CLOSED"} {
		model := sampleOverrideModel()
		model.OverrideType = types.StringValue(overrideType)
		model.TimeWindows = overrideTimeWindowsValue(t)
		response := &resource.ValidateConfigResponse{}
		configValidator.ValidateResource(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(overrideState(t, model))}, response)
		if response.Diagnostics.HasError() {
			t.Errorf("%s should permit empty time_windows: %v", overrideType, response.Diagnostics)
		}
	}

	open := sampleOverrideModel()
	open.OverrideType = types.StringValue("OPEN")
	open.TimeWindows = overrideTimeWindowsValue(t)
	response := &resource.ValidateConfigResponse{}
	configValidator.ValidateResource(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(overrideState(t, open))}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("OPEN should reject empty time_windows")
	}

	for name, model := range map[string]hoursOfOperationOverrideModel{
		"unknown type": func() hoursOfOperationOverrideModel {
			model := open
			model.OverrideType = types.StringUnknown()
			return model
		}(),
		"unknown windows": func() hoursOfOperationOverrideModel {
			model := open
			model.TimeWindows = types.SetUnknown(types.ObjectType{AttrTypes: hoursOfOperationOverrideTimeWindowAttributeTypes})
			return model
		}(),
	} {
		response = &resource.ValidateConfigResponse{}
		configValidator.ValidateResource(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(overrideState(t, model))}, response)
		if response.Diagnostics.HasError() {
			t.Errorf("expected %s to remain deferred, got %v", name, response.Diagnostics)
		}
	}
}

func TestHoursOfOperationOverrideStringConstraintValidators(t *testing.T) {
	tests := []struct {
		name      string
		validator stringConstraintValidator
		valid     []string
		invalid   []string
	}{
		{
			name:      "instance identifier",
			validator: stringConstraintValidator{attributeName: "instance_id", minLength: 1, maxLength: 100},
			valid:     []string{"x", strings.Repeat("x", 100)},
			invalid:   []string{"", strings.Repeat("x", 101)},
		},
		{
			name:      "name",
			validator: stringConstraintValidator{attributeName: "name", minLength: 1, maxLength: 127, allowLineBreaks: true},
			valid:     []string{"x", strings.Repeat("x", 127), "a\r", "a\n", "a\t"},
			invalid:   []string{"", strings.Repeat("x", 128), "a\x00", "a\u200b"},
		},
		{
			name:      "description",
			validator: stringConstraintValidator{attributeName: "description", minLength: 1, maxLength: 250, allowLineBreaks: true},
			valid:     []string{"x", strings.Repeat("x", 250), "a\r", "a\n", "a\t"},
			invalid:   []string{"", strings.Repeat("x", 251), "a\x00", "a\u200b"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, value := range test.valid {
				response := &validator.StringResponse{}
				test.validator.ValidateString(context.Background(), validator.StringRequest{Path: path.Root(test.name), ConfigValue: types.StringValue(value)}, response)
				if response.Diagnostics.HasError() {
					t.Errorf("unexpected diagnostic for valid value of length %d: %v", utf8.RuneCountInString(value), response.Diagnostics)
				}
			}
			for _, value := range test.invalid {
				response := &validator.StringResponse{}
				test.validator.ValidateString(context.Background(), validator.StringRequest{Path: path.Root(test.name), ConfigValue: types.StringValue(value)}, response)
				if !response.Diagnostics.HasError() {
					t.Errorf("expected diagnostic for invalid value of length %d", utf8.RuneCountInString(value))
				}
			}
			for name, value := range map[string]types.String{"null": types.StringNull(), "unknown": types.StringUnknown()} {
				response := &validator.StringResponse{}
				test.validator.ValidateString(context.Background(), validator.StringRequest{ConfigValue: value}, response)
				if response.Diagnostics.HasError() {
					t.Errorf("unexpected %s diagnostic: %v", name, response.Diagnostics)
				}
			}
		})
	}
}

func TestHoursOfOperationOverrideRemovalPlanModifiers(t *testing.T) {
	description := descriptionRemovalRequiresReplace{}
	if description.Description(context.Background()) == "" || description.MarkdownDescription(context.Background()) == "" {
		t.Fatal("expected description plan modifier descriptions")
	}
	for name, request := range map[string]planmodifier.StringRequest{
		"removal":  {StateValue: types.StringValue("stored"), PlanValue: types.StringNull()},
		"addition": {StateValue: types.StringNull(), PlanValue: types.StringValue("new")},
		"change":   {StateValue: types.StringValue("old"), PlanValue: types.StringValue("new")},
	} {
		response := &planmodifier.StringResponse{}
		description.PlanModifyString(context.Background(), request, response)
		if (name == "removal") != response.RequiresReplace {
			t.Errorf("description %s requires_replace=%v", name, response.RequiresReplace)
		}
	}

	recurrence := sampleRecurrenceValue()
	recurrenceModifier := recurrenceRemovalRequiresReplace{}
	for name, request := range map[string]planmodifier.ObjectRequest{
		"removal":  {StateValue: recurrence, PlanValue: types.ObjectNull(hoursOfOperationOverrideRecurrenceAttributeTypes)},
		"addition": {StateValue: types.ObjectNull(hoursOfOperationOverrideRecurrenceAttributeTypes), PlanValue: recurrence},
		"change":   {StateValue: recurrence, PlanValue: recurrence},
	} {
		response := &planmodifier.ObjectResponse{}
		recurrenceModifier.PlanModifyObject(context.Background(), request, response)
		if (name == "removal") != response.RequiresReplace {
			t.Errorf("recurrence %s requires_replace=%v", name, response.RequiresReplace)
		}
	}
}

func TestHoursOfOperationOverrideTimeWindowMappingNormalizesSetOrder(t *testing.T) {
	first := overrideTimeWindowsValue(t,
		overrideTimeWindowEntry("SUNDAY", "10:00", "11:00"),
		overrideTimeWindowEntry("MONDAY", "09:30", "10:30"),
	)
	second := overrideTimeWindowsValue(t,
		overrideTimeWindowEntry("MONDAY", "09:30", "10:30"),
		overrideTimeWindowEntry("SUNDAY", "10:00", "11:00"),
	)
	firstInput, firstDiagnostics := hoursOfOperationOverrideTimeWindowsFromTerraform(first)
	secondInput, secondDiagnostics := hoursOfOperationOverrideTimeWindowsFromTerraform(second)
	if firstDiagnostics.HasError() || secondDiagnostics.HasError() {
		t.Fatalf("unexpected time window mapping diagnostics: %v / %v", firstDiagnostics, secondDiagnostics)
	}
	if !reflect.DeepEqual(firstInput, secondInput) {
		t.Fatalf("expected order-independent time window mapping, first=%#v second=%#v", firstInput, secondInput)
	}
	if got := string(firstInput[0].Day); got != "MONDAY" {
		t.Fatalf("expected canonical day order, got %q", got)
	}
}

func TestHoursOfOperationOverrideCreateReadUpdateDelete(t *testing.T) {
	var createInput *awsconnect.CreateHoursOfOperationOverrideInput
	var updateInput *awsconnect.UpdateHoursOfOperationOverrideInput
	client := &fakeHoursOfOperationOverrideClient{
		create: func(_ context.Context, input *awsconnect.CreateHoursOfOperationOverrideInput) (*awsconnect.CreateHoursOfOperationOverrideOutput, error) {
			createInput = input
			return &awsconnect.CreateHoursOfOperationOverrideOutput{HoursOfOperationOverrideId: aws.String(overrideID)}, nil
		},
		describe: func(_ context.Context, input *awsconnect.DescribeHoursOfOperationOverrideInput) (*awsconnect.DescribeHoursOfOperationOverrideOutput, error) {
			if aws.ToString(input.InstanceId) != overrideInstanceID || aws.ToString(input.HoursOfOperationId) != overrideHoursID || aws.ToString(input.HoursOfOperationOverrideId) != overrideID {
				t.Fatalf("unexpected describe identity: %#v", input)
			}
			return &awsconnect.DescribeHoursOfOperationOverrideOutput{HoursOfOperationOverride: sampleRemoteOverride()}, nil
		},
		update: func(_ context.Context, input *awsconnect.UpdateHoursOfOperationOverrideInput) (*awsconnect.UpdateHoursOfOperationOverrideOutput, error) {
			updateInput = input
			return &awsconnect.UpdateHoursOfOperationOverrideOutput{}, nil
		},
	}
	implementation := &hoursOfOperationOverrideResource{client: client}

	createModel := sampleOverrideModel()
	createModel.OverrideID = types.StringNull()
	createPlan := overridePlan(t, createModel)
	createResponse := &resource.CreateResponse{State: overrideState(t, createModel)}
	implementation.Create(context.Background(), resource.CreateRequest{Plan: createPlan}, createResponse)
	if createResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected create diagnostics: %v", createResponse.Diagnostics)
	}
	if createInput == nil || aws.ToString(createInput.HoursOfOperationId) != overrideHoursID || aws.ToString(createInput.InstanceId) != overrideInstanceID || aws.ToString(createInput.Name) != overrideName {
		t.Fatalf("unexpected create input: %#v", createInput)
	}
	if aws.ToString(createInput.EffectiveFrom) != overrideEffectiveFrom || aws.ToString(createInput.EffectiveTill) != overrideEffectiveTill || createInput.OverrideType != connecttypes.OverrideTypeStandard {
		t.Fatalf("unexpected create dates or type: %#v", createInput)
	}
	expectedConfig := []connecttypes.HoursOfOperationOverrideConfig{
		{Day: connecttypes.OverrideDaysMonday, StartTime: &connecttypes.OverrideTimeSlice{Hours: aws.Int32(9), Minutes: aws.Int32(30)}, EndTime: &connecttypes.OverrideTimeSlice{Hours: aws.Int32(10), Minutes: aws.Int32(30)}},
		{Day: connecttypes.OverrideDaysSunday, StartTime: &connecttypes.OverrideTimeSlice{Hours: aws.Int32(10), Minutes: aws.Int32(0)}, EndTime: &connecttypes.OverrideTimeSlice{Hours: aws.Int32(11), Minutes: aws.Int32(0)}},
	}
	if !reflect.DeepEqual(createInput.Config, expectedConfig) {
		t.Fatalf("unexpected create config: %#v", createInput.Config)
	}
	if createInput.RecurrenceConfig == nil || createInput.Description == nil {
		t.Fatalf("expected create optionals, got %#v", createInput)
	}
	if aws.ToString(createInput.Description) != overrideDescription {
		t.Fatalf("unexpected create description %q", aws.ToString(createInput.Description))
	}
	if createInput.RecurrenceConfig.RecurrencePattern == nil || createInput.RecurrenceConfig.RecurrencePattern.Frequency != connecttypes.RecurrenceFrequencyYearly || aws.ToInt32(createInput.RecurrenceConfig.RecurrencePattern.Interval) != 1 || !reflect.DeepEqual(createInput.RecurrenceConfig.RecurrencePattern.ByMonth, []int32{12}) || !reflect.DeepEqual(createInput.RecurrenceConfig.RecurrencePattern.ByMonthDay, []int32{24}) || len(createInput.RecurrenceConfig.RecurrencePattern.ByWeekdayOccurrence) != 0 {
		t.Fatalf("unexpected create recurrence: %#v", createInput.RecurrenceConfig)
	}
	var createdState hoursOfOperationOverrideModel
	createResponse.Diagnostics.Append(createResponse.State.Get(context.Background(), &createdState)...)
	if createResponse.Diagnostics.HasError() || createdState.OverrideID.ValueString() != overrideID {
		t.Fatalf("expected created state ID, state=%#v diagnostics=%v", createdState, createResponse.Diagnostics)
	}

	readResponse := &resource.ReadResponse{State: createResponse.State}
	implementation.Read(context.Background(), resource.ReadRequest{State: createResponse.State}, readResponse)
	if readResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected read diagnostics: %v", readResponse.Diagnostics)
	}
	var readState hoursOfOperationOverrideModel
	readResponse.Diagnostics.Append(readResponse.State.Get(context.Background(), &readState)...)
	if readResponse.Diagnostics.HasError() || readState.Name.ValueString() != "remote holiday closure" || readState.Description.ValueString() != "remote description" {
		t.Fatalf("unexpected read state: %#v diagnostics=%v", readState, readResponse.Diagnostics)
	}

	planned := sampleOverrideModel()
	planned.Description = types.StringNull()
	planned.Recurrence = types.ObjectNull(hoursOfOperationOverrideRecurrenceAttributeTypes)
	updatePlan := overridePlan(t, planned)
	updateResponse := &resource.UpdateResponse{State: readResponse.State}
	implementation.Update(context.Background(), resource.UpdateRequest{State: readResponse.State, Plan: updatePlan}, updateResponse)
	if updateResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected update diagnostics: %v", updateResponse.Diagnostics)
	}
	if updateInput == nil || updateInput.Description == nil || updateInput.RecurrenceConfig == nil {
		t.Fatalf("expected update to preserve stored omitted optionals, got %#v", updateInput)
	}
	if aws.ToString(updateInput.InstanceId) != overrideInstanceID || aws.ToString(updateInput.HoursOfOperationId) != overrideHoursID || aws.ToString(updateInput.HoursOfOperationOverrideId) != overrideID || aws.ToString(updateInput.Name) != overrideName || aws.ToString(updateInput.EffectiveFrom) != overrideEffectiveFrom || aws.ToString(updateInput.EffectiveTill) != overrideEffectiveTill || updateInput.OverrideType != connecttypes.OverrideTypeStandard {
		t.Fatalf("unexpected update identity or mutable fields: %#v", updateInput)
	}
	if !reflect.DeepEqual(updateInput.Config, expectedConfig) || aws.ToString(updateInput.Description) != "remote description" {
		t.Fatalf("unexpected update config or description: %#v", updateInput)
	}
	if updateInput.RecurrenceConfig.RecurrencePattern == nil || updateInput.RecurrenceConfig.RecurrencePattern.Frequency != connecttypes.RecurrenceFrequencyYearly || !reflect.DeepEqual(updateInput.RecurrenceConfig.RecurrencePattern.ByMonth, []int32{12}) || !reflect.DeepEqual(updateInput.RecurrenceConfig.RecurrencePattern.ByMonthDay, []int32{24}) {
		t.Fatalf("unexpected update recurrence: %#v", updateInput.RecurrenceConfig)
	}

	emptyTimeWindowsModel := sampleOverrideModel()
	emptyTimeWindowsModel.TimeWindows = overrideTimeWindowsValue(t)
	emptyTimeWindowsPlan := overridePlan(t, emptyTimeWindowsModel)
	emptyConfigResponse := &resource.UpdateResponse{State: readResponse.State}
	implementation.Update(context.Background(), resource.UpdateRequest{State: readResponse.State, Plan: emptyTimeWindowsPlan}, emptyConfigResponse)
	if emptyConfigResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected empty time_windows update diagnostics: %v", emptyConfigResponse.Diagnostics)
	}
	if updateInput == nil || updateInput.Config == nil || len(updateInput.Config) != 0 {
		t.Fatalf("expected empty time_windows update to send a non-nil empty slice, got %#v", updateInput.Config)
	}

	deleteClient := &fakeHoursOfOperationOverrideClient{delete: func(_ context.Context, input *awsconnect.DeleteHoursOfOperationOverrideInput) (*awsconnect.DeleteHoursOfOperationOverrideOutput, error) {
		if aws.ToString(input.HoursOfOperationOverrideId) != overrideID {
			t.Fatalf("unexpected delete identity: %#v", input)
		}
		return nil, fmt.Errorf("gone: %w", &connecttypes.ResourceNotFoundException{})
	}}
	implementation.client = deleteClient
	deleteResponse := &resource.DeleteResponse{}
	implementation.Delete(context.Background(), resource.DeleteRequest{State: emptyConfigResponse.State}, deleteResponse)
	if deleteResponse.Diagnostics.HasError() {
		t.Fatalf("expected delete to tolerate not-found, got %v", deleteResponse.Diagnostics)
	}
}

func TestHoursOfOperationOverrideReadNotFoundRemovesState(t *testing.T) {
	client := &fakeHoursOfOperationOverrideClient{describe: func(context.Context, *awsconnect.DescribeHoursOfOperationOverrideInput) (*awsconnect.DescribeHoursOfOperationOverrideOutput, error) {
		return nil, fmt.Errorf("wrapped: %w", &connecttypes.ResourceNotFoundException{})
	}}
	state := overrideState(t, sampleOverrideModel())
	response := &resource.ReadResponse{State: state}
	(&hoursOfOperationOverrideResource{client: client}).Read(context.Background(), resource.ReadRequest{State: state}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected not-found diagnostics: %v", response.Diagnostics)
	}
	if !response.State.Raw.IsNull() {
		t.Fatalf("expected not-found read to remove state, got %s", response.State.Raw)
	}
}

func TestHoursOfOperationOverrideErrorsAndNilClient(t *testing.T) {
	model := sampleOverrideModel()
	plan := overridePlan(t, model)
	state := overrideState(t, model)
	createError := errors.New("create failed")
	resourceWithError := &hoursOfOperationOverrideResource{client: &fakeHoursOfOperationOverrideClient{create: func(context.Context, *awsconnect.CreateHoursOfOperationOverrideInput) (*awsconnect.CreateHoursOfOperationOverrideOutput, error) {
		return nil, createError
	}}}
	createResponse := &resource.CreateResponse{State: overrideState(t, model)}
	resourceWithError.Create(context.Background(), resource.CreateRequest{Plan: plan}, createResponse)
	if !createResponse.Diagnostics.HasError() {
		t.Fatal("expected create API error diagnostic")
	}

	readErrorResource := &hoursOfOperationOverrideResource{client: &fakeHoursOfOperationOverrideClient{describe: func(context.Context, *awsconnect.DescribeHoursOfOperationOverrideInput) (*awsconnect.DescribeHoursOfOperationOverrideOutput, error) {
		return nil, errors.New("read failed")
	}}}
	readResponse := &resource.ReadResponse{State: state}
	readErrorResource.Read(context.Background(), resource.ReadRequest{State: state}, readResponse)
	if !readResponse.Diagnostics.HasError() {
		t.Fatal("expected read API error diagnostic")
	}

	updateErrorResource := &hoursOfOperationOverrideResource{client: &fakeHoursOfOperationOverrideClient{update: func(context.Context, *awsconnect.UpdateHoursOfOperationOverrideInput) (*awsconnect.UpdateHoursOfOperationOverrideOutput, error) {
		return nil, errors.New("update failed")
	}}}
	updateResponse := &resource.UpdateResponse{State: state}
	updateErrorResource.Update(context.Background(), resource.UpdateRequest{State: state, Plan: plan}, updateResponse)
	if !updateResponse.Diagnostics.HasError() {
		t.Fatal("expected update API error diagnostic")
	}

	deleteErrorResource := &hoursOfOperationOverrideResource{client: &fakeHoursOfOperationOverrideClient{delete: func(context.Context, *awsconnect.DeleteHoursOfOperationOverrideInput) (*awsconnect.DeleteHoursOfOperationOverrideOutput, error) {
		return nil, errors.New("delete failed")
	}}}
	deleteResponse := &resource.DeleteResponse{}
	deleteErrorResource.Delete(context.Background(), resource.DeleteRequest{State: state}, deleteResponse)
	if !deleteResponse.Diagnostics.HasError() {
		t.Fatal("expected delete API error diagnostic")
	}

	nilResource := &hoursOfOperationOverrideResource{}
	nilResponse := &resource.CreateResponse{State: state}
	nilResource.Create(context.Background(), resource.CreateRequest{Plan: plan}, nilResponse)
	if !nilResponse.Diagnostics.HasError() {
		t.Fatal("expected nil client diagnostic")
	}
	nilReadResponse := &resource.ReadResponse{State: state}
	nilResource.Read(context.Background(), resource.ReadRequest{State: state}, nilReadResponse)
	if !nilReadResponse.Diagnostics.HasError() {
		t.Fatal("expected nil client read diagnostic")
	}
	nilUpdateResponse := &resource.UpdateResponse{State: state}
	nilResource.Update(context.Background(), resource.UpdateRequest{State: state, Plan: plan}, nilUpdateResponse)
	if !nilUpdateResponse.Diagnostics.HasError() {
		t.Fatal("expected nil client update diagnostic")
	}
	nilDeleteResponse := &resource.DeleteResponse{}
	nilResource.Delete(context.Background(), resource.DeleteRequest{State: state}, nilDeleteResponse)
	if !nilDeleteResponse.Diagnostics.HasError() {
		t.Fatal("expected nil client delete diagnostic")
	}
}

func TestHoursOfOperationOverrideImportState(t *testing.T) {
	implementation := NewHoursOfOperationOverrideResource()
	initialState := overrideState(t, sampleOverrideModel())
	response := &resource.ImportStateResponse{State: initialState}
	importable, ok := implementation.(resource.ResourceWithImportState)
	if !ok {
		t.Fatal("expected importable resource")
	}
	importable.ImportState(context.Background(), resource.ImportStateRequest{ID: "import-instance:import-hours:import-override"}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected import diagnostics: %v", response.Diagnostics)
	}
	var data hoursOfOperationOverrideModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &data)...)
	if response.Diagnostics.HasError() || data.InstanceID.ValueString() != "import-instance" || data.HoursOfOperationID.ValueString() != "import-hours" || data.OverrideID.ValueString() != "import-override" {
		t.Fatalf("unexpected imported identity: %#v diagnostics=%v", data, response.Diagnostics)
	}
	for _, invalid := range []string{"", "instance", "instance:hours", "instance::override", "instance:hours:"} {
		response = &resource.ImportStateResponse{State: initialState}
		importable.ImportState(context.Background(), resource.ImportStateRequest{ID: invalid}, response)
		if !response.Diagnostics.HasError() {
			t.Errorf("expected import diagnostic for %q", invalid)
		}
	}
}

func sampleOverrideModel() hoursOfOperationOverrideModel {
	return hoursOfOperationOverrideModel{
		InstanceID:         types.StringValue(overrideInstanceID),
		HoursOfOperationID: types.StringValue(overrideHoursID),
		OverrideID:         types.StringValue(overrideID),
		Name:               types.StringValue(overrideName),
		Description:        types.StringValue(overrideDescription),
		EffectiveFrom:      types.StringValue(overrideEffectiveFrom),
		EffectiveTill:      types.StringValue(overrideEffectiveTill),
		OverrideType:       types.StringValue("STANDARD"),
		TimeWindows: overrideTimeWindowsValue(nil,
			overrideTimeWindowEntry("SUNDAY", "10:00", "11:00"),
			overrideTimeWindowEntry("MONDAY", "09:30", "10:30"),
		),
		Recurrence: sampleRecurrenceValue(),
	}
}

func sampleRecurrenceValue() types.Object {
	return types.ObjectValueMust(hoursOfOperationOverrideRecurrenceAttributeTypes, map[string]attr.Value{
		"frequency":             types.StringValue("YEARLY"),
		"interval":              types.Int32Value(1),
		"by_month":              types.Int32Value(12),
		"by_month_day":          types.Int32Value(24),
		"by_weekday_occurrence": types.Int32Null(),
	})
}

func overrideTimeWindowEntry(day, opens, closes string) attr.Value {
	return types.ObjectValueMust(hoursOfOperationOverrideTimeWindowAttributeTypes, map[string]attr.Value{
		"day":    types.StringValue(day),
		"opens":  types.StringValue(opens),
		"closes": types.StringValue(closes),
	})
}

func overrideTimeWindowsValue(t *testing.T, entries ...attr.Value) types.Set {
	if t == nil {
		value, diagnostics := types.SetValue(types.ObjectType{AttrTypes: hoursOfOperationOverrideTimeWindowAttributeTypes}, entries)
		if diagnostics.HasError() {
			panic(diagnostics)
		}
		return value
	}
	value, diagnostics := types.SetValue(types.ObjectType{AttrTypes: hoursOfOperationOverrideTimeWindowAttributeTypes}, entries)
	if diagnostics.HasError() {
		t.Fatalf("unexpected time_windows set diagnostics: %v", diagnostics)
	}
	return value
}

func sampleRemoteOverride() *connecttypes.HoursOfOperationOverride {
	return &connecttypes.HoursOfOperationOverride{
		Config: []connecttypes.HoursOfOperationOverrideConfig{
			{Day: connecttypes.OverrideDaysMonday, StartTime: &connecttypes.OverrideTimeSlice{Hours: aws.Int32(9), Minutes: aws.Int32(30)}, EndTime: &connecttypes.OverrideTimeSlice{Hours: aws.Int32(10), Minutes: aws.Int32(30)}},
			{Day: connecttypes.OverrideDaysSunday, StartTime: &connecttypes.OverrideTimeSlice{Hours: aws.Int32(10), Minutes: aws.Int32(0)}, EndTime: &connecttypes.OverrideTimeSlice{Hours: aws.Int32(11), Minutes: aws.Int32(0)}},
		},
		Description:                aws.String("remote description"),
		EffectiveFrom:              aws.String(overrideEffectiveFrom),
		EffectiveTill:              aws.String(overrideEffectiveTill),
		HoursOfOperationId:         aws.String(overrideHoursID),
		HoursOfOperationOverrideId: aws.String(overrideID),
		Name:                       aws.String("remote holiday closure"),
		OverrideType:               connecttypes.OverrideTypeStandard,
		RecurrenceConfig: &connecttypes.RecurrenceConfig{RecurrencePattern: &connecttypes.RecurrencePattern{
			Frequency:  connecttypes.RecurrenceFrequencyYearly,
			Interval:   aws.Int32(1),
			ByMonth:    []int32{12},
			ByMonthDay: []int32{24},
		}},
	}
}

func overridePlan(t *testing.T, model hoursOfOperationOverrideModel) tfsdk.Plan {
	return tfsdk.Plan(overrideState(t, model))
}

func overrideState(t *testing.T, model hoursOfOperationOverrideModel) tfsdk.State {
	t.Helper()
	response := &resource.SchemaResponse{}
	NewHoursOfOperationOverrideResource().Schema(context.Background(), resource.SchemaRequest{}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", response.Diagnostics)
	}
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
		t.Fatalf("unexpected raw state conversion error: %v", err)
	}
	return tfsdk.State{Raw: raw, Schema: response.Schema}
}

func TestHoursOfOperationOverrideTimeWindowsSetValidatorMaximum(t *testing.T) {
	entries := make([]attr.Value, 0, 101)
	for index := 0; index < 101; index++ {
		entries = append(entries, overrideTimeWindowEntry(fmt.Sprintf("DAY-%d", index), "00:00", "01:00"))
	}
	value := overrideTimeWindowsValue(t, entries...)
	response := &validator.SetResponse{}
	overrideTimeWindowsSetValidator{}.ValidateSet(context.Background(), validator.SetRequest{Path: path.Root("time_windows"), ConfigValue: value}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected time_windows maximum diagnostic")
	}
}

func TestHoursOfOperationOverrideFactoryCreatesIndependentResources(t *testing.T) {
	factory := HoursOfOperationOverrideResourceFactory()
	first, ok := factory().(*hoursOfOperationOverrideResource)
	if !ok {
		t.Fatal("expected hours override resource")
	}
	second, ok := factory().(*hoursOfOperationOverrideResource)
	if !ok {
		t.Fatal("expected hours override resource")
	}
	if first == second {
		t.Fatal("expected factory to create independent resources")
	}
}

func TestHoursOfOperationOverrideTimeWindowMappingAcceptsOmittedAndRejectsUnknownApplyValues(t *testing.T) {
	empty, diagnostics := hoursOfOperationOverrideTimeWindowsFromTerraform(types.SetNull(types.ObjectType{AttrTypes: hoursOfOperationOverrideTimeWindowAttributeTypes}))
	if diagnostics.HasError() || empty == nil || len(empty) != 0 {
		t.Fatalf("expected omitted time_windows to map to a known non-nil empty slice, got %#v diagnostics=%v", empty, diagnostics)
	}
	model := sampleOverrideModel()
	model.OverrideType = types.StringValue("STANDARD")
	model.TimeWindows = types.SetNull(types.ObjectType{AttrTypes: hoursOfOperationOverrideTimeWindowAttributeTypes})
	createInput, createDiagnostics := createHoursOfOperationOverrideInput(context.Background(), model)
	if createDiagnostics.HasError() || createInput.Config == nil || len(createInput.Config) != 0 {
		t.Fatalf("expected omitted time_windows create to send a non-nil empty slice, got %#v diagnostics=%v", createInput, createDiagnostics)
	}
	for name, value := range map[string]types.Set{
		"unknown": types.SetUnknown(types.ObjectType{AttrTypes: hoursOfOperationOverrideTimeWindowAttributeTypes}),
	} {
		t.Run(name, func(t *testing.T) {
			_, diagnostics := hoursOfOperationOverrideTimeWindowsFromTerraform(value)
			if !diagnostics.HasError() {
				t.Fatal("expected apply-time time_windows diagnostic")
			}
		})
	}
}

func TestHoursOfOperationOverrideImportRejectsExtraComponents(t *testing.T) {
	implementation, ok := NewHoursOfOperationOverrideResource().(resource.ResourceWithImportState)
	if !ok {
		t.Fatal("expected importable resource")
	}
	state := overrideState(t, sampleOverrideModel())
	response := &resource.ImportStateResponse{State: state}
	implementation.ImportState(context.Background(), resource.ImportStateRequest{ID: "a:b:c:d"}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected diagnostic for extra import component")
	}
}

func TestHoursOfOperationOverrideDateValidatorAllowsUnknownAndNull(t *testing.T) {
	for name, value := range map[string]types.String{"unknown": types.StringUnknown(), "null": types.StringNull()} {
		t.Run(name, func(t *testing.T) {
			response := &validator.StringResponse{}
			dateValidator{}.ValidateString(context.Background(), validator.StringRequest{ConfigValue: value}, response)
			if response.Diagnostics.HasError() {
				t.Fatalf("unexpected %s diagnostic: %v", name, response.Diagnostics)
			}
		})
	}
}

func TestHoursOfOperationOverrideMappingRoundTripsConfigAndRecurrence(t *testing.T) {
	remote := sampleRemoteOverride()
	model := hoursOfOperationOverrideModel{}
	diagnostics := setHoursOfOperationOverrideModel(&model, *remote)
	if diagnostics.HasError() {
		t.Fatalf("unexpected remote mapping diagnostics: %v", diagnostics)
	}
	if model.TimeWindows.IsNull() || model.Recurrence.IsNull() {
		t.Fatal("expected mapped time_windows and recurrence values")
	}
	var timeWindows []hoursOfOperationOverrideTimeWindowModel
	diagnostics.Append(model.TimeWindows.ElementsAs(context.Background(), &timeWindows, false)...)
	if diagnostics.HasError() {
		t.Fatalf("unexpected time_windows state decode diagnostics: %v", diagnostics)
	}
	if len(timeWindows) != 2 {
		t.Fatalf("expected two time window entries, got %d", len(timeWindows))
	}
	keys := make([]string, 0, len(model.TimeWindows.Elements()))
	for _, element := range model.TimeWindows.Elements() {
		object, ok := element.(types.Object)
		if !ok {
			t.Fatalf("expected time_windows object element, got %T", element)
		}
		day, ok := object.Attributes()["day"].(types.String)
		if !ok {
			t.Fatalf("expected time_windows day string, got %T", object.Attributes()["day"])
		}
		keys = append(keys, day.ValueString())
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"MONDAY", "SUNDAY"}) {
		t.Fatalf("unexpected config days: %v", keys)
	}
}

func TestHoursOfOperationOverrideReadMapsNilAndEmptyRemoteWindowsToCanonicalEmptySet(t *testing.T) {
	for name, config := range map[string][]connecttypes.HoursOfOperationOverrideConfig{
		"nil":   nil,
		"empty": make([]connecttypes.HoursOfOperationOverrideConfig, 0),
	} {
		t.Run(name, func(t *testing.T) {
			model := hoursOfOperationOverrideModel{}
			remote := connecttypes.HoursOfOperationOverride{Config: config}
			diagnostics := setHoursOfOperationOverrideModel(&model, remote)
			if diagnostics.HasError() || model.TimeWindows.IsNull() || model.TimeWindows.IsUnknown() || len(model.TimeWindows.Elements()) != 0 {
				t.Fatalf("expected canonical empty time_windows for %s config, model=%#v diagnostics=%v", name, model.TimeWindows, diagnostics)
			}
		})
	}
}
