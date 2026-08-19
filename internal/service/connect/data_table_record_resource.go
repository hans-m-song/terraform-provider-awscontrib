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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &dataTableRecordResource{}
var _ resource.ResourceWithConfigure = &dataTableRecordResource{}
var _ resource.ResourceWithConfigValidators = &dataTableRecordResource{}

type dataTableRecordClient interface {
	BatchCreateDataTableValue(context.Context, *awsconnect.BatchCreateDataTableValueInput, ...func(*awsconnect.Options)) (*awsconnect.BatchCreateDataTableValueOutput, error)
	BatchUpdateDataTableValue(context.Context, *awsconnect.BatchUpdateDataTableValueInput, ...func(*awsconnect.Options)) (*awsconnect.BatchUpdateDataTableValueOutput, error)
	BatchDeleteDataTableValue(context.Context, *awsconnect.BatchDeleteDataTableValueInput, ...func(*awsconnect.Options)) (*awsconnect.BatchDeleteDataTableValueOutput, error)
	ListDataTablePrimaryValues(context.Context, *awsconnect.ListDataTablePrimaryValuesInput, ...func(*awsconnect.Options)) (*awsconnect.ListDataTablePrimaryValuesOutput, error)
	ListDataTableValues(context.Context, *awsconnect.ListDataTableValuesInput, ...func(*awsconnect.Options)) (*awsconnect.ListDataTableValuesOutput, error)
}

type dataTableRecordResource struct {
	client      dataTableRecordClient
	coordinator *dataTableCoordinator
}

type dataTableRecordModel struct {
	InstanceID    types.String `tfsdk:"instance_id"`
	DataTableID   types.String `tfsdk:"data_table_id"`
	PrimaryValues types.Map    `tfsdk:"primary_values"`
	Values        types.Map    `tfsdk:"values"`
	RecordID      types.String `tfsdk:"record_id"`
}

type dataTableRecordConfiguration struct {
	key           dataTableKey
	primaryValues map[string]string
	values        map[string]string
}

type dataTableRemoteRecord struct {
	recordID string
	values   map[string]dataTableRemoteRecordValue
}

type dataTableRemoteRecordValue struct {
	value       string
	lockVersion *connecttypes.DataTableLockVersion
}

var errDataTableRecordNotFound = errors.New("data-table record not found")

func NewDataTableRecordResource() resource.Resource {
	return &dataTableRecordResource{coordinator: newDataTableCoordinator()}
}

func (r *dataTableRecordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connect_data_table_record"
}

func (r *dataTableRecordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		MarkdownDescription: "Manages one non-default Amazon Connect data-table record. The values map is authoritative for every value in the remote record, and same-table mutations are serialized within one provider process.",
		Attributes: map[string]resourceschema.Attribute{
			"instance_id": resourceschema.StringAttribute{
				MarkdownDescription: "Amazon Connect instance identifier.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"data_table_id": resourceschema.StringAttribute{
				MarkdownDescription: "Amazon Connect data-table identifier.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"primary_values": resourceschema.MapAttribute{
				MarkdownDescription: "Nonempty composite primary key, keyed by primary attribute name. Changing it replaces the record.",
				Required:            true,
				ElementType:         types.StringType,
				PlanModifiers:       []planmodifier.Map{requiresReplaceMapPlanModifier{}},
			},
			"values": resourceschema.MapAttribute{
				MarkdownDescription: "Nonempty authoritative set of record values keyed by non-primary attribute name. Remote values absent from configuration are deleted.",
				Required:            true,
				ElementType:         types.StringType,
			},
			"record_id": resourceschema.StringAttribute{
				MarkdownDescription: "Amazon Connect record identifier derived from the composite primary key.",
				Computed:            true,
			},
		},
	}
}

func (r *dataTableRecordResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{dataTableRecordConfigValidator{}}
}

