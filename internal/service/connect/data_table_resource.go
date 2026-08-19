package connect

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var _ resource.Resource = &dataTableResource{}
var _ resource.ResourceWithConfigure = &dataTableResource{}
var _ resource.ResourceWithImportState = &dataTableResource{}
var _ resource.ResourceWithModifyPlan = &dataTableResource{}
var _ resource.ResourceWithConfigValidators = &dataTableResource{}

type dataTableClient interface {
	CreateDataTable(context.Context, *awsconnect.CreateDataTableInput, ...func(*awsconnect.Options)) (*awsconnect.CreateDataTableOutput, error)
	DescribeDataTable(context.Context, *awsconnect.DescribeDataTableInput, ...func(*awsconnect.Options)) (*awsconnect.DescribeDataTableOutput, error)
	UpdateDataTableMetadata(context.Context, *awsconnect.UpdateDataTableMetadataInput, ...func(*awsconnect.Options)) (*awsconnect.UpdateDataTableMetadataOutput, error)
	DeleteDataTable(context.Context, *awsconnect.DeleteDataTableInput, ...func(*awsconnect.Options)) (*awsconnect.DeleteDataTableOutput, error)
	CreateDataTableAttribute(context.Context, *awsconnect.CreateDataTableAttributeInput, ...func(*awsconnect.Options)) (*awsconnect.CreateDataTableAttributeOutput, error)
	UpdateDataTableAttribute(context.Context, *awsconnect.UpdateDataTableAttributeInput, ...func(*awsconnect.Options)) (*awsconnect.UpdateDataTableAttributeOutput, error)
	DeleteDataTableAttribute(context.Context, *awsconnect.DeleteDataTableAttributeInput, ...func(*awsconnect.Options)) (*awsconnect.DeleteDataTableAttributeOutput, error)
	ListDataTableAttributes(context.Context, *awsconnect.ListDataTableAttributesInput, ...func(*awsconnect.Options)) (*awsconnect.ListDataTableAttributesOutput, error)
	BatchCreateDataTableValue(context.Context, *awsconnect.BatchCreateDataTableValueInput, ...func(*awsconnect.Options)) (*awsconnect.BatchCreateDataTableValueOutput, error)
	BatchUpdateDataTableValue(context.Context, *awsconnect.BatchUpdateDataTableValueInput, ...func(*awsconnect.Options)) (*awsconnect.BatchUpdateDataTableValueOutput, error)
	BatchDeleteDataTableValue(context.Context, *awsconnect.BatchDeleteDataTableValueInput, ...func(*awsconnect.Options)) (*awsconnect.BatchDeleteDataTableValueOutput, error)
	ListDataTableValues(context.Context, *awsconnect.ListDataTableValuesInput, ...func(*awsconnect.Options)) (*awsconnect.ListDataTableValuesOutput, error)
}

type dataTableResource struct {
	client      dataTableClient
	coordinator *dataTableCoordinator
}

