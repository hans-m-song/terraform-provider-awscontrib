package connect

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var _ resource.Resource = &hoursOfOperationOverrideResource{}
var _ resource.ResourceWithConfigure = &hoursOfOperationOverrideResource{}
var _ resource.ResourceWithImportState = &hoursOfOperationOverrideResource{}
var _ resource.ResourceWithConfigValidators = &hoursOfOperationOverrideResource{}

type hoursOfOperationOverrideClient interface {
	CreateHoursOfOperationOverride(context.Context, *awsconnect.CreateHoursOfOperationOverrideInput, ...func(*awsconnect.Options)) (*awsconnect.CreateHoursOfOperationOverrideOutput, error)
	DescribeHoursOfOperationOverride(context.Context, *awsconnect.DescribeHoursOfOperationOverrideInput, ...func(*awsconnect.Options)) (*awsconnect.DescribeHoursOfOperationOverrideOutput, error)
	UpdateHoursOfOperationOverride(context.Context, *awsconnect.UpdateHoursOfOperationOverrideInput, ...func(*awsconnect.Options)) (*awsconnect.UpdateHoursOfOperationOverrideOutput, error)
	DeleteHoursOfOperationOverride(context.Context, *awsconnect.DeleteHoursOfOperationOverrideInput, ...func(*awsconnect.Options)) (*awsconnect.DeleteHoursOfOperationOverrideOutput, error)
}

type hoursOfOperationOverrideResource struct {
	client hoursOfOperationOverrideClient
}