func (r *dataTableRecordResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *dataTableRecordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var planned dataTableRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	if resp.Diagnostics.HasError() || !r.requireClient(&resp.Diagnostics) {
		return
	}
	configuration, diagnostics := dataTableRecordConfigurationFromTerraform(planned)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.coordinator.withLock(configuration.key, func() error {
		output, err := r.client.BatchCreateDataTableValue(ctx, &awsconnect.BatchCreateDataTableValueInput{
			DataTableId: aws.String(configuration.key.dataTableID),
			InstanceId:  aws.String(configuration.key.instanceID),
			Values:      dataTableRecordCreateValues(configuration.primaryValues, configuration.values),
		})
		if err != nil {
			return fmt.Errorf("could not create record values: %w", err)
		}
		if output == nil {
			return errors.New("amazon Connect returned no batch-create record response")
		}
		recordID, successfulValues, identityErr := dataTableRecordCreateIdentity(output.Successful, configuration.primaryValues, configuration.values)
		if recordID != "" {
			recoverable := dataTableRecordModel{
				InstanceID: planned.InstanceID, DataTableID: planned.DataTableID, PrimaryValues: planned.PrimaryValues,
				Values: stringMapValue(successfulValues), RecordID: types.StringValue(recordID),
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, &recoverable)...)
		}
		if identityErr != nil {
			if len(output.Failed) != 0 {
				return fmt.Errorf("%v; batch create record values failed: %s", identityErr, formatCreateValueFailures(output.Failed))
			}
			return identityErr
		}
		if len(output.Failed) != 0 {
			return fmt.Errorf("batch create record values failed: %s", formatCreateValueFailures(output.Failed))
		}
		if len(successfulValues) != len(configuration.values) {
			return fmt.Errorf("amazon Connect reported %d successful record values for %d requested values", len(successfulValues), len(configuration.values))
		}
		remote, err := r.readRemoteRecordByID(ctx, configuration.key, recordID)
		if err != nil {
			return fmt.Errorf("could not refresh created record: %w", err)
		}
		return r.setState(ctx, resp.State.Set, planned, remote, &resp.Diagnostics)
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Data Table Record", err.Error())
	}
}

func (r *dataTableRecordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dataTableRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || !r.requireClient(&resp.Diagnostics) {
		return
	}
	configuration, diagnostics := dataTableRecordConfigurationFromTerraform(state)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.coordinator.withLock(configuration.key, func() error {
		remote, err := r.readRemoteRecord(ctx, configuration.key, configuration.primaryValues)
		if err != nil {
			return err
		}
		return r.setState(ctx, resp.State.Set, state, remote, &resp.Diagnostics)
	})
	if errors.Is(err, errDataTableRecordNotFound) || isDataTableNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Data Table Record", fmt.Sprintf("Could not read data-table record: %s", err))
	}
}