type dataTableModel struct {
	InstanceID     types.String `tfsdk:"instance_id"`
	ID             types.String `tfsdk:"id"`
	ARN            types.String `tfsdk:"arn"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	TimeZone       types.String `tfsdk:"time_zone"`
	ValueLockLevel types.String `tfsdk:"value_lock_level"`
	Status         types.String `tfsdk:"status"`
	Attributes     types.Map    `tfsdk:"attributes"`
	DefaultValues  types.Map    `tfsdk:"default_values"`
}

type dataTableAttributeModel struct {
	ValueType   types.String `tfsdk:"value_type"`
	Description types.String `tfsdk:"description"`
	Primary     types.Bool   `tfsdk:"primary"`
}

var dataTableAttributeTypes = map[string]attr.Type{
	"value_type":  types.StringType,
	"description": types.StringType,
	"primary":     types.BoolType,
}

const defaultDataTableRecordID = "DEFAULT"

func NewDataTableResource() resource.Resource {
	return &dataTableResource{coordinator: newDataTableCoordinator()}
}

func DataTableResourceFactory() func() resource.Resource {
	coordinator := newDataTableCoordinator()
	return func() resource.Resource {
		return &dataTableResource{coordinator: coordinator}
	}
}

func DataTableResourceFactories() (func() resource.Resource, func() resource.Resource) {
	coordinator := newDataTableCoordinator()
	return func() resource.Resource {
			return &dataTableResource{coordinator: coordinator}
		}, func() resource.Resource {
			return &dataTableRecordResource{coordinator: coordinator}
		}
}

func (r *dataTableResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connect_data_table"
}

func (r *dataTableResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		MarkdownDescription: "Manages Amazon Connect data-table metadata, the complete set of attributes represented by this resource, and explicit DEFAULT values. Mutations for the same instance and table are serialized within one provider process. Attribute validation rules and tags are not currently represented.",
		Attributes: map[string]resourceschema.Attribute{
			"instance_id": resourceschema.StringAttribute{
				MarkdownDescription: "Amazon Connect instance identifier.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"id": resourceschema.StringAttribute{
				MarkdownDescription: "Data-table identifier.",
				Computed:            true,
			},
			"arn": resourceschema.StringAttribute{
				MarkdownDescription: "Data-table ARN.",
				Computed:            true,
			},
			"name": resourceschema.StringAttribute{
				MarkdownDescription: "Data-table name.",
				Required:            true,
			},
			"description": resourceschema.StringAttribute{
				MarkdownDescription: "Optional data-table description. Removing it sends an explicit empty description.",
				Optional:            true,
			},
			"time_zone": resourceschema.StringAttribute{
				MarkdownDescription: "IANA time-zone identifier used by the data table.",
				Required:            true,
			},
			"value_lock_level": resourceschema.StringAttribute{
				MarkdownDescription: "Value concurrency lock level: NONE, DATA_TABLE, PRIMARY_VALUE, ATTRIBUTE, or VALUE.",
				Required:            true,
				Validators:          []validator.String{stringEnumValidator{attributeName: "value_lock_level", allowed: []string{"NONE", "DATA_TABLE", "PRIMARY_VALUE", "ATTRIBUTE", "VALUE"}}},
			},
			"status": resourceschema.StringAttribute{
				MarkdownDescription: "Data-table status. The pinned Amazon Connect SDK supports PUBLISHED.",
				Required:            true,
				Validators:          []validator.String{stringEnumValidator{attributeName: "status", allowed: []string{"PUBLISHED"}}},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"attributes": resourceschema.MapNestedAttribute{
				MarkdownDescription: "Complete data-table attribute schema keyed by attribute name. Removing a key deletes that remote attribute and all its values.",
				Required:            true,
				NestedObject: resourceschema.NestedAttributeObject{Attributes: map[string]resourceschema.Attribute{
					"value_type": resourceschema.StringAttribute{
						MarkdownDescription: "Attribute value type: TEXT, NUMBER, BOOLEAN, TEXT_LIST, or NUMBER_LIST.",
						Required:            true,
						Validators:          []validator.String{stringEnumValidator{attributeName: "value_type", allowed: []string{"TEXT", "NUMBER", "BOOLEAN", "TEXT_LIST", "NUMBER_LIST"}}},
					},
					"description": resourceschema.StringAttribute{
						MarkdownDescription: "Optional attribute description.",
						Optional:            true,
					},
					"primary": resourceschema.BoolAttribute{
						MarkdownDescription: "Whether the attribute participates in the record primary key.",
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
					},
				}},
			},
			"default_values": resourceschema.MapAttribute{
				MarkdownDescription: "Explicit DEFAULT record values keyed by declared non-primary attribute name. Omitting a key means no stored default value.",
				Optional:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

func (r *dataTableResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{dataTableConfigValidator{}}
}

func (r *dataTableResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	factory, ok := req.ProviderData.(clientFactory)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected an Amazon Connect client factory, got %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	r.client = factory.Connect()
}

func (r *dataTableResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var prior dataTableModel
	var planned dataTableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	if resp.Diagnostics.HasError() || prior.Attributes.IsNull() || prior.Attributes.IsUnknown() || planned.Attributes.IsNull() || planned.Attributes.IsUnknown() {
		return
	}
	for name, priorElement := range prior.Attributes.Elements() {
		plannedElement, retained := planned.Attributes.Elements()[name]
		if !retained {
			continue
		}
		before, beforeKnown := dataTablePlannedPrimary(priorElement)
		after, afterKnown := dataTablePlannedPrimary(plannedElement)
		if beforeKnown && afterKnown && before && !after {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root("attributes").AtMapKey(name).AtName("primary"))
		}
	}
}

func dataTablePlannedPrimary(value attr.Value) (bool, bool) {
	object, ok := value.(types.Object)
	if !ok || object.IsNull() || object.IsUnknown() {
		return false, false
	}
	primary, ok := object.Attributes()["primary"].(types.Bool)
	if !ok || primary.IsNull() || primary.IsUnknown() {
		return false, false
	}
	return primary.ValueBool(), true
}

func (r *dataTableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var planned dataTableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	if resp.Diagnostics.HasError() || !r.requireClient(&resp.Diagnostics) {
		return
	}
	configuration, diagnostics := dataTableConfigurationFromTerraform(ctx, planned, false)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	output, err := r.client.CreateDataTable(ctx, &awsconnect.CreateDataTableInput{
		InstanceId:     aws.String(configuration.instanceID),
		Name:           aws.String(configuration.name),
		Description:    configuration.description,
		TimeZone:       aws.String(configuration.timeZone),
		ValueLockLevel: connecttypes.DataTableLockLevel(configuration.valueLockLevel),
		Status:         connecttypes.DataTableStatus(configuration.status),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Data Table", fmt.Sprintf("Could not create data table %q: %s", configuration.name, err))
		return
	}
	if output == nil || aws.ToString(output.Id) == "" {
		resp.Diagnostics.AddError("Invalid Create Data Table Response", "Amazon Connect returned no data-table identifier after creation.")
		return
	}
	planned.ID = types.StringValue(aws.ToString(output.Id))
	if arn := aws.ToString(output.Arn); arn != "" {
		planned.ARN = types.StringValue(arn)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &planned)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key := dataTableKey{instanceID: configuration.instanceID, dataTableID: aws.ToString(output.Id)}
	err = r.coordinator.withLock(key, func() error {
		if err := r.createAttributes(ctx, key, configuration.attributes); err != nil {
			return err
		}
		if err := r.createDefaultValues(ctx, key, configuration.defaultValues); err != nil {
			return err
		}
		refreshed, err := r.readRemote(ctx, key)
		if err != nil {
			return err
		}
		refreshed.InstanceID = planned.InstanceID
		resp.Diagnostics.Append(resp.State.Set(ctx, &refreshed)...)
		return nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Complete Data Table Creation", err.Error())
	}
}

func (r *dataTableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dataTableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || !r.requireClient(&resp.Diagnostics) {
		return
	}
	key, diagnostics := dataTableIdentity(state)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.coordinator.withLock(key, func() error {
		refreshed, err := r.readRemote(ctx, key)
		if err != nil {
			return err
		}
		refreshed.InstanceID = state.InstanceID
		resp.Diagnostics.Append(resp.State.Set(ctx, &refreshed)...)
		return nil
	})
	if isDataTableNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Data Table", fmt.Sprintf("Could not read data table %q: %s", key.dataTableID, err))
	}
}

func (r *dataTableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var prior dataTableModel
	var planned dataTableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	if resp.Diagnostics.HasError() || !r.requireClient(&resp.Diagnostics) {
		return
	}
	if planned.ID.IsNull() || planned.ID.IsUnknown() {
		planned.ID = prior.ID
	}
	if planned.ARN.IsNull() || planned.ARN.IsUnknown() {
		planned.ARN = prior.ARN
	}
	configuration, diagnostics := dataTableConfigurationFromTerraform(ctx, planned, true)
	resp.Diagnostics.Append(diagnostics...)
	key, identityDiagnostics := dataTableIdentity(planned)
	resp.Diagnostics.Append(identityDiagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.coordinator.withLock(key, func() error {
		remote, err := r.readRemoteSnapshot(ctx, key)
		if err != nil {
			return err
		}
		if _, err := r.client.UpdateDataTableMetadata(ctx, &awsconnect.UpdateDataTableMetadataInput{
			DataTableId:    aws.String(key.dataTableID),
			InstanceId:     aws.String(key.instanceID),
			Name:           aws.String(configuration.name),
			Description:    configuration.description,
			TimeZone:       aws.String(configuration.timeZone),
			ValueLockLevel: connecttypes.DataTableLockLevel(configuration.valueLockLevel),
		}); err != nil {
			return fmt.Errorf("could not update data-table metadata: %w", err)
		}
		if err := r.reconcileAttributesBeforeDefaults(ctx, key, remote.attributes, configuration.attributes); err != nil {
			return err
		}
		remote, err = r.readRemoteSnapshot(ctx, key)
		if err != nil {
			return fmt.Errorf("could not refresh data-table locks before DEFAULT reconciliation: %w", err)
		}
		if err := r.reconcileDefaultValues(ctx, key, remote.defaultValues, configuration.defaultValues); err != nil {
			return err
		}
		if err := r.deleteRemovedAttributes(ctx, key, remote.attributes, configuration.attributes); err != nil {
			return err
		}
		refreshed, err := r.readRemote(ctx, key)
		if err != nil {
			return err
		}
		refreshed.InstanceID = planned.InstanceID
		resp.Diagnostics.Append(resp.State.Set(ctx, &refreshed)...)
		return nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update Data Table", fmt.Sprintf("Could not reconcile data table %q: %s", key.dataTableID, err))
	}
}

func (r *dataTableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dataTableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || !r.requireClient(&resp.Diagnostics) {
		return
	}
	key, diagnostics := dataTableIdentity(state)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.coordinator.withLock(key, func() error {
		_, err := r.client.DeleteDataTable(ctx, &awsconnect.DeleteDataTableInput{DataTableId: aws.String(key.dataTableID), InstanceId: aws.String(key.instanceID)})
		return err
	})
	if err != nil && !isDataTableNotFound(err) {
		resp.Diagnostics.AddError("Unable to Delete Data Table", fmt.Sprintf("Could not delete data table %q: %s", key.dataTableID, err))
	}
}

func (r *dataTableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Unexpected Import Identifier", fmt.Sprintf("expected import identifier with format instance_id:data_table_id; got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("instance_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

func (r *dataTableResource) requireClient(diagnostics *diag.Diagnostics) bool {
	if r.client != nil {
		return true
	}
	diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
	return false
}

type dataTableAttributeConfiguration struct {
	valueType   string
	description *string
	primary     bool
}

type dataTableConfiguration struct {
	instanceID     string
	name           string
	description    *string
	timeZone       string
	valueLockLevel string
	status         string
	attributes     map[string]dataTableAttributeConfiguration
	defaultValues  map[string]string
}

func dataTableConfigurationFromTerraform(ctx context.Context, model dataTableModel, updating bool) (dataTableConfiguration, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	instanceID, current := dataTableRequiredString(model.InstanceID, path.Root("instance_id"))
	diagnostics.Append(current...)
	name, current := dataTableRequiredString(model.Name, path.Root("name"))
	diagnostics.Append(current...)
	timeZone, current := dataTableRequiredString(model.TimeZone, path.Root("time_zone"))
	diagnostics.Append(current...)
	valueLockLevel, current := dataTableRequiredString(model.ValueLockLevel, path.Root("value_lock_level"))
	diagnostics.Append(current...)
	status, current := dataTableRequiredString(model.Status, path.Root("status"))
	diagnostics.Append(current...)
	description, current := dataTableDescription(model.Description, updating)
	diagnostics.Append(current...)
	attributes, current := dataTableAttributesFromTerraform(ctx, model.Attributes, updating)
	diagnostics.Append(current...)
	defaultValues, current := dataTableDefaultValuesFromTerraform(ctx, model.DefaultValues)
	diagnostics.Append(current...)
	for name := range defaultValues {
		attribute, declared := attributes[name]
		if !declared {
			diagnostics.AddAttributeError(path.Root("default_values").AtMapKey(name), "Undeclared DEFAULT Attribute", fmt.Sprintf("default_values key %q must also be declared in attributes", name))
		} else if attribute.primary {
			diagnostics.AddAttributeError(path.Root("default_values").AtMapKey(name), "Primary Attribute Cannot Have DEFAULT Value", fmt.Sprintf("default_values key %q refers to a primary attribute", name))
		}
	}
	return dataTableConfiguration{instanceID: instanceID, name: name, description: description, timeZone: timeZone, valueLockLevel: valueLockLevel, status: status, attributes: attributes, defaultValues: defaultValues}, diagnostics
}

func dataTableRequiredString(value types.String, attributePath path.Path) (string, diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return "", diag.Diagnostics{diag.NewAttributeErrorDiagnostic(attributePath, "Unknown Required Data Table Value", "the value must be known and non-null before an Amazon Connect request can be sent")}
	}
	return value.ValueString(), nil
}

func dataTableDescription(value types.String, updating bool) (*string, diag.Diagnostics) {
	if value.IsUnknown() {
		return nil, diag.Diagnostics{diag.NewAttributeErrorDiagnostic(path.Root("description"), "Unknown Data Table Description", "description must be known before an Amazon Connect request can be sent")}
	}
	if value.IsNull() {
		if updating {
			return aws.String(""), nil
		}
		return nil, nil
	}
	return aws.String(value.ValueString()), nil
}

func dataTableAttributesFromTerraform(ctx context.Context, value types.Map, updating bool) (map[string]dataTableAttributeConfiguration, diag.Diagnostics) {
	result := make(map[string]dataTableAttributeConfiguration)
	if value.IsNull() || value.IsUnknown() {
		return result, diag.Diagnostics{diag.NewAttributeErrorDiagnostic(path.Root("attributes"), "Unknown Data Table Attributes", "attributes must be known and non-null before an Amazon Connect request can be sent")}
	}
	var diagnostics diag.Diagnostics
	for name, element := range value.Elements() {
		object, ok := element.(types.Object)
		if !ok || object.IsNull() || object.IsUnknown() {
			diagnostics.AddAttributeError(path.Root("attributes").AtMapKey(name), "Invalid Data Table Attribute", "the attribute must be a known, non-null object before an Amazon Connect request can be sent")
			continue
		}
		var model dataTableAttributeModel
		current := object.As(ctx, &model, basetypes.ObjectAsOptions{})
		diagnostics.Append(current...)
		if current.HasError() {
			continue
		}
		valueType, current := dataTableRequiredString(model.ValueType, path.Root("attributes").AtMapKey(name).AtName("value_type"))
		diagnostics.Append(current...)
		if model.Primary.IsNull() || model.Primary.IsUnknown() {
			diagnostics.AddAttributeError(path.Root("attributes").AtMapKey(name).AtName("primary"), "Unknown Data Table Attribute Primary Flag", "primary must be known and non-null before an Amazon Connect request can be sent")
			continue
		}
		description, current := dataTableAttributeDescription(model.Description, path.Root("attributes").AtMapKey(name).AtName("description"), updating)
		diagnostics.Append(current...)
		if current.HasError() {
			continue
		}
		result[name] = dataTableAttributeConfiguration{valueType: valueType, description: description, primary: model.Primary.ValueBool()}
	}
	return result, diagnostics
}

func dataTableAttributeDescription(value types.String, attributePath path.Path, updating bool) (*string, diag.Diagnostics) {
	if value.IsUnknown() {
		return nil, diag.Diagnostics{diag.NewAttributeErrorDiagnostic(attributePath, "Unknown Data Table Attribute Description", "the description must be known before an Amazon Connect request can be sent")}
	}
	if value.IsNull() {
		if updating {
			return aws.String(""), nil
		}
		return nil, nil
	}
	return aws.String(value.ValueString()), nil
}

func dataTableDefaultValuesFromTerraform(ctx context.Context, value types.Map) (map[string]string, diag.Diagnostics) {
	result := make(map[string]string)
	if value.IsNull() {
		return result, nil
	}
	if value.IsUnknown() {
		return result, diag.Diagnostics{diag.NewAttributeErrorDiagnostic(path.Root("default_values"), "Unknown Data Table DEFAULT Values", "default_values must be known before an Amazon Connect request can be sent")}
	}
	var diagnostics diag.Diagnostics
	for name, element := range value.Elements() {
		stringValue, ok := element.(types.String)
		if !ok || stringValue.IsNull() || stringValue.IsUnknown() {
			diagnostics.AddAttributeError(path.Root("default_values").AtMapKey(name), "Invalid Data Table DEFAULT Value", "the DEFAULT value must be a known, non-null string before an Amazon Connect request can be sent")
			continue
		}
		result[name] = stringValue.ValueString()
	}
	_ = ctx
	return result, diagnostics
}

func dataTableIdentity(model dataTableModel) (dataTableKey, diag.Diagnostics) {
	instanceID, instanceDiagnostics := dataTableRequiredString(model.InstanceID, path.Root("instance_id"))
	id, idDiagnostics := dataTableRequiredString(model.ID, path.Root("id"))
	var diagnostics diag.Diagnostics
	diagnostics.Append(instanceDiagnostics...)
	diagnostics.Append(idDiagnostics...)
	return dataTableKey{instanceID: instanceID, dataTableID: id}, diagnostics
}

type dataTableRemoteDefault struct {
	value       string
	lockVersion *connecttypes.DataTableLockVersion
}

type dataTableRemoteSnapshot struct {
	table         connecttypes.DataTable
	attributes    map[string]dataTableAttributeConfiguration
	defaultValues map[string]dataTableRemoteDefault
}

func (r *dataTableResource) readRemoteSnapshot(ctx context.Context, key dataTableKey) (dataTableRemoteSnapshot, error) {
	described, err := r.client.DescribeDataTable(ctx, &awsconnect.DescribeDataTableInput{DataTableId: aws.String(key.dataTableID), InstanceId: aws.String(key.instanceID)})
	if err != nil {
		return dataTableRemoteSnapshot{}, err
	}
	if described == nil || described.DataTable == nil {
		return dataTableRemoteSnapshot{}, errors.New("amazon Connect returned no data-table details")
	}
	if described.DataTable.Status != connecttypes.DataTableStatusPublished {
		return dataTableRemoteSnapshot{}, fmt.Errorf("unsupported remote data-table status %q; this provider version supports only PUBLISHED", described.DataTable.Status)
	}
	attributes := make(map[string]dataTableAttributeConfiguration)
	var nextToken *string
	seenAttributeTokens := make(map[string]struct{})
	for {
		page, err := r.client.ListDataTableAttributes(ctx, &awsconnect.ListDataTableAttributesInput{DataTableId: aws.String(key.dataTableID), InstanceId: aws.String(key.instanceID), NextToken: nextToken})
		if err != nil {
			return dataTableRemoteSnapshot{}, fmt.Errorf("could not list data-table attributes: %w", err)
		}
		if page == nil {
			return dataTableRemoteSnapshot{}, errors.New("amazon Connect returned no data-table attribute page")
		}
		for _, attribute := range page.Attributes {
			name := aws.ToString(attribute.Name)
			if name == "" {
				return dataTableRemoteSnapshot{}, errors.New("amazon Connect returned an attribute without a name")
			}
			if _, duplicate := attributes[name]; duplicate {
				return dataTableRemoteSnapshot{}, fmt.Errorf("amazon Connect returned duplicate attribute %q", name)
			}
			attributes[name] = dataTableAttributeConfiguration{valueType: string(attribute.ValueType), description: attribute.Description, primary: attribute.Primary}
		}
		nextToken = page.NextToken
		token := aws.ToString(nextToken)
		if token == "" {
			break
		}
		if _, repeated := seenAttributeTokens[token]; repeated {
			return dataTableRemoteSnapshot{}, fmt.Errorf("amazon Connect repeated data-table attribute pagination token %q", token)
		}
		seenAttributeTokens[token] = struct{}{}
	}
	defaultValues := make(map[string]dataTableRemoteDefault)
	nextToken = nil
	seenValueTokens := make(map[string]struct{})
	for {
		page, err := r.client.ListDataTableValues(ctx, &awsconnect.ListDataTableValuesInput{DataTableId: aws.String(key.dataTableID), InstanceId: aws.String(key.instanceID), NextToken: nextToken})
		if err != nil {
			return dataTableRemoteSnapshot{}, fmt.Errorf("could not list data-table values: %w", err)
		}
		if page == nil {
			return dataTableRemoteSnapshot{}, errors.New("amazon Connect returned no data-table value page")
		}
		for _, value := range page.Values {
			if aws.ToString(value.RecordId) != defaultDataTableRecordID {
				continue
			}
			name := aws.ToString(value.AttributeName)
			if name == "" {
				return dataTableRemoteSnapshot{}, errors.New("amazon Connect returned a DEFAULT value without an attribute name")
			}
			if _, duplicate := defaultValues[name]; duplicate {
				return dataTableRemoteSnapshot{}, fmt.Errorf("amazon Connect returned duplicate DEFAULT value for attribute %q", name)
			}
			defaultValues[name] = dataTableRemoteDefault{value: aws.ToString(value.Value), lockVersion: value.LockVersion}
		}
		nextToken = page.NextToken
		token := aws.ToString(nextToken)
		if token == "" {
			break
		}
		if _, repeated := seenValueTokens[token]; repeated {
			return dataTableRemoteSnapshot{}, fmt.Errorf("amazon Connect repeated data-table value pagination token %q", token)
		}
		seenValueTokens[token] = struct{}{}
	}
	return dataTableRemoteSnapshot{table: *described.DataTable, attributes: attributes, defaultValues: defaultValues}, nil
}

func (r *dataTableResource) readRemote(ctx context.Context, key dataTableKey) (dataTableModel, error) {
	remote, err := r.readRemoteSnapshot(ctx, key)
	if err != nil {
		return dataTableModel{}, err
	}
	attributeValues := make(map[string]attr.Value, len(remote.attributes))
	for name, attribute := range remote.attributes {
		description := types.StringNull()
		if attribute.description != nil && aws.ToString(attribute.description) != "" {
			description = types.StringValue(aws.ToString(attribute.description))
		}
		attributeValues[name] = types.ObjectValueMust(dataTableAttributeTypes, map[string]attr.Value{
			"value_type":  types.StringValue(attribute.valueType),
			"description": description,
			"primary":     types.BoolValue(attribute.primary),
		})
	}
	attributes, diagnostics := types.MapValue(types.ObjectType{AttrTypes: dataTableAttributeTypes}, attributeValues)
	if diagnostics.HasError() {
		return dataTableModel{}, fmt.Errorf("could not map data-table attributes: %s", diagnostics.Errors()[0].Detail())
	}
	defaultElements := make(map[string]attr.Value, len(remote.defaultValues))
	for name, value := range remote.defaultValues {
		defaultElements[name] = types.StringValue(value.value)
	}
	defaultValues := types.MapNull(types.StringType)
	if len(defaultElements) != 0 {
		var diagnostics diag.Diagnostics
		defaultValues, diagnostics = types.MapValue(types.StringType, defaultElements)
		if diagnostics.HasError() {
			return dataTableModel{}, fmt.Errorf("could not map data-table DEFAULT values: %s", diagnostics.Errors()[0].Detail())
		}
	}
	description := types.StringNull()
	if remote.table.Description != nil && aws.ToString(remote.table.Description) != "" {
		description = types.StringValue(aws.ToString(remote.table.Description))
	}
	return dataTableModel{
		InstanceID:     types.StringValue(key.instanceID),
		ID:             types.StringValue(key.dataTableID),
		ARN:            stringValueOrNull(remote.table.Arn),
		Name:           stringValueOrNull(remote.table.Name),
		Description:    description,
		TimeZone:       stringValueOrNull(remote.table.TimeZone),
		ValueLockLevel: types.StringValue(string(remote.table.ValueLockLevel)),
		Status:         types.StringValue(string(remote.table.Status)),
		Attributes:     attributes,
		DefaultValues:  defaultValues,
	}, nil
}

func (r *dataTableResource) createAttributes(ctx context.Context, key dataTableKey, attributes map[string]dataTableAttributeConfiguration) error {
	for _, primary := range []bool{true, false} {
		for _, name := range sortedDataTableAttributeNames(attributes) {
			attribute := attributes[name]
			if attribute.primary != primary {
				continue
			}
			if _, err := r.client.CreateDataTableAttribute(ctx, dataTableCreateAttributeInput(key, name, attribute)); err != nil {
				return fmt.Errorf("could not create attribute %q: %w", name, err)
			}
		}
	}
	return nil
}

func (r *dataTableResource) reconcileAttributesBeforeDefaults(ctx context.Context, key dataTableKey, remote, desired map[string]dataTableAttributeConfiguration) error {
	for _, primary := range []bool{true, false} {
		for _, name := range sortedDataTableAttributeNames(desired) {
			attribute := desired[name]
			if attribute.primary != primary {
				continue
			}
			current, exists := remote[name]
			if !exists {
				if _, err := r.client.CreateDataTableAttribute(ctx, dataTableCreateAttributeInput(key, name, attribute)); err != nil {
					return fmt.Errorf("could not create attribute %q: %w", name, err)
				}
				continue
			}
			if dataTableAttributeEqual(current, attribute) {
				continue
			}
			if _, err := r.client.UpdateDataTableAttribute(ctx, &awsconnect.UpdateDataTableAttributeInput{
				AttributeName: aws.String(name), DataTableId: aws.String(key.dataTableID), InstanceId: aws.String(key.instanceID), Name: aws.String(name),
				Description: attribute.description, Primary: attribute.primary, ValueType: connecttypes.DataTableAttributeValueType(attribute.valueType),
			}); err != nil {
				return fmt.Errorf("could not update attribute %q: %w", name, err)
			}
		}
	}
	return nil
}

func dataTableCreateAttributeInput(key dataTableKey, name string, attribute dataTableAttributeConfiguration) *awsconnect.CreateDataTableAttributeInput {
	return &awsconnect.CreateDataTableAttributeInput{
		DataTableId: aws.String(key.dataTableID), InstanceId: aws.String(key.instanceID), Name: aws.String(name),
		Description: attribute.description, Primary: attribute.primary, ValueType: connecttypes.DataTableAttributeValueType(attribute.valueType),
	}
}

func (r *dataTableResource) deleteRemovedAttributes(ctx context.Context, key dataTableKey, remote, desired map[string]dataTableAttributeConfiguration) error {
	for _, name := range sortedDataTableAttributeNames(remote) {
		if _, retained := desired[name]; retained {
			continue
		}
		if _, err := r.client.DeleteDataTableAttribute(ctx, &awsconnect.DeleteDataTableAttributeInput{AttributeName: aws.String(name), DataTableId: aws.String(key.dataTableID), InstanceId: aws.String(key.instanceID)}); err != nil {
			return fmt.Errorf("could not delete attribute %q: %w", name, err)
		}
	}
	return nil
}

func (r *dataTableResource) createDefaultValues(ctx context.Context, key dataTableKey, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	requests := make([]connecttypes.DataTableValue, 0, len(values))
	for _, name := range sortedStringMapKeys(values) {
		requests = append(requests, connecttypes.DataTableValue{AttributeName: aws.String(name), Value: aws.String(values[name]), PrimaryValues: nil})
	}
	output, err := r.client.BatchCreateDataTableValue(ctx, &awsconnect.BatchCreateDataTableValueInput{DataTableId: aws.String(key.dataTableID), InstanceId: aws.String(key.instanceID), Values: requests})
	if err != nil {
		return fmt.Errorf("could not create DEFAULT values: %w", err)
	}
	if output == nil {
		return errors.New("amazon Connect returned no batch-create DEFAULT response")
	}
	if len(output.Failed) != 0 {
		return fmt.Errorf("batch create DEFAULT values failed: %s", formatCreateValueFailures(output.Failed))
	}
	return nil
}

func (r *dataTableResource) reconcileDefaultValues(ctx context.Context, key dataTableKey, remote map[string]dataTableRemoteDefault, desired map[string]string) error {
	creates := make(map[string]string)
	updates := make([]connecttypes.DataTableValue, 0)
	deletes := make([]connecttypes.DataTableDeleteValueIdentifier, 0)
	for _, name := range sortedStringMapKeys(desired) {
		current, exists := remote[name]
		if !exists {
			creates[name] = desired[name]
			continue
		}
		if current.value != desired[name] {
			if current.lockVersion == nil {
				return fmt.Errorf("default value %q has no lock version for update", name)
			}
			updates = append(updates, connecttypes.DataTableValue{AttributeName: aws.String(name), Value: aws.String(desired[name]), PrimaryValues: nil, LockVersion: current.lockVersion})
		}
	}
	for _, name := range sortedRemoteDefaultNames(remote) {
		if _, retained := desired[name]; retained {
			continue
		}
		if remote[name].lockVersion == nil {
			return fmt.Errorf("default value %q has no lock version for deletion", name)
		}
		deletes = append(deletes, connecttypes.DataTableDeleteValueIdentifier{AttributeName: aws.String(name), PrimaryValues: nil, LockVersion: remote[name].lockVersion})
	}
	if err := r.createDefaultValues(ctx, key, creates); err != nil {
		return err
	}
	if len(updates) != 0 {
		output, err := r.client.BatchUpdateDataTableValue(ctx, &awsconnect.BatchUpdateDataTableValueInput{DataTableId: aws.String(key.dataTableID), InstanceId: aws.String(key.instanceID), Values: updates})
		if err != nil {
			return fmt.Errorf("could not update DEFAULT values: %w", err)
		}
		if output == nil {
			return errors.New("amazon Connect returned no batch-update DEFAULT response")
		}
		if len(output.Failed) != 0 {
			return fmt.Errorf("batch update DEFAULT values failed: %s", formatUpdateValueFailures(output.Failed))
		}
	}
	if len(deletes) != 0 {
		output, err := r.client.BatchDeleteDataTableValue(ctx, &awsconnect.BatchDeleteDataTableValueInput{DataTableId: aws.String(key.dataTableID), InstanceId: aws.String(key.instanceID), Values: deletes})
		if err != nil {
			return fmt.Errorf("could not delete DEFAULT values: %w", err)
		}
		if output == nil {
			return errors.New("amazon Connect returned no batch-delete DEFAULT response")
		}
		if len(output.Failed) != 0 {
			return fmt.Errorf("batch delete DEFAULT values failed: %s", formatDeleteValueFailures(output.Failed))
		}
	}
	return nil
}

func dataTableAttributeEqual(left, right dataTableAttributeConfiguration) bool {
	return left.valueType == right.valueType && left.primary == right.primary && aws.ToString(left.description) == aws.ToString(right.description)
}

func sortedDataTableAttributeNames(values map[string]dataTableAttributeConfiguration) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedStringMapKeys(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedRemoteDefaultNames(values map[string]dataTableRemoteDefault) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func formatCreateValueFailures(failures []connecttypes.BatchCreateDataTableValueFailureResult) string {
	values := make([]string, 0, len(failures))
	for _, failure := range failures {
		values = append(values, fmt.Sprintf("%s: %s", aws.ToString(failure.AttributeName), aws.ToString(failure.Message)))
	}
	sort.Strings(values)
	return strings.Join(values, "; ")
}

func formatUpdateValueFailures(failures []connecttypes.BatchUpdateDataTableValueFailureResult) string {
	values := make([]string, 0, len(failures))
	for _, failure := range failures {
		values = append(values, fmt.Sprintf("%s: %s", aws.ToString(failure.AttributeName), aws.ToString(failure.Message)))
	}
	sort.Strings(values)
	return strings.Join(values, "; ")
}

func formatDeleteValueFailures(failures []connecttypes.BatchDeleteDataTableValueFailureResult) string {
	values := make([]string, 0, len(failures))
	for _, failure := range failures {
		values = append(values, fmt.Sprintf("%s: %s", aws.ToString(failure.AttributeName), aws.ToString(failure.Message)))
	}
	sort.Strings(values)
	return strings.Join(values, "; ")
}

func isDataTableNotFound(err error) bool {
	if err == nil {
		return false
	}
	var notFound *connecttypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

type dataTableConfigValidator struct{}

func (dataTableConfigValidator) Description(context.Context) string {
	return "DEFAULT values must reference declared non-primary attributes, and collection elements must be known and non-null when applied."
}

func (dataTableConfigValidator) MarkdownDescription(ctx context.Context) string {
	return dataTableConfigValidator{}.Description(ctx)
}

func (dataTableConfigValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var attributes types.Map
	var defaultValues types.Map
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("attributes"), &attributes)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("default_values"), &defaultValues)...)
	if resp.Diagnostics.HasError() || attributes.IsNull() || attributes.IsUnknown() {
		return
	}
	primary := make(map[string]bool)
	for name, element := range attributes.Elements() {
		object, ok := element.(types.Object)
		if !ok || object.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("attributes").AtMapKey(name), "Null Data Table Attribute", "attributes cannot contain null elements")
			continue
		}
		if object.IsUnknown() {
			continue
		}
		value, ok := object.Attributes()["primary"].(types.Bool)
		if ok && !value.IsNull() && !value.IsUnknown() {
			primary[name] = value.ValueBool()
		}
	}
	if defaultValues.IsNull() || defaultValues.IsUnknown() {
		return
	}
	for name, element := range defaultValues.Elements() {
		if element.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("default_values").AtMapKey(name), "Null Data Table DEFAULT Value", "default_values cannot contain null elements")
			continue
		}
		isPrimary, declared := primary[name]
		if !declared {
			if _, exists := attributes.Elements()[name]; !exists {
				resp.Diagnostics.AddAttributeError(path.Root("default_values").AtMapKey(name), "Undeclared DEFAULT Attribute", fmt.Sprintf("default_values key %q must also be declared in attributes", name))
			}
			continue
		}
		if isPrimary {
			resp.Diagnostics.AddAttributeError(path.Root("default_values").AtMapKey(name), "Primary Attribute Cannot Have DEFAULT Value", fmt.Sprintf("default_values key %q refers to a primary attribute", name))
		}
	}
}