type hoursOfOperationOverrideModel struct {
	InstanceID         types.String `tfsdk:"instance_id"`
	HoursOfOperationID types.String `tfsdk:"hours_of_operation_id"`
	OverrideID         types.String `tfsdk:"override_id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	EffectiveFrom      types.String `tfsdk:"effective_from"`
	EffectiveTill      types.String `tfsdk:"effective_till"`
	OverrideType       types.String `tfsdk:"override_type"`
	TimeWindows        types.Set    `tfsdk:"time_windows"`
	Recurrence         types.Object `tfsdk:"recurrence"`
}

type hoursOfOperationOverrideTimeWindowModel struct {
	Day    types.String `tfsdk:"day"`
	Opens  types.String `tfsdk:"opens"`
	Closes types.String `tfsdk:"closes"`
}

type hoursOfOperationOverrideRecurrenceModel struct {
	Frequency           types.String `tfsdk:"frequency"`
	Interval            types.Int32  `tfsdk:"interval"`
	ByMonth             types.Int32  `tfsdk:"by_month"`
	ByMonthDay          types.Int32  `tfsdk:"by_month_day"`
	ByWeekdayOccurrence types.Int32  `tfsdk:"by_weekday_occurrence"`
}

var hoursOfOperationOverrideTimeWindowAttributeTypes = map[string]attr.Type{
	"day":    types.StringType,
	"opens":  types.StringType,
	"closes": types.StringType,
}

var hoursOfOperationOverrideRecurrenceAttributeTypes = map[string]attr.Type{
	"frequency":             types.StringType,
	"interval":              types.Int32Type,
	"by_month":              types.Int32Type,
	"by_month_day":          types.Int32Type,
	"by_weekday_occurrence": types.Int32Type,
}

const (
	dateLayout = "2006-01-02"
)

func NewHoursOfOperationOverrideResource() resource.Resource {
	return &hoursOfOperationOverrideResource{}
}

func HoursOfOperationOverrideResourceFactory() func() resource.Resource {
	return func() resource.Resource {
		return NewHoursOfOperationOverrideResource()
	}
}

func (r *hoursOfOperationOverrideResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connect_hours_of_operation_override"
}

func (r *hoursOfOperationOverrideResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		MarkdownDescription: "Manages an Amazon Connect hours-of-operation override.",
		Attributes: map[string]resourceschema.Attribute{
			"instance_id": resourceschema.StringAttribute{
				MarkdownDescription: "Amazon Connect instance identifier.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{stringConstraintValidator{attributeName: "instance_id", minLength: 1, maxLength: 100}},
			},
			"hours_of_operation_id": resourceschema.StringAttribute{
				MarkdownDescription: "Parent hours-of-operation identifier.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"override_id": resourceschema.StringAttribute{
				MarkdownDescription: "Hours-of-operation override identifier.",
				Computed:            true,
			},
			"name": resourceschema.StringAttribute{
				MarkdownDescription: "Override name.",
				Required:            true,
				Validators:          []validator.String{stringConstraintValidator{attributeName: "name", minLength: 1, maxLength: 127, allowLineBreaks: true}},
			},
			"description": resourceschema.StringAttribute{
				MarkdownDescription: "Optional override description. Removing a stored description requires replacement because the Amazon Connect API cannot clear it.",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{descriptionRemovalRequiresReplace{}},
				Validators:          []validator.String{stringConstraintValidator{attributeName: "description", minLength: 1, maxLength: 250, allowLineBreaks: true}},
			},
			"effective_from": resourceschema.StringAttribute{
				MarkdownDescription: "Date on which the override becomes effective, in YYYY-MM-DD format.",
				Required:            true,
				Validators:          []validator.String{dateValidator{}},
			},
			"effective_till": resourceschema.StringAttribute{
				MarkdownDescription: "Date through which the override is effective, in YYYY-MM-DD format.",
				Required:            true,
				Validators:          []validator.String{dateValidator{}},
			},
			"override_type": resourceschema.StringAttribute{
				MarkdownDescription: "Override behavior: STANDARD, OPEN, or CLOSED.",
				Required:            true,
				Validators:          []validator.String{stringEnumValidator{attributeName: "override_type", allowed: []string{"STANDARD", "OPEN", "CLOSED"}}},
			},
			"time_windows": resourceschema.SetNestedAttribute{
				MarkdownDescription: "Unordered day/time windows for the override. Each time uses zero-padded HH:MM format. STANDARD and CLOSED overrides may omit windows.",
				Optional:            true,
				Computed:            true,
				Default:             setdefault.StaticValue(emptyHoursOfOperationOverrideTimeWindows()),
				Validators:          []validator.Set{overrideTimeWindowsSetValidator{}},
				NestedObject: resourceschema.NestedAttributeObject{
					Attributes: map[string]resourceschema.Attribute{
						"day": resourceschema.StringAttribute{
							MarkdownDescription: "Day to which the override applies.",
							Required:            true,
							Validators:          []validator.String{stringEnumValidator{attributeName: "day", allowed: []string{"SUNDAY", "MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY"}}},
						},
						"opens": resourceschema.StringAttribute{
							MarkdownDescription: "Opening time in zero-padded HH:MM format, from 00:00 through 23:59.",
							Required:            true,
							Validators:          []validator.String{timeWindowStringValidator{attributeName: "opens"}},
						},
						"closes": resourceschema.StringAttribute{
							MarkdownDescription: "Closing time in zero-padded HH:MM format, from 00:00 through 23:59.",
							Required:            true,
							Validators:          []validator.String{timeWindowStringValidator{attributeName: "closes"}},
						},
					},
				},
			},
			"recurrence": resourceschema.SingleNestedAttribute{
				MarkdownDescription: "Optional recurrence pattern. Removing an existing recurrence requires replacement because the Amazon Connect API cannot clear it.",
				Optional:            true,
				PlanModifiers:       []planmodifier.Object{recurrenceRemovalRequiresReplace{}},
				Validators:          []validator.Object{recurrenceMutualExclusionValidator{}},
				Attributes: map[string]resourceschema.Attribute{
					"frequency": resourceschema.StringAttribute{
						MarkdownDescription: "Recurrence frequency: WEEKLY, MONTHLY, or YEARLY.",
						Required:            true,
						Validators:          []validator.String{stringEnumValidator{attributeName: "frequency", allowed: []string{"WEEKLY", "MONTHLY", "YEARLY"}}},
					},
					"interval": resourceschema.Int32Attribute{
						MarkdownDescription: "Number of frequency units between occurrences, from 1 through 6.",
						Required:            true,
						Validators:          []validator.Int32{int32RangeValidator{attributeName: "interval", min: 1, max: 6}},
					},
					"by_month": resourceschema.Int32Attribute{
						MarkdownDescription: "Optional month number from 1 through 12.",
						Optional:            true,
						Validators:          []validator.Int32{int32RangeValidator{attributeName: "by_month", min: 1, max: 12}},
					},
					"by_month_day": resourceschema.Int32Attribute{
						MarkdownDescription: "Optional month day from -1 through 31, excluding 0.",
						Optional:            true,
						Validators:          []validator.Int32{int32RangeValidator{attributeName: "by_month_day", min: -1, max: 31, excludeZero: true}},
					},
					"by_weekday_occurrence": resourceschema.Int32Attribute{
						MarkdownDescription: "Optional weekday occurrence from -1 through 4, excluding 0.",
						Optional:            true,
						Validators:          []validator.Int32{int32RangeValidator{attributeName: "by_weekday_occurrence", min: -1, max: 4, excludeZero: true}},
					},
				},
			},
		},
	}
}

func (r *hoursOfOperationOverrideResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{hoursOfOperationOverrideConfigValidator{}}
}

func (r *hoursOfOperationOverrideResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	factory, ok := req.ProviderData.(clientFactory)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected an Amazon Connect client factory, got %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = factory.Connect()
}

func (r *hoursOfOperationOverrideResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data hoursOfOperationOverrideModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}

	input, diagnostics := createHoursOfOperationOverrideInput(ctx, data)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	output, err := r.client.CreateHoursOfOperationOverride(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Hours-of-Operation Override",
			fmt.Sprintf("Could not create an hours-of-operation override in instance %q: %s", aws.ToString(input.InstanceId), err),
		)
		return
	}
	if output == nil || output.HoursOfOperationOverrideId == nil || aws.ToString(output.HoursOfOperationOverrideId) == "" {
		resp.Diagnostics.AddError(
			"Invalid Create Hours-of-Operation Override Response",
			"Amazon Connect returned no hours-of-operation override identifier after creation.",
		)
		return
	}

	data.OverrideID = types.StringValue(aws.ToString(output.HoursOfOperationOverrideId))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *hoursOfOperationOverrideResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data hoursOfOperationOverrideModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}

	input, diagnostics := hoursOfOperationOverrideIdentity(data)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	output, err := r.client.DescribeHoursOfOperationOverride(ctx, input)
	if err != nil {
		if isHoursOfOperationOverrideNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Unable to Read Hours-of-Operation Override",
			fmt.Sprintf("Could not describe hours-of-operation override %q: %s", aws.ToString(input.HoursOfOperationOverrideId), err),
		)
		return
	}
	if output == nil || output.HoursOfOperationOverride == nil {
		resp.Diagnostics.AddError(
			"Invalid Describe Hours-of-Operation Override Response",
			"Amazon Connect returned no hours-of-operation override details.",
		)
		return
	}

	resp.Diagnostics.Append(setHoursOfOperationOverrideModel(&data, *output.HoursOfOperationOverride)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *hoursOfOperationOverrideResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var prior hoursOfOperationOverrideModel
	var planned hoursOfOperationOverrideModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}

	planned.Description = preserveOptionalString(planned.Description, prior.Description)
	planned.Recurrence = preserveOptionalObject(planned.Recurrence, prior.Recurrence)
	if planned.OverrideID.IsNull() || planned.OverrideID.IsUnknown() {
		planned.OverrideID = prior.OverrideID
	}
	if planned.Description.IsUnknown() || planned.Recurrence.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("description"),
			"Unknown Optional Hours-of-Operation Override Value",
			"description and recurrence must be known before an hours-of-operation override can be updated",
		)
		return
	}

	input, diagnostics := updateHoursOfOperationOverrideInput(ctx, planned)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.client.UpdateHoursOfOperationOverride(ctx, input); err != nil {
		resp.Diagnostics.AddError(
			"Unable to Update Hours-of-Operation Override",
			fmt.Sprintf("Could not update hours-of-operation override %q: %s", aws.ToString(input.HoursOfOperationOverrideId), err),
		)
		return
	}

	if planned.OverrideID.IsNull() || planned.OverrideID.IsUnknown() {
		planned.OverrideID = prior.OverrideID
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &planned)...)
}

func (r *hoursOfOperationOverrideResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data hoursOfOperationOverrideModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}

	input, diagnostics := hoursOfOperationOverrideIdentity(data)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteInput := &awsconnect.DeleteHoursOfOperationOverrideInput{
		HoursOfOperationId:         input.HoursOfOperationId,
		HoursOfOperationOverrideId: input.HoursOfOperationOverrideId,
		InstanceId:                 input.InstanceId,
	}
	if _, err := r.client.DeleteHoursOfOperationOverride(ctx, deleteInput); err != nil && !isHoursOfOperationOverrideNotFound(err) {
		resp.Diagnostics.AddError(
			"Unable to Delete Hours-of-Operation Override",
			fmt.Sprintf("Could not delete hours-of-operation override %q: %s", aws.ToString(input.HoursOfOperationOverrideId), err),
		)
	}
}

func (r *hoursOfOperationOverrideResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, ":")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("expected import identifier with format instance_id:hours_of_operation_id:override_id; got %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("instance_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("hours_of_operation_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("override_id"), parts[2])...)
}

func createHoursOfOperationOverrideInput(ctx context.Context, data hoursOfOperationOverrideModel) (*awsconnect.CreateHoursOfOperationOverrideInput, diag.Diagnostics) {
	identity, diagnostics := requiredHoursOfOperationOverrideFields(data)
	if diagnostics.HasError() {
		return nil, diagnostics
	}

	timeWindows, timeWindowsDiagnostics := hoursOfOperationOverrideTimeWindowsFromTerraform(data.TimeWindows)
	diagnostics.Append(timeWindowsDiagnostics...)
	if diagnostics.HasError() {
		return nil, diagnostics
	}
	recurrence, recurrenceDiagnostics := hoursOfOperationOverrideRecurrenceFromTerraform(ctx, data.Recurrence)
	diagnostics.Append(recurrenceDiagnostics...)
	if diagnostics.HasError() {
		return nil, diagnostics
	}
	description, descriptionDiagnostics := optionalStringPointer(data.Description, path.Root("description"))
	diagnostics.Append(descriptionDiagnostics...)
	if diagnostics.HasError() {
		return nil, diagnostics
	}

	return &awsconnect.CreateHoursOfOperationOverrideInput{
		Config:             timeWindows,
		Description:        description,
		EffectiveFrom:      aws.String(identity.effectiveFrom),
		EffectiveTill:      aws.String(identity.effectiveTill),
		HoursOfOperationId: aws.String(identity.hoursOfOperationID),
		InstanceId:         aws.String(identity.instanceID),
		Name:               aws.String(identity.name),
		OverrideType:       connecttypes.OverrideType(identity.overrideType),
		RecurrenceConfig:   recurrence,
	}, diagnostics
}

func updateHoursOfOperationOverrideInput(ctx context.Context, data hoursOfOperationOverrideModel) (*awsconnect.UpdateHoursOfOperationOverrideInput, diag.Diagnostics) {
	identity, diagnostics := requiredHoursOfOperationOverrideFields(data)
	if diagnostics.HasError() {
		return nil, diagnostics
	}

	timeWindows, timeWindowsDiagnostics := hoursOfOperationOverrideTimeWindowsFromTerraform(data.TimeWindows)
	diagnostics.Append(timeWindowsDiagnostics...)
	if diagnostics.HasError() {
		return nil, diagnostics
	}
	recurrence, recurrenceDiagnostics := hoursOfOperationOverrideRecurrenceFromTerraform(ctx, data.Recurrence)
	diagnostics.Append(recurrenceDiagnostics...)
	if diagnostics.HasError() {
		return nil, diagnostics
	}
	description, descriptionDiagnostics := optionalStringPointer(data.Description, path.Root("description"))
	diagnostics.Append(descriptionDiagnostics...)
	if diagnostics.HasError() {
		return nil, diagnostics
	}

	return &awsconnect.UpdateHoursOfOperationOverrideInput{
		Config:                     timeWindows,
		Description:                description,
		EffectiveFrom:              aws.String(identity.effectiveFrom),
		EffectiveTill:              aws.String(identity.effectiveTill),
		HoursOfOperationId:         aws.String(identity.hoursOfOperationID),
		HoursOfOperationOverrideId: aws.String(identity.overrideID),
		InstanceId:                 aws.String(identity.instanceID),
		Name:                       aws.String(identity.name),
		OverrideType:               connecttypes.OverrideType(identity.overrideType),
		RecurrenceConfig:           recurrence,
	}, diagnostics
}

type hoursOfOperationOverrideRequiredFields struct {
	instanceID         string
	hoursOfOperationID string
	overrideID         string
	name               string
	effectiveFrom      string
	effectiveTill      string
	overrideType       string
}

func requiredHoursOfOperationOverrideFields(data hoursOfOperationOverrideModel) (hoursOfOperationOverrideRequiredFields, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	instanceID, instanceDiagnostics := requiredStringValue(data.InstanceID, path.Root("instance_id"))
	diagnostics.Append(instanceDiagnostics...)
	hoursOfOperationID, hoursDiagnostics := requiredStringValue(data.HoursOfOperationID, path.Root("hours_of_operation_id"))
	diagnostics.Append(hoursDiagnostics...)
	overrideID, overrideDiagnostics := requiredStringValue(data.OverrideID, path.Root("override_id"))
	if !data.OverrideID.IsNull() && !data.OverrideID.IsUnknown() {
		diagnostics.Append(overrideDiagnostics...)
	}
	name, nameDiagnostics := requiredStringValue(data.Name, path.Root("name"))
	diagnostics.Append(nameDiagnostics...)
	effectiveFrom, fromDiagnostics := requiredStringValue(data.EffectiveFrom, path.Root("effective_from"))
	diagnostics.Append(fromDiagnostics...)
	effectiveTill, tillDiagnostics := requiredStringValue(data.EffectiveTill, path.Root("effective_till"))
	diagnostics.Append(tillDiagnostics...)
	overrideType, typeDiagnostics := requiredStringValue(data.OverrideType, path.Root("override_type"))
	diagnostics.Append(typeDiagnostics...)

	return hoursOfOperationOverrideRequiredFields{
		instanceID:         instanceID,
		hoursOfOperationID: hoursOfOperationID,
		overrideID:         overrideID,
		name:               name,
		effectiveFrom:      effectiveFrom,
		effectiveTill:      effectiveTill,
		overrideType:       overrideType,
	}, diagnostics
}

func hoursOfOperationOverrideIdentity(data hoursOfOperationOverrideModel) (*awsconnect.DescribeHoursOfOperationOverrideInput, diag.Diagnostics) {
	instanceID, instanceDiagnostics := requiredStringValue(data.InstanceID, path.Root("instance_id"))
	hoursOfOperationID, hoursDiagnostics := requiredStringValue(data.HoursOfOperationID, path.Root("hours_of_operation_id"))
	overrideID, overrideDiagnostics := requiredStringValue(data.OverrideID, path.Root("override_id"))
	var diagnostics diag.Diagnostics
	diagnostics.Append(instanceDiagnostics...)
	diagnostics.Append(hoursDiagnostics...)
	diagnostics.Append(overrideDiagnostics...)
	if diagnostics.HasError() {
		return nil, diagnostics
	}

	return &awsconnect.DescribeHoursOfOperationOverrideInput{
		HoursOfOperationId:         aws.String(hoursOfOperationID),
		HoursOfOperationOverrideId: aws.String(overrideID),
		InstanceId:                 aws.String(instanceID),
	}, diagnostics
}

func requiredStringValue(value types.String, attributePath path.Path) (string, diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return "", diag.Diagnostics{diag.NewAttributeErrorDiagnostic(
			attributePath,
			"Unknown Required Hours-of-Operation Override Value",
			"the value must be known and non-null before an hours-of-operation override request can be sent",
		)}
	}
	return value.ValueString(), nil
}

func optionalStringPointer(value types.String, attributePath path.Path) (*string, diag.Diagnostics) {
	if value.IsUnknown() {
		return nil, diag.Diagnostics{diag.NewAttributeErrorDiagnostic(
			attributePath,
			"Unknown Optional Hours-of-Operation Override Value",
			"the value must be known before an hours-of-operation override request can be sent",
		)}
	}
	if value.IsNull() {
		return nil, nil
	}
	return aws.String(value.ValueString()), nil
}

func emptyHoursOfOperationOverrideTimeWindows() types.Set {
	return types.SetValueMust(types.ObjectType{AttrTypes: hoursOfOperationOverrideTimeWindowAttributeTypes}, []attr.Value{})
}

func hoursOfOperationOverrideTimeWindowsFromTerraform(value types.Set) ([]connecttypes.HoursOfOperationOverrideConfig, diag.Diagnostics) {
	if value.IsNull() {
		return make([]connecttypes.HoursOfOperationOverrideConfig, 0), nil
	}
	if value.IsUnknown() {
		return nil, diag.Diagnostics{diag.NewAttributeErrorDiagnostic(
			path.Root("time_windows"),
			"Unknown Hours-of-Operation Override Time Windows",
			"time_windows must be known before an hours-of-operation override request can be sent",
		)}
	}

	entries := make([]connecttypes.HoursOfOperationOverrideConfig, 0, len(value.Elements()))
	var diagnostics diag.Diagnostics
	for index, element := range value.Elements() {
		object, ok := element.(types.Object)
		if !ok || object.IsNull() || object.IsUnknown() {
			diagnostics.AddAttributeError(
				path.Root("time_windows"),
				"Invalid Hours-of-Operation Override Time Window",
				fmt.Sprintf("time_windows element %d must be a known object", index),
			)
			continue
		}

		attributes := object.Attributes()
		day, dayOK := attributes["day"].(types.String)
		opens, opensOK := attributes["opens"].(types.String)
		closes, closesOK := attributes["closes"].(types.String)
		if !dayOK || !opensOK || !closesOK {
			diagnostics.AddAttributeError(path.Root("time_windows"), "Invalid Hours-of-Operation Override Time Window", fmt.Sprintf("time_windows element %d has an unexpected object shape", index))
			continue
		}

		dayValue, dayDiagnostics := requiredStringValue(day, path.Root("time_windows"))
		opensValue, opensDiagnostics := hoursOfOperationOverrideTimeStringValue(opens, path.Root("time_windows").AtName("opens"))
		closesValue, closesDiagnostics := hoursOfOperationOverrideTimeStringValue(closes, path.Root("time_windows").AtName("closes"))
		diagnostics.Append(dayDiagnostics...)
		diagnostics.Append(opensDiagnostics...)
		diagnostics.Append(closesDiagnostics...)
		if dayDiagnostics.HasError() || opensDiagnostics.HasError() || closesDiagnostics.HasError() {
			continue
		}

		entries = append(entries, connecttypes.HoursOfOperationOverrideConfig{
			Day:       connecttypes.OverrideDays(dayValue),
			StartTime: opensValue,
			EndTime:   closesValue,
		})
	}
	if diagnostics.HasError() {
		return nil, diagnostics
	}

	sort.Slice(entries, func(left, right int) bool {
		return hoursOfOperationOverrideTimeWindowSortKey(entries[left]) < hoursOfOperationOverrideTimeWindowSortKey(entries[right])
	})
	return entries, diagnostics
}

func hoursOfOperationOverrideTimeStringValue(value types.String, attributePath path.Path) (*connecttypes.OverrideTimeSlice, diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return nil, diag.Diagnostics{diag.NewAttributeErrorDiagnostic(attributePath, "Unknown Hours-of-Operation Override Time", "the time must be known and non-null before an hours-of-operation override request can be sent")}
	}
	hours, minutes, ok := parseHoursOfOperationOverrideTime(value.ValueString())
	if !ok {
		return nil, diag.Diagnostics{diag.NewAttributeErrorDiagnostic(attributePath, "Invalid Hours-of-Operation Override Time", fmt.Sprintf("time must use zero-padded HH:MM format from 00:00 through 23:59; got %q", value.ValueString()))}
	}
	return &connecttypes.OverrideTimeSlice{Hours: aws.Int32(hours), Minutes: aws.Int32(minutes)}, nil
}

func parseHoursOfOperationOverrideTime(value string) (int32, int32, bool) {
	if len(value) != 5 || value[2] != ':' || value[0] < '0' || value[0] > '9' || value[1] < '0' || value[1] > '9' || value[3] < '0' || value[3] > '9' || value[4] < '0' || value[4] > '9' {
		return 0, 0, false
	}
	hours := int32(value[0]-'0')*10 + int32(value[1]-'0')
	minutes := int32(value[3]-'0')*10 + int32(value[4]-'0')
	if hours > 23 || minutes > 59 {
		return 0, 0, false
	}
	return hours, minutes, true
}

func requiredInt32Value(value types.Int32, attributePath path.Path) (int32, diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return 0, diag.Diagnostics{diag.NewAttributeErrorDiagnostic(attributePath, "Unknown Required Hours-of-Operation Override Value", "the value must be known and non-null before an hours-of-operation override request can be sent")}
	}
	return value.ValueInt32(), nil
}

func hoursOfOperationOverrideRecurrenceFromTerraform(ctx context.Context, value types.Object) (*connecttypes.RecurrenceConfig, diag.Diagnostics) {
	if value.IsNull() {
		return nil, nil
	}
	if value.IsUnknown() {
		return nil, diag.Diagnostics{diag.NewAttributeErrorDiagnostic(path.Root("recurrence"), "Unknown Hours-of-Operation Override Recurrence", "recurrence must be known before an hours-of-operation override request can be sent")}
	}

	var model hoursOfOperationOverrideRecurrenceModel
	diagnostics := value.As(ctx, &model, basetypes.ObjectAsOptions{})
	if diagnostics.HasError() {
		return nil, diagnostics
	}
	frequency, frequencyDiagnostics := requiredStringValue(model.Frequency, path.Root("recurrence").AtName("frequency"))
	interval, intervalDiagnostics := requiredInt32Value(model.Interval, path.Root("recurrence").AtName("interval"))
	diagnostics.Append(frequencyDiagnostics...)
	diagnostics.Append(intervalDiagnostics...)
	if diagnostics.HasError() {
		return nil, diagnostics
	}

	byMonth, byMonthDiagnostics := optionalInt32Slice(model.ByMonth, path.Root("recurrence").AtName("by_month"))
	byMonthDay, byMonthDayDiagnostics := optionalInt32Slice(model.ByMonthDay, path.Root("recurrence").AtName("by_month_day"))
	byWeekdayOccurrence, byWeekdayDiagnostics := optionalInt32Slice(model.ByWeekdayOccurrence, path.Root("recurrence").AtName("by_weekday_occurrence"))
	diagnostics.Append(byMonthDiagnostics...)
	diagnostics.Append(byMonthDayDiagnostics...)
	diagnostics.Append(byWeekdayDiagnostics...)
	if diagnostics.HasError() {
		return nil, diagnostics
	}

	return &connecttypes.RecurrenceConfig{RecurrencePattern: &connecttypes.RecurrencePattern{
		Frequency:           connecttypes.RecurrenceFrequency(frequency),
		Interval:            aws.Int32(interval),
		ByMonth:             byMonth,
		ByMonthDay:          byMonthDay,
		ByWeekdayOccurrence: byWeekdayOccurrence,
	}}, diagnostics
}

func optionalInt32Slice(value types.Int32, attributePath path.Path) ([]int32, diag.Diagnostics) {
	if value.IsUnknown() {
		return nil, diag.Diagnostics{diag.NewAttributeErrorDiagnostic(attributePath, "Unknown Optional Hours-of-Operation Override Value", "the value must be known before an hours-of-operation override request can be sent")}
	}
	if value.IsNull() {
		return nil, nil
	}
	return []int32{value.ValueInt32()}, nil
}

func setHoursOfOperationOverrideModel(data *hoursOfOperationOverrideModel, remote connecttypes.HoursOfOperationOverride) diag.Diagnostics {
	if value := aws.ToString(remote.HoursOfOperationOverrideId); value != "" {
		data.OverrideID = types.StringValue(value)
	}
	if value := aws.ToString(remote.HoursOfOperationId); value != "" {
		data.HoursOfOperationID = types.StringValue(value)
	}
	data.Name = stringValueOrNull(remote.Name)
	data.Description = stringValueOrNull(remote.Description)
	data.EffectiveFrom = stringValueOrNull(remote.EffectiveFrom)
	data.EffectiveTill = stringValueOrNull(remote.EffectiveTill)
	data.OverrideType = types.StringValue(string(remote.OverrideType))

	timeWindows, timeWindowsDiagnostics := hoursOfOperationOverrideTimeWindowsToTerraform(remote.Config)
	var diagnostics diag.Diagnostics
	diagnostics.Append(timeWindowsDiagnostics...)
	data.TimeWindows = timeWindows
	data.Recurrence = hoursOfOperationOverrideRecurrenceToTerraform(remote.RecurrenceConfig)
	return diagnostics
}

func stringValueOrNull(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(aws.ToString(value))
}

func hoursOfOperationOverrideTimeWindowsToTerraform(configs []connecttypes.HoursOfOperationOverrideConfig) (types.Set, diag.Diagnostics) {
	values := make([]attr.Value, 0, len(configs))
	for _, config := range configs {
		opens := hoursOfOperationOverrideTimeToTerraform(config.StartTime)
		closes := hoursOfOperationOverrideTimeToTerraform(config.EndTime)
		values = append(values, types.ObjectValueMust(hoursOfOperationOverrideTimeWindowAttributeTypes, map[string]attr.Value{
			"day":    types.StringValue(string(config.Day)),
			"opens":  opens,
			"closes": closes,
		}))
	}
	return types.SetValue(types.ObjectType{AttrTypes: hoursOfOperationOverrideTimeWindowAttributeTypes}, values)
}

func hoursOfOperationOverrideTimeToTerraform(value *connecttypes.OverrideTimeSlice) types.String {
	if value == nil {
		return types.StringNull()
	}
	hours := aws.ToInt32(value.Hours)
	minutes := aws.ToInt32(value.Minutes)
	return types.StringValue(fmt.Sprintf("%02d:%02d", hours, minutes))
}

func int32ValueOrNull(value *int32) types.Int32 {
	if value == nil {
		return types.Int32Null()
	}
	return types.Int32Value(aws.ToInt32(value))
}

func hoursOfOperationOverrideRecurrenceToTerraform(value *connecttypes.RecurrenceConfig) types.Object {
	if value == nil {
		return types.ObjectNull(hoursOfOperationOverrideRecurrenceAttributeTypes)
	}
	if value.RecurrencePattern == nil {
		return types.ObjectValueMust(hoursOfOperationOverrideRecurrenceAttributeTypes, map[string]attr.Value{
			"frequency":             types.StringNull(),
			"interval":              types.Int32Null(),
			"by_month":              types.Int32Null(),
			"by_month_day":          types.Int32Null(),
			"by_weekday_occurrence": types.Int32Null(),
		})
	}
	pattern := value.RecurrencePattern
	return types.ObjectValueMust(hoursOfOperationOverrideRecurrenceAttributeTypes, map[string]attr.Value{
		"frequency":             types.StringValue(string(pattern.Frequency)),
		"interval":              int32ValueOrNull(pattern.Interval),
		"by_month":              firstInt32OrNull(pattern.ByMonth),
		"by_month_day":          firstInt32OrNull(pattern.ByMonthDay),
		"by_weekday_occurrence": firstInt32OrNull(pattern.ByWeekdayOccurrence),
	})
}

func firstInt32OrNull(values []int32) types.Int32 {
	if len(values) == 0 {
		return types.Int32Null()
	}
	return types.Int32Value(values[0])
}

func hoursOfOperationOverrideTimeWindowSortKey(value connecttypes.HoursOfOperationOverrideConfig) string {
	return fmt.Sprintf("%s:%s:%s", value.Day, hoursOfOperationOverrideTimeSortKey(value.StartTime), hoursOfOperationOverrideTimeSortKey(value.EndTime))
}

func hoursOfOperationOverrideTimeSortKey(value *connecttypes.OverrideTimeSlice) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", aws.ToInt32(value.Hours), aws.ToInt32(value.Minutes))
}

func preserveOptionalString(planned, prior types.String) types.String {
	if planned.IsNull() && !prior.IsNull() {
		return prior
	}
	return planned
}

func preserveOptionalObject(planned, prior types.Object) types.Object {
	if planned.IsNull() && !prior.IsNull() {
		return prior
	}
	return planned
}

func isHoursOfOperationOverrideNotFound(err error) bool {
	if err == nil {
		return false
	}
	var notFound *connecttypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

type descriptionRemovalRequiresReplace struct{}

func (descriptionRemovalRequiresReplace) Description(context.Context) string {
	return "Removing an existing description requires replacement because Amazon Connect cannot clear it."
}

func (descriptionRemovalRequiresReplace) MarkdownDescription(ctx context.Context) string {
	return descriptionRemovalRequiresReplace{}.Description(ctx)
}

func (descriptionRemovalRequiresReplace) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() || req.PlanValue.IsUnknown() || !req.PlanValue.IsNull() {
		return
	}
	resp.RequiresReplace = true
}

type recurrenceRemovalRequiresReplace struct{}

func (recurrenceRemovalRequiresReplace) Description(context.Context) string {
	return "Removing an existing recurrence requires replacement because Amazon Connect cannot clear it."
}

func (recurrenceRemovalRequiresReplace) MarkdownDescription(ctx context.Context) string {
	return recurrenceRemovalRequiresReplace{}.Description(ctx)
}

func (recurrenceRemovalRequiresReplace) PlanModifyObject(_ context.Context, req planmodifier.ObjectRequest, resp *planmodifier.ObjectResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() || req.PlanValue.IsUnknown() || !req.PlanValue.IsNull() {
		return
	}
	resp.RequiresReplace = true
}

type stringConstraintValidator struct {
	attributeName   string
	minLength       int
	maxLength       int
	allowLineBreaks bool
}

func (v stringConstraintValidator) Description(context.Context) string {
	allowedCharacters := ""
	if v.allowLineBreaks {
		allowedCharacters = "; control characters other than carriage return, line feed, and tab are not allowed"
	}
	return fmt.Sprintf("%s must contain between %d and %d characters%s", v.attributeName, v.minLength, v.maxLength, allowedCharacters)
}

func (v stringConstraintValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v stringConstraintValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	value := req.ConfigValue.ValueString()
	length := utf8.RuneCountInString(value)
	if length < v.minLength || length > v.maxLength {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid "+v.attributeName,
			fmt.Sprintf("%s must contain between %d and %d characters; got %d", v.attributeName, v.minLength, v.maxLength, length),
		)
		return
	}
	if v.allowLineBreaks && !validHoursOverrideText(value) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid "+v.attributeName,
			fmt.Sprintf("%s contains a control character other than carriage return, line feed, or tab", v.attributeName),
		)
	}
}

func validHoursOverrideText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.Is(unicode.C, character) && character != '\r' && character != '\n' && character != '\t' {
			return false
		}
	}
	return true
}

type stringEnumValidator struct {
	attributeName string
	allowed       []string
}

func (v stringEnumValidator) Description(context.Context) string {
	return fmt.Sprintf("must be one of %s", strings.Join(v.allowed, ", "))
}

func (v stringEnumValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v stringEnumValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for _, allowed := range v.allowed {
		if req.ConfigValue.ValueString() == allowed {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Invalid "+v.attributeName, fmt.Sprintf("%s must be one of %s; got %q", v.attributeName, strings.Join(v.allowed, ", "), req.ConfigValue.ValueString()))
}

type int32RangeValidator struct {
	attributeName string
	min           int32
	max           int32
	excludeZero   bool
}

func (v int32RangeValidator) Description(context.Context) string {
	if v.excludeZero {
		return fmt.Sprintf("must be between %d and %d, excluding 0", v.min, v.max)
	}
	return fmt.Sprintf("must be between %d and %d", v.min, v.max)
}

func (v int32RangeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v int32RangeValidator) ValidateInt32(_ context.Context, req validator.Int32Request, resp *validator.Int32Response) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueInt32()
	if value < v.min || value > v.max || (v.excludeZero && value == 0) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid "+v.attributeName, fmt.Sprintf("%s %d is outside the supported range", v.attributeName, value))
	}
}

type dateValidator struct{}

func (dateValidator) Description(context.Context) string {
	return "must be a valid date in YYYY-MM-DD format"
}

func (dateValidator) MarkdownDescription(ctx context.Context) string {
	return dateValidator{}.Description(ctx)
}

func (dateValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()
	if len(value) != len(dateLayout) || value[4] != '-' || value[7] != '-' {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Date", fmt.Sprintf("%s must use YYYY-MM-DD format; got %q", req.Path, value))
		return
	}
	if _, err := time.Parse(dateLayout, value); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Date", fmt.Sprintf("%s must be a valid date in YYYY-MM-DD format; got %q", req.Path, value))
	}
}

type timeWindowStringValidator struct {
	attributeName string
}

func (v timeWindowStringValidator) Description(context.Context) string {
	return v.attributeName + " must use zero-padded HH:MM format from 00:00 through 23:59"
}

func (v timeWindowStringValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v timeWindowStringValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, _, ok := parseHoursOfOperationOverrideTime(req.ConfigValue.ValueString()); !ok {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid "+v.attributeName, fmt.Sprintf("%s must use zero-padded HH:MM format from 00:00 through 23:59; got %q", v.attributeName, req.ConfigValue.ValueString()))
	}
}

type hoursOfOperationOverrideConfigValidator struct{}

func (hoursOfOperationOverrideConfigValidator) Description(context.Context) string {
	return "OPEN overrides require at least one time window; STANDARD and CLOSED overrides may have no time windows"
}

func (hoursOfOperationOverrideConfigValidator) MarkdownDescription(ctx context.Context) string {
	return hoursOfOperationOverrideConfigValidator{}.Description(ctx)
}

func (hoursOfOperationOverrideConfigValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var overrideType types.String
	var timeWindows types.Set
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("override_type"), &overrideType)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("time_windows"), &timeWindows)...)
	if resp.Diagnostics.HasError() || overrideType.IsNull() || overrideType.IsUnknown() || timeWindows.IsUnknown() {
		return
	}
	if overrideType.ValueString() != "OPEN" {
		return
	}
	if timeWindows.IsNull() || len(timeWindows.Elements()) == 0 {
		resp.Diagnostics.AddAttributeError(path.Root("time_windows"), "Missing OPEN Override Time Window", "OPEN overrides require at least one time window")
	}
}

type overrideTimeWindowsSetValidator struct{}

func (overrideTimeWindowsSetValidator) Description(context.Context) string {
	return "must contain at most 100 time windows"
}

func (overrideTimeWindowsSetValidator) MarkdownDescription(ctx context.Context) string {
	return overrideTimeWindowsSetValidator{}.Description(ctx)
}

func (overrideTimeWindowsSetValidator) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if len(req.ConfigValue.Elements()) > 100 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Hours-of-Operation Override Time Windows", fmt.Sprintf("time_windows must contain at most 100 entries; got %d", len(req.ConfigValue.Elements())))
	}
}

type recurrenceMutualExclusionValidator struct{}

func (recurrenceMutualExclusionValidator) Description(context.Context) string {
	return "by_month_day and by_weekday_occurrence cannot both be configured"
}

func (recurrenceMutualExclusionValidator) MarkdownDescription(ctx context.Context) string {
	return recurrenceMutualExclusionValidator{}.Description(ctx)
}

func (recurrenceMutualExclusionValidator) ValidateObject(_ context.Context, req validator.ObjectRequest, resp *validator.ObjectResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	attributes := req.ConfigValue.Attributes()
	byMonthDay, monthDayOK := attributes["by_month_day"].(types.Int32)
	byWeekdayOccurrence, weekdayOK := attributes["by_weekday_occurrence"].(types.Int32)
	if monthDayOK && weekdayOK && !byMonthDay.IsNull() && !byMonthDay.IsUnknown() && !byWeekdayOccurrence.IsNull() && !byWeekdayOccurrence.IsUnknown() {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Recurrence Pattern", "by_month_day and by_weekday_occurrence cannot both be configured")
	}
}