func (r *dataTableRecordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var prior dataTableRecordModel
	var planned dataTableRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	if resp.Diagnostics.HasError() || !r.requireClient(&resp.Diagnostics) {
		return
	}
	if planned.RecordID.IsNull() || planned.RecordID.IsUnknown() {
		planned.RecordID = prior.RecordID
	}
	configuration, diagnostics := dataTableRecordConfigurationFromTerraform(planned)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.coordinator.withLock(configuration.key, func() error {
		remote, err := r.readRemoteRecord(ctx, configuration.key, configuration.primaryValues)
		if err != nil {
			return err
		}
		if err := r.reconcileRecordValues(ctx, configuration, remote); err != nil {
			return err
		}
		refreshed, err := r.readRemoteRecord(ctx, configuration.key, configuration.primaryValues)
		if err != nil {
			return fmt.Errorf("could not refresh updated record: %w", err)
		}
		return r.setState(ctx, resp.State.Set, planned, refreshed, &resp.Diagnostics)
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update Data Table Record", fmt.Sprintf("Could not reconcile data-table record: %s", err))
	}
}

func (r *dataTableRecordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dataTableRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || !r.requireClient(&resp.Diagnostics) {
		return
	}
	configuration, diagnostics := dataTableRecordConfigurationFromTerraform(state)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.coordinator.withLock(configuration.key, func() error {
		remote, err := r.readRemoteRecord(ctx, configuration.key, configuration.primaryValues)
		if errors.Is(err, errDataTableRecordNotFound) || isDataTableNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		deletes, err := dataTableRecordDeleteValues(configuration.primaryValues, remote.values)
		if err != nil {
			return err
		}
		if len(deletes) == 0 {
			return nil
		}
		output, err := r.client.BatchDeleteDataTableValue(ctx, &awsconnect.BatchDeleteDataTableValueInput{
			DataTableId: aws.String(configuration.key.dataTableID), InstanceId: aws.String(configuration.key.instanceID), Values: deletes,
		})
		if isDataTableNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("could not delete record values: %w", err)
		}
		if output == nil {
			return errors.New("amazon Connect returned no batch-delete record response")
		}
		if len(output.Failed) != 0 {
			return fmt.Errorf("batch delete record values failed: %s", formatDeleteValueFailures(output.Failed))
		}
		if err := validateDataTableRecordDeleteSuccesses(output.Successful, configuration.primaryValues, deletes); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete Data Table Record", fmt.Sprintf("Could not delete data-table record: %s", err))
	}
}

func (r *dataTableRecordResource) requireClient(diagnostics *diag.Diagnostics) bool {
	if r.client != nil {
		return true
	}
	diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
	return false
}

type stateSetter func(context.Context, interface{}) diag.Diagnostics

func (r *dataTableRecordResource) setState(ctx context.Context, set stateSetter, identity dataTableRecordModel, remote dataTableRemoteRecord, diagnostics *diag.Diagnostics) error {
	model := dataTableRecordModel{
		InstanceID: identity.InstanceID, DataTableID: identity.DataTableID, PrimaryValues: identity.PrimaryValues,
		Values: stringMapValue(remoteValues(remote.values)), RecordID: types.StringValue(remote.recordID),
	}
	diagnostics.Append(set(ctx, &model)...)
	return nil
}

func dataTableRecordConfigurationFromTerraform(model dataTableRecordModel) (dataTableRecordConfiguration, diag.Diagnostics) {
	instanceID, instanceDiagnostics := dataTableRequiredString(model.InstanceID, path.Root("instance_id"))
	dataTableID, tableDiagnostics := dataTableRequiredString(model.DataTableID, path.Root("data_table_id"))
	primaryValues, primaryDiagnostics := knownStringMap(model.PrimaryValues, path.Root("primary_values"), "Data Table Record Primary Values")
	values, valueDiagnostics := knownStringMap(model.Values, path.Root("values"), "Data Table Record Values")
	var diagnostics diag.Diagnostics
	diagnostics.Append(instanceDiagnostics...)
	diagnostics.Append(tableDiagnostics...)
	diagnostics.Append(primaryDiagnostics...)
	diagnostics.Append(valueDiagnostics...)
	if len(primaryValues) == 0 && !primaryDiagnostics.HasError() {
		diagnostics.AddAttributeError(path.Root("primary_values"), "Empty Data Table Record Primary Values", "primary_values must contain at least one entry")
	}
	if len(values) == 0 && !valueDiagnostics.HasError() {
		diagnostics.AddAttributeError(path.Root("values"), "Empty Data Table Record Values", "values must contain at least one entry")
	}
	for name := range values {
		if _, primary := primaryValues[name]; primary {
			diagnostics.AddAttributeError(path.Root("values").AtMapKey(name), "Primary Attribute Cannot Be a Record Value", fmt.Sprintf("values key %q is also declared in primary_values", name))
		}
	}
	return dataTableRecordConfiguration{key: dataTableKey{instanceID: instanceID, dataTableID: dataTableID}, primaryValues: primaryValues, values: values}, diagnostics
}

func knownStringMap(value types.Map, attributePath path.Path, title string) (map[string]string, diag.Diagnostics) {
	result := make(map[string]string)
	if value.IsNull() || value.IsUnknown() {
		return result, diag.Diagnostics{diag.NewAttributeErrorDiagnostic(attributePath, "Unknown "+title, strings.ToLower(title)+" must be known and non-null before an Amazon Connect request can be sent")}
	}
	var diagnostics diag.Diagnostics
	for name, element := range value.Elements() {
		stringValue, ok := element.(types.String)
		if !ok || stringValue.IsNull() || stringValue.IsUnknown() {
			diagnostics.AddAttributeError(attributePath.AtMapKey(name), "Invalid "+title, "the map value must be a known, non-null string before an Amazon Connect request can be sent")
			continue
		}
		result[name] = stringValue.ValueString()
	}
	return result, diagnostics
}

func dataTableRecordPrimaryValues(values map[string]string) []connecttypes.PrimaryValue {
	result := make([]connecttypes.PrimaryValue, 0, len(values))
	for _, name := range sortedStringMapKeys(values) {
		result = append(result, connecttypes.PrimaryValue{AttributeName: aws.String(name), Value: aws.String(values[name])})
	}
	return result
}

func dataTableRecordPrimaryFilters(values map[string]string) []connecttypes.PrimaryAttributeValueFilter {
	result := make([]connecttypes.PrimaryAttributeValueFilter, 0, len(values))
	for _, name := range sortedStringMapKeys(values) {
		result = append(result, connecttypes.PrimaryAttributeValueFilter{AttributeName: aws.String(name), Values: []string{values[name]}})
	}
	return result
}

func dataTableRecordCreateValues(primaryValues, values map[string]string) []connecttypes.DataTableValue {
	primary := dataTableRecordPrimaryValues(primaryValues)
	result := make([]connecttypes.DataTableValue, 0, len(values))
	for _, name := range sortedStringMapKeys(values) {
		result = append(result, connecttypes.DataTableValue{AttributeName: aws.String(name), Value: aws.String(values[name]), PrimaryValues: primary})
	}
	return result
}

func dataTableRecordCreateIdentity(successes []connecttypes.BatchCreateDataTableValueSuccessResult, primaryValues, requested map[string]string) (string, map[string]string, error) {
	recordID := ""
	successful := make(map[string]string, len(successes))
	for _, success := range successes {
		name := aws.ToString(success.AttributeName)
		value, requestedValue := requested[name]
		if name == "" || !requestedValue {
			return "", successful, fmt.Errorf("amazon Connect reported an unexpected successful record attribute %q", name)
		}
		if _, duplicate := successful[name]; duplicate {
			return "", successful, fmt.Errorf("amazon Connect reported duplicate success for record attribute %q", name)
		}
		currentID := aws.ToString(success.RecordId)
		if currentID == "" || currentID == defaultDataTableRecordID {
			return "", successful, fmt.Errorf("amazon Connect returned invalid non-default record identifier %q", currentID)
		}
		if recordID != "" && recordID != currentID {
			return "", successful, fmt.Errorf("amazon Connect returned inconsistent record identifiers %q and %q", recordID, currentID)
		}
		if !primaryValueMapEqual(success.PrimaryValues, primaryValues) {
			return "", successful, fmt.Errorf("amazon Connect reported create success for record attribute %q with different primary values", name)
		}
		recordID = currentID
		successful[name] = value
	}
	if recordID == "" {
		return "", successful, errors.New("amazon Connect returned no successful non-default record identity")
	}
	return recordID, successful, nil
}

func (r *dataTableRecordResource) readRemoteRecord(ctx context.Context, key dataTableKey, primaryValues map[string]string) (dataTableRemoteRecord, error) {
	recordID, err := r.findRecordID(ctx, key, primaryValues)
	if err != nil {
		return dataTableRemoteRecord{}, err
	}
	return r.readRemoteRecordByID(ctx, key, recordID)
}

func (r *dataTableRecordResource) findRecordID(ctx context.Context, key dataTableKey, primaryValues map[string]string) (string, error) {
	var matches []string
	var nextToken *string
	seenTokens := make(map[string]struct{})
	for {
		page, err := r.client.ListDataTablePrimaryValues(ctx, &awsconnect.ListDataTablePrimaryValuesInput{
			DataTableId: aws.String(key.dataTableID), InstanceId: aws.String(key.instanceID),
			PrimaryAttributeValues: dataTableRecordPrimaryFilters(primaryValues), NextToken: nextToken,
		})
		if err != nil {
			return "", fmt.Errorf("could not list data-table primary values: %w", err)
		}
		if page == nil {
			return "", errors.New("amazon Connect returned no data-table primary-value page")
		}
		for _, candidate := range page.PrimaryValuesList {
			if !primaryValueResponseMapEqual(candidate.PrimaryValues, primaryValues) {
				continue
			}
			recordID := aws.ToString(candidate.RecordId)
			if recordID == "" || recordID == defaultDataTableRecordID {
				return "", fmt.Errorf("amazon Connect returned invalid non-default record identifier %q", recordID)
			}
			matches = append(matches, recordID)
		}
		nextToken = page.NextToken
		token := aws.ToString(nextToken)
		if token == "" {
			break
		}
		if _, repeated := seenTokens[token]; repeated {
			return "", fmt.Errorf("amazon Connect repeated data-table primary-value pagination token %q", token)
		}
		seenTokens[token] = struct{}{}
	}
	if len(matches) == 0 {
		return "", errDataTableRecordNotFound
	}
	if len(matches) != 1 {
		sort.Strings(matches)
		return "", fmt.Errorf("amazon Connect returned multiple records for the exact composite primary key: %s", strings.Join(matches, ", "))
	}
	return matches[0], nil
}

func primaryValueResponseMapEqual(candidate []connecttypes.PrimaryValueResponse, expected map[string]string) bool {
	if len(candidate) != len(expected) {
		return false
	}
	seen := make(map[string]string, len(candidate))
	for _, primary := range candidate {
		name := aws.ToString(primary.AttributeName)
		if name == "" {
			return false
		}
		if _, duplicate := seen[name]; duplicate {
			return false
		}
		seen[name] = aws.ToString(primary.Value)
	}
	for name, value := range expected {
		if seen[name] != value {
			return false
		}
	}
	return true
}

func (r *dataTableRecordResource) readRemoteRecordByID(ctx context.Context, key dataTableKey, recordID string) (dataTableRemoteRecord, error) {
	if recordID == "" || recordID == defaultDataTableRecordID {
		return dataTableRemoteRecord{}, fmt.Errorf("invalid non-default record identifier %q", recordID)
	}
	values := make(map[string]dataTableRemoteRecordValue)
	var nextToken *string
	seenTokens := make(map[string]struct{})
	for {
		page, err := r.client.ListDataTableValues(ctx, &awsconnect.ListDataTableValuesInput{
			DataTableId: aws.String(key.dataTableID), InstanceId: aws.String(key.instanceID), RecordIds: []string{recordID}, NextToken: nextToken,
		})
		if err != nil {
			return dataTableRemoteRecord{}, fmt.Errorf("could not list data-table record values: %w", err)
		}
		if page == nil {
			return dataTableRemoteRecord{}, errors.New("amazon Connect returned no data-table value page")
		}
		for _, remote := range page.Values {
			if aws.ToString(remote.RecordId) != recordID {
				continue
			}
			name := aws.ToString(remote.AttributeName)
			if name == "" {
				return dataTableRemoteRecord{}, errors.New("amazon Connect returned a record value without an attribute name")
			}
			if _, duplicate := values[name]; duplicate {
				return dataTableRemoteRecord{}, fmt.Errorf("amazon Connect returned duplicate record value for attribute %q", name)
			}
			values[name] = dataTableRemoteRecordValue{value: aws.ToString(remote.Value), lockVersion: remote.LockVersion}
		}
		nextToken = page.NextToken
		token := aws.ToString(nextToken)
		if token == "" {
			break
		}
		if _, repeated := seenTokens[token]; repeated {
			return dataTableRemoteRecord{}, fmt.Errorf("amazon Connect repeated data-table value pagination token %q", token)
		}
		seenTokens[token] = struct{}{}
	}
	if len(values) == 0 {
		return dataTableRemoteRecord{}, errDataTableRecordNotFound
	}
	return dataTableRemoteRecord{recordID: recordID, values: values}, nil
}

func (r *dataTableRecordResource) reconcileRecordValues(ctx context.Context, configuration dataTableRecordConfiguration, remote dataTableRemoteRecord) error {
	creates := make(map[string]string)
	updates := make([]connecttypes.DataTableValue, 0)
	deletes := make([]connecttypes.DataTableDeleteValueIdentifier, 0)
	primaryValues := dataTableRecordPrimaryValues(configuration.primaryValues)
	for _, name := range sortedStringMapKeys(configuration.values) {
		current, exists := remote.values[name]
		if !exists {
			creates[name] = configuration.values[name]
			continue
		}
		if current.value == configuration.values[name] {
			continue
		}
		if current.lockVersion == nil {
			return fmt.Errorf("record value %q has no lock version for update", name)
		}
		updates = append(updates, connecttypes.DataTableValue{
			AttributeName: aws.String(name), Value: aws.String(configuration.values[name]), PrimaryValues: primaryValues, LockVersion: current.lockVersion,
		})
	}
	for _, name := range sortedRemoteRecordValueNames(remote.values) {
		if _, retained := configuration.values[name]; retained {
			continue
		}
		current := remote.values[name]
		if current.lockVersion == nil {
			return fmt.Errorf("record value %q has no lock version for deletion", name)
		}
		deletes = append(deletes, connecttypes.DataTableDeleteValueIdentifier{AttributeName: aws.String(name), PrimaryValues: primaryValues, LockVersion: current.lockVersion})
	}
	if len(creates) != 0 {
		output, err := r.client.BatchCreateDataTableValue(ctx, &awsconnect.BatchCreateDataTableValueInput{
			DataTableId: aws.String(configuration.key.dataTableID), InstanceId: aws.String(configuration.key.instanceID),
			Values: dataTableRecordCreateValues(configuration.primaryValues, creates),
		})
		if err != nil {
			return fmt.Errorf("could not create missing record values: %w", err)
		}
		if output == nil {
			return errors.New("amazon Connect returned no batch-create record response")
		}
		if len(output.Failed) != 0 {
			return fmt.Errorf("batch create record values failed: %s", formatCreateValueFailures(output.Failed))
		}
		createdRecordID, successfulValues, err := dataTableRecordCreateIdentity(output.Successful, configuration.primaryValues, creates)
		if err != nil {
			return err
		}
		if createdRecordID != remote.recordID {
			return fmt.Errorf("amazon Connect created missing record values under record %q instead of managed record %q", createdRecordID, remote.recordID)
		}
		if len(successfulValues) != len(creates) {
			return fmt.Errorf("amazon Connect reported %d successful record creates for %d requested values", len(successfulValues), len(creates))
		}
	}
	if len(updates) != 0 {
		output, err := r.client.BatchUpdateDataTableValue(ctx, &awsconnect.BatchUpdateDataTableValueInput{
			DataTableId: aws.String(configuration.key.dataTableID), InstanceId: aws.String(configuration.key.instanceID), Values: updates,
		})
		if err != nil {
			return fmt.Errorf("could not update record values: %w", err)
		}
		if output == nil {
			return errors.New("amazon Connect returned no batch-update record response")
		}
		if len(output.Failed) != 0 {
			return fmt.Errorf("batch update record values failed: %s", formatUpdateValueFailures(output.Failed))
		}
		if err := validateDataTableRecordUpdateSuccesses(output.Successful, configuration.primaryValues, updates); err != nil {
			return err
		}
	}
	if len(deletes) != 0 {
		output, err := r.client.BatchDeleteDataTableValue(ctx, &awsconnect.BatchDeleteDataTableValueInput{
			DataTableId: aws.String(configuration.key.dataTableID), InstanceId: aws.String(configuration.key.instanceID), Values: deletes,
		})
		if err != nil {
			return fmt.Errorf("could not delete removed record values: %w", err)
		}
		if output == nil {
			return errors.New("amazon Connect returned no batch-delete record response")
		}
		if len(output.Failed) != 0 {
			return fmt.Errorf("batch delete record values failed: %s", formatDeleteValueFailures(output.Failed))
		}
		if err := validateDataTableRecordDeleteSuccesses(output.Successful, configuration.primaryValues, deletes); err != nil {
			return err
		}
	}
	return nil
}

func validateDataTableRecordUpdateSuccesses(successes []connecttypes.BatchUpdateDataTableValueSuccessResult, primaryValues map[string]string, requested []connecttypes.DataTableValue) error {
	requestedNames := make(map[string]struct{}, len(requested))
	for _, value := range requested {
		requestedNames[aws.ToString(value.AttributeName)] = struct{}{}
	}
	seen := make(map[string]struct{}, len(successes))
	for _, success := range successes {
		name := aws.ToString(success.AttributeName)
		if _, requested := requestedNames[name]; name == "" || !requested {
			return fmt.Errorf("amazon Connect reported an unexpected successful update for record attribute %q", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("amazon Connect reported duplicate update success for record attribute %q", name)
		}
		if !primaryValueMapEqual(success.PrimaryValues, primaryValues) {
			return fmt.Errorf("amazon Connect reported update success for record attribute %q with different primary values", name)
		}
		seen[name] = struct{}{}
	}
	if len(seen) != len(requestedNames) {
		return fmt.Errorf("amazon Connect reported %d successful record updates for %d requested values", len(seen), len(requestedNames))
	}
	return nil
}

func validateDataTableRecordDeleteSuccesses(successes []connecttypes.BatchDeleteDataTableValueSuccessResult, primaryValues map[string]string, requested []connecttypes.DataTableDeleteValueIdentifier) error {
	requestedNames := make(map[string]struct{}, len(requested))
	for _, value := range requested {
		requestedNames[aws.ToString(value.AttributeName)] = struct{}{}
	}
	seen := make(map[string]struct{}, len(successes))
	for _, success := range successes {
		name := aws.ToString(success.AttributeName)
		if _, requested := requestedNames[name]; name == "" || !requested {
			return fmt.Errorf("amazon Connect reported an unexpected successful deletion for record attribute %q", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("amazon Connect reported duplicate deletion success for record attribute %q", name)
		}
		if !primaryValueMapEqual(success.PrimaryValues, primaryValues) {
			return fmt.Errorf("amazon Connect reported deletion success for record attribute %q with different primary values", name)
		}
		seen[name] = struct{}{}
	}
	if len(seen) != len(requestedNames) {
		return fmt.Errorf("amazon Connect reported %d successful record deletions for %d requested values", len(seen), len(requestedNames))
	}
	return nil
}

func primaryValueMapEqual(candidate []connecttypes.PrimaryValue, expected map[string]string) bool {
	if len(candidate) != len(expected) {
		return false
	}
	seen := make(map[string]string, len(candidate))
	for _, primary := range candidate {
		name := aws.ToString(primary.AttributeName)
		if name == "" {
			return false
		}
		if _, duplicate := seen[name]; duplicate {
			return false
		}
		seen[name] = aws.ToString(primary.Value)
	}
	for name, value := range expected {
		if seen[name] != value {
			return false
		}
	}
	return true
}

func dataTableRecordDeleteValues(primaryValues map[string]string, values map[string]dataTableRemoteRecordValue) ([]connecttypes.DataTableDeleteValueIdentifier, error) {
	primary := dataTableRecordPrimaryValues(primaryValues)
	result := make([]connecttypes.DataTableDeleteValueIdentifier, 0, len(values))
	for _, name := range sortedRemoteRecordValueNames(values) {
		if values[name].lockVersion == nil {
			return nil, fmt.Errorf("record value %q has no lock version for deletion", name)
		}
		result = append(result, connecttypes.DataTableDeleteValueIdentifier{AttributeName: aws.String(name), PrimaryValues: primary, LockVersion: values[name].lockVersion})
	}
	return result, nil
}

func sortedRemoteRecordValueNames(values map[string]dataTableRemoteRecordValue) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func remoteValues(values map[string]dataTableRemoteRecordValue) map[string]string {
	result := make(map[string]string, len(values))
	for name, value := range values {
		result[name] = value.value
	}
	return result
}

func stringMapValue(values map[string]string) types.Map {
	elements := make(map[string]attr.Value, len(values))
	for name, value := range values {
		elements[name] = types.StringValue(value)
	}
	return types.MapValueMust(types.StringType, elements)
}

type dataTableRecordConfigValidator struct{}

func (dataTableRecordConfigValidator) Description(context.Context) string {
	return "Primary values and record values must be nonempty maps without null elements."
}

func (dataTableRecordConfigValidator) MarkdownDescription(ctx context.Context) string {
	return dataTableRecordConfigValidator{}.Description(ctx)
}

func (dataTableRecordConfigValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	knownMaps := make(map[string]types.Map, 2)
	for _, attributeName := range []string{"primary_values", "values"} {
		var value types.Map
		attributePath := path.Root(attributeName)
		current := req.Config.GetAttribute(ctx, attributePath, &value)
		resp.Diagnostics.Append(current...)
		if current.HasError() || value.IsNull() || value.IsUnknown() {
			continue
		}
		knownMaps[attributeName] = value
		if len(value.Elements()) == 0 {
			resp.Diagnostics.AddAttributeError(attributePath, "Empty Data Table Record Map", attributeName+" must contain at least one entry")
		}
		for name, element := range value.Elements() {
			if element.IsNull() {
				resp.Diagnostics.AddAttributeError(attributePath.AtMapKey(name), "Null Data Table Record Value", attributeName+" cannot contain null elements")
			}
		}
	}
	primaryValues, primaryKnown := knownMaps["primary_values"]
	values, valuesKnown := knownMaps["values"]
	if !primaryKnown || !valuesKnown {
		return
	}
	for name := range values.Elements() {
		if _, primary := primaryValues.Elements()[name]; primary {
			resp.Diagnostics.AddAttributeError(path.Root("values").AtMapKey(name), "Primary Attribute Cannot Be a Record Value", fmt.Sprintf("values key %q is also declared in primary_values", name))
		}
	}
}

type requiresReplaceMapPlanModifier struct{}

func (requiresReplaceMapPlanModifier) Description(context.Context) string {
	return "Changes to this map require replacement."
}

func (requiresReplaceMapPlanModifier) MarkdownDescription(ctx context.Context) string {
	return requiresReplaceMapPlanModifier{}.Description(ctx)
}

func (requiresReplaceMapPlanModifier) PlanModifyMap(_ context.Context, req planmodifier.MapRequest, resp *planmodifier.MapResponse) {
	if req.StateValue.IsNull() || req.PlanValue.IsNull() || req.StateValue.IsUnknown() || req.PlanValue.IsUnknown() {
		return
	}
	if !req.StateValue.Equal(req.PlanValue) {
		resp.RequiresReplace = true
	}
}
