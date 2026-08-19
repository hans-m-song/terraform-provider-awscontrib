package connect

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const (
	instanceID          = "00000000-0000-0000-0000-000000000010"
	queueID             = "00000000-0000-0000-0000-000000000011"
	quickConnectID1     = "00000000-0000-0000-0000-000000000001"
	quickConnectID2     = "00000000-0000-0000-0000-000000000002"
	quickConnectID3     = "00000000-0000-0000-0000-000000000003"
	quickConnectID4     = "00000000-0000-0000-0000-000000000004"
	otherQuickConnectID = "00000000-0000-0000-0000-000000000099"
)

type fakeAssociationClient struct {
	associate    func(context.Context, *awsconnect.AssociateQueueQuickConnectsInput) (*awsconnect.AssociateQueueQuickConnectsOutput, error)
	disassociate func(context.Context, *awsconnect.DisassociateQueueQuickConnectsInput) (*awsconnect.DisassociateQueueQuickConnectsOutput, error)
	list         func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error)
}

func (f *fakeAssociationClient) AssociateQueueQuickConnects(ctx context.Context, input *awsconnect.AssociateQueueQuickConnectsInput, _ ...func(*awsconnect.Options)) (*awsconnect.AssociateQueueQuickConnectsOutput, error) {
	if f.associate == nil {
		return &awsconnect.AssociateQueueQuickConnectsOutput{}, nil
	}
	return f.associate(ctx, input)
}

func (f *fakeAssociationClient) DisassociateQueueQuickConnects(ctx context.Context, input *awsconnect.DisassociateQueueQuickConnectsInput, _ ...func(*awsconnect.Options)) (*awsconnect.DisassociateQueueQuickConnectsOutput, error) {
	if f.disassociate == nil {
		return &awsconnect.DisassociateQueueQuickConnectsOutput{}, nil
	}
	return f.disassociate(ctx, input)
}

func (f *fakeAssociationClient) ListQueueQuickConnects(ctx context.Context, input *awsconnect.ListQueueQuickConnectsInput, _ ...func(*awsconnect.Options)) (*awsconnect.ListQueueQuickConnectsOutput, error) {
	if f.list == nil {
		return &awsconnect.ListQueueQuickConnectsOutput{}, nil
	}
	return f.list(ctx, input)
}

func newTestAssociationsResource(client associationClient) *queueQuickConnectAssociationsResource {
	return &queueQuickConnectAssociationsResource{
		client:      client,
		coordinator: newQueueCoordinator(),
		reconciliation: reconciliationPolicy{
			attempts: 5,
			wait:     func(context.Context, int) error { return nil },
		},
	}
}

func TestAssociationsResourceMetadataUsesPluralTypeName(t *testing.T) {
	response := &resource.MetadataResponse{}
	NewQueueQuickConnectAssociationsResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "awscontrib"}, response)
	if response.TypeName != "awscontrib_connect_queue_quick_connect_associations" {
		t.Fatalf("unexpected resource type %q", response.TypeName)
	}
}

func TestAssociationsResourceSchemaUsesRequiredReplacementAttributes(t *testing.T) {
	response := &resource.SchemaResponse{}
	NewQueueQuickConnectAssociationsResource().Schema(context.Background(), resource.SchemaRequest{}, response)

	for _, name := range []string{"instance_id", "queue_id"} {
		attribute, ok := response.Schema.Attributes[name].(resourceschema.StringAttribute)
		if !ok {
			t.Fatalf("expected %s to be a string attribute", name)
		}
		if !attribute.Required || len(attribute.PlanModifiers) != 1 {
			t.Fatalf("expected %s to be required and replacement-only: %#v", name, attribute)
		}
	}

	attribute, ok := response.Schema.Attributes["quick_connect_ids"].(resourceschema.SetAttribute)
	if !ok {
		t.Fatal("expected quick_connect_ids to be a set attribute")
	}
	if !attribute.Required || len(attribute.PlanModifiers) != 1 || len(attribute.Validators) != 1 {
		t.Fatalf("expected quick_connect_ids to be required, replacement-only, and validated: %#v", attribute)
	}
	if !attribute.ElementType.Equal(types.StringType) {
		t.Fatalf("expected quick_connect_ids to contain strings, got %s", attribute.ElementType)
	}
}

func TestQuickConnectIDsValidator(t *testing.T) {
	validSet, diagnostics := types.SetValueFrom(context.Background(), types.StringType, []string{quickConnectID1})
	if diagnostics.HasError() {
		t.Fatalf("unexpected set construction diagnostics: %v", diagnostics)
	}
	response := &validator.SetResponse{}
	quickConnectIDsValidator{}.ValidateSet(context.Background(), validator.SetRequest{Path: path.Root("quick_connect_ids"), ConfigValue: validSet}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected valid UUID diagnostic: %v", response.Diagnostics)
	}

	invalidSet, diagnostics := types.SetValueFrom(context.Background(), types.StringType, []string{"not-a-uuid"})
	if diagnostics.HasError() {
		t.Fatalf("unexpected set construction diagnostics: %v", diagnostics)
	}
	response = &validator.SetResponse{}
	quickConnectIDsValidator{}.ValidateSet(context.Background(), validator.SetRequest{Path: path.Root("quick_connect_ids"), ConfigValue: invalidSet}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected invalid UUID diagnostic")
	}
}

func TestQuickConnectIDsValidatorRejectsEmptySet(t *testing.T) {
	emptySet, diagnostics := types.SetValueFrom(context.Background(), types.StringType, []string{})
	if diagnostics.HasError() {
		t.Fatalf("unexpected set construction diagnostics: %v", diagnostics)
	}
	response := &validator.SetResponse{}
	quickConnectIDsValidator{}.ValidateSet(context.Background(), validator.SetRequest{Path: path.Root("quick_connect_ids"), ConfigValue: emptySet}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected empty quick-connect ID set diagnostic")
	}
}

func TestQuickConnectIDsValidatorAllowsUnknownElements(t *testing.T) {
	set, diagnostics := types.SetValue(types.StringType, []attr.Value{
		types.StringValue(quickConnectID1),
		types.StringUnknown(),
	})
	if diagnostics.HasError() {
		t.Fatalf("unexpected set construction diagnostics: %v", diagnostics)
	}

	response := &validator.SetResponse{}
	quickConnectIDsValidator{}.ValidateSet(context.Background(), validator.SetRequest{Path: path.Root("quick_connect_ids"), ConfigValue: set}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostic for unknown UUID: %v", response.Diagnostics)
	}
}

func TestQuickConnectIDsValidatorRejectsNullElements(t *testing.T) {
	set, diagnostics := types.SetValue(types.StringType, []attr.Value{types.StringNull()})
	if diagnostics.HasError() {
		t.Fatalf("unexpected set construction diagnostics: %v", diagnostics)
	}

	response := &validator.SetResponse{}
	quickConnectIDsValidator{}.ValidateSet(context.Background(), validator.SetRequest{Path: path.Root("quick_connect_ids"), ConfigValue: set}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected null quick-connect ID diagnostic")
	}

	diagnostic := response.Diagnostics[0]
	if diagnostic.Summary() != "Invalid Quick Connect ID" {
		t.Fatalf("unexpected null quick-connect ID diagnostic summary: %q", diagnostic.Summary())
	}
	if !strings.Contains(diagnostic.Detail(), "replace the null value") {
		t.Fatalf("expected actionable null quick-connect ID detail, got %q", diagnostic.Detail())
	}
}

func TestAssociationsResourceConfigureAcceptsNilProviderData(t *testing.T) {
	implementation := NewQueueQuickConnectAssociationsResource()
	configurable, ok := implementation.(resource.ResourceWithConfigure)
	if !ok {
		t.Fatal("expected configurable resource")
	}
	response := &resource.ConfigureResponse{}
	configurable.Configure(context.Background(), resource.ConfigureRequest{}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected nil provider data diagnostic: %v", response.Diagnostics)
	}
}

func TestAssociationsResourceConfigureRejectsUnexpectedProviderData(t *testing.T) {
	implementation := NewQueueQuickConnectAssociationsResource()
	configurable, ok := implementation.(resource.ResourceWithConfigure)
	if !ok {
		t.Fatal("expected configurable resource")
	}
	response := &resource.ConfigureResponse{}
	configurable.Configure(context.Background(), resource.ConfigureRequest{ProviderData: "unexpected"}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected unexpected provider data diagnostic")
	}
}

func TestListAssociationsPaginatesUntilEnd(t *testing.T) {
	var tokens []string
	client := &fakeAssociationClient{
		list: func(_ context.Context, input *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			tokens = append(tokens, aws.ToString(input.NextToken))
			if input.NextToken == nil {
				return &awsconnect.ListQueueQuickConnectsOutput{
					QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String(quickConnectID1)}},
					NextToken:               aws.String("page-2"),
				}, nil
			}
			return &awsconnect.ListQueueQuickConnectsOutput{
				QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String(quickConnectID2)}},
			}, nil
		},
	}
	resource := newTestAssociationsResource(client)
	ids, err := resource.listAssociations(context.Background(), associationIdentity{instanceID: "instance", queueID: "queue"})
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(ids) != 2 || len(tokens) != 2 || tokens[0] != "" || tokens[1] != "page-2" {
		t.Fatalf("unexpected paginated result: ids=%v tokens=%v", ids, tokens)
	}
}

func TestListAssociationsTreatsMissingQueueAsEmpty(t *testing.T) {
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			return nil, fmt.Errorf("wrapped: %w", &connecttypes.ResourceNotFoundException{})
		},
	}
	ids, err := newTestAssociationsResource(client).listAssociations(context.Background(), associationIdentity{})
	if err != nil {
		t.Fatalf("unexpected missing queue error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty memberships, got %v", ids)
	}
}

func TestCreateAssociationsOnlyAssociatesMissingIDs(t *testing.T) {
	remote := map[string]struct{}{quickConnectID1: {}, otherQuickConnectID: {}}
	var associateIDs []string
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			return summariesOutput(remote), nil
		},
		associate: func(_ context.Context, input *awsconnect.AssociateQueueQuickConnectsInput) (*awsconnect.AssociateQueueQuickConnectsOutput, error) {
			associateIDs = append(associateIDs, input.QuickConnectIds...)
			for _, id := range input.QuickConnectIds {
				remote[id] = struct{}{}
			}
			return &awsconnect.AssociateQueueQuickConnectsOutput{}, nil
		},
	}
	resource := newTestAssociationsResource(client)
	if err := resource.createAssociations(context.Background(), associationIdentity{instanceID: "instance", queueID: "queue"}, []string{quickConnectID1, quickConnectID2}); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if !reflect.DeepEqual(associateIDs, []string{quickConnectID2}) {
		t.Fatalf("expected only missing ID to be associated, got %v", associateIDs)
	}
	if _, ok := remote[otherQuickConnectID]; !ok {
		t.Fatal("create removed an unrelated association")
	}
}

func TestCreateAssociationsSkipsFullyManagedMembership(t *testing.T) {
	associateCalls := 0
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			return summariesOutput(map[string]struct{}{quickConnectID1: {}, quickConnectID2: {}}), nil
		},
		associate: func(context.Context, *awsconnect.AssociateQueueQuickConnectsInput) (*awsconnect.AssociateQueueQuickConnectsOutput, error) {
			associateCalls++
			return &awsconnect.AssociateQueueQuickConnectsOutput{}, nil
		},
	}
	if err := newTestAssociationsResource(client).createAssociations(context.Background(), associationIdentity{}, []string{quickConnectID2, quickConnectID1}); err != nil {
		t.Fatalf("unexpected idempotent create error: %v", err)
	}
	if associateCalls != 0 {
		t.Fatalf("expected no association call, got %d", associateCalls)
	}
}

func TestMutateAssociationsSplitsBatchesAtAPILimit(t *testing.T) {
	var batches [][]string
	client := &fakeAssociationClient{
		associate: func(_ context.Context, input *awsconnect.AssociateQueueQuickConnectsInput) (*awsconnect.AssociateQueueQuickConnectsOutput, error) {
			batches = append(batches, append([]string(nil), input.QuickConnectIds...))
			return &awsconnect.AssociateQueueQuickConnectsOutput{}, nil
		},
	}
	ids := make([]string, maxQuickConnectIDsPerRequest+1)
	for index := range ids {
		ids[index] = fmt.Sprintf("00000000-0000-0000-0000-%012d", index+1)
	}
	if err := newTestAssociationsResource(client).mutateAssociations(context.Background(), associationIdentity{}, ids, true); err != nil {
		t.Fatalf("unexpected batch mutation error: %v", err)
	}
	if len(batches) != 2 || len(batches[0]) != maxQuickConnectIDsPerRequest || len(batches[1]) != 1 {
		t.Fatalf("expected 50/1 batches, got sizes %d/%d", len(batches[0]), len(batches[1]))
	}
}

func TestMutateAssociationsSplitsDisassociateBatchesAtAPILimit(t *testing.T) {
	var batches [][]string
	client := &fakeAssociationClient{
		disassociate: func(_ context.Context, input *awsconnect.DisassociateQueueQuickConnectsInput) (*awsconnect.DisassociateQueueQuickConnectsOutput, error) {
			batches = append(batches, append([]string(nil), input.QuickConnectIds...))
			return &awsconnect.DisassociateQueueQuickConnectsOutput{}, nil
		},
	}
	ids := make([]string, maxQuickConnectIDsPerRequest+1)
	for index := range ids {
		ids[index] = fmt.Sprintf("00000000-0000-0000-0000-%012d", index+1)
	}
	if err := newTestAssociationsResource(client).mutateAssociations(context.Background(), associationIdentity{}, ids, false); err != nil {
		t.Fatalf("unexpected disassociation batch error: %v", err)
	}
	if len(batches) != 2 || len(batches[0]) != maxQuickConnectIDsPerRequest || len(batches[1]) != 1 {
		t.Fatalf("expected 50/1 disassociation batches, got sizes %d/%d", len(batches[0]), len(batches[1]))
	}
}

func TestDeleteAssociationsOnlyDisassociatesOwnedPresentIDs(t *testing.T) {
	remote := map[string]struct{}{quickConnectID1: {}, quickConnectID2: {}, otherQuickConnectID: {}}
	var disassociateIDs []string
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			return summariesOutput(remote), nil
		},
		disassociate: func(_ context.Context, input *awsconnect.DisassociateQueueQuickConnectsInput) (*awsconnect.DisassociateQueueQuickConnectsOutput, error) {
			disassociateIDs = append(disassociateIDs, input.QuickConnectIds...)
			for _, id := range input.QuickConnectIds {
				delete(remote, id)
			}
			return &awsconnect.DisassociateQueueQuickConnectsOutput{}, nil
		},
	}
	if err := newTestAssociationsResource(client).deleteAssociations(context.Background(), associationIdentity{}, []string{quickConnectID3, quickConnectID1}); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if !reflect.DeepEqual(disassociateIDs, []string{quickConnectID1}) {
		t.Fatalf("expected only owned present ID to be disassociated, got %v", disassociateIDs)
	}
	if _, ok := remote[quickConnectID2]; !ok {
		t.Fatal("delete removed an unrelated owned-by-another-resource association")
	}
	if _, ok := remote[otherQuickConnectID]; !ok {
		t.Fatal("delete removed an unrelated association")
	}
}

func TestDeleteAssociationsToleratesTotalAbsence(t *testing.T) {
	disassociateCalls := 0
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			return &awsconnect.ListQueueQuickConnectsOutput{}, nil
		},
		disassociate: func(context.Context, *awsconnect.DisassociateQueueQuickConnectsInput) (*awsconnect.DisassociateQueueQuickConnectsOutput, error) {
			disassociateCalls++
			return nil, errors.New("unexpected disassociate call")
		},
	}
	if err := newTestAssociationsResource(client).deleteAssociations(context.Background(), associationIdentity{}, []string{quickConnectID1, quickConnectID2}); err != nil {
		t.Fatalf("unexpected total-drift delete error: %v", err)
	}
	if disassociateCalls != 0 {
		t.Fatalf("expected no disassociate call for total absence, got %d", disassociateCalls)
	}
}

func TestReadOwnedMembershipRepresentsPartialDrift(t *testing.T) {
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			return summariesOutput(map[string]struct{}{quickConnectID2: {}, otherQuickConnectID: {}}), nil
		},
	}
	state := associationsState(t, []string{quickConnectID1, quickConnectID2})
	response := &resource.ReadResponse{State: state}
	newTestAssociationsResource(client).Read(context.Background(), resource.ReadRequest{State: state}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected read error: %v", response.Diagnostics)
	}
	var data queueQuickConnectAssociationsModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &data)...)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected state decode error: %v", response.Diagnostics)
	}
	var got []string
	response.Diagnostics.Append(data.QuickConnectIDs.ElementsAs(context.Background(), &got, false)...)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected set decode error: %v", response.Diagnostics)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{quickConnectID2}) {
		t.Fatalf("expected partial state to contain only present owned ID, got %v", got)
	}

	present := intersectQuickConnectIDs([]string{quickConnectID1, quickConnectID2}, map[string]struct{}{quickConnectID2: {}, otherQuickConnectID: {}})
	if !reflect.DeepEqual(present, []string{quickConnectID2}) {
		t.Fatalf("expected partial state to contain only present owned ID, got %v", present)
	}
}

func TestReadOwnedMembershipTreatsTotalAbsenceAsEmpty(t *testing.T) {
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			return summariesOutput(map[string]struct{}{otherQuickConnectID: {}}), nil
		},
	}
	state := associationsState(t, []string{quickConnectID1, quickConnectID2})
	response := &resource.ReadResponse{State: state}
	newTestAssociationsResource(client).Read(context.Background(), resource.ReadRequest{State: state}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected read error: %v", response.Diagnostics)
	}
	if !response.State.Raw.IsNull() {
		t.Fatalf("expected total absence to remove resource state, got %s", response.State.Raw)
	}

	present := intersectQuickConnectIDs([]string{quickConnectID1, quickConnectID2}, map[string]struct{}{otherQuickConnectID: {}})
	if len(present) != 0 {
		t.Fatalf("expected total absence to produce empty owned membership, got %v", present)
	}
}

func TestReconcileAssociationsRetriesTooManyRequests(t *testing.T) {
	listCalls := 0
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			listCalls++
			if listCalls < 3 {
				return nil, &connecttypes.TooManyRequestsException{}
			}
			return summariesOutput(map[string]struct{}{quickConnectID1: {}}), nil
		},
	}
	if err := newTestAssociationsResource(client).reconcileAssociations(context.Background(), associationIdentity{}, []string{quickConnectID1}, true); err != nil {
		t.Fatalf("unexpected throttling reconciliation error: %v", err)
	}
	if listCalls != 3 {
		t.Fatalf("expected two transient retries and a successful observation, got %d calls", listCalls)
	}
}

func TestReconcileAssociationsDoesNotRetryPermanentError(t *testing.T) {
	listCalls := 0
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			listCalls++
			return nil, &connecttypes.InvalidParameterException{}
		},
	}
	if err := newTestAssociationsResource(client).reconcileAssociations(context.Background(), associationIdentity{}, []string{quickConnectID1}, true); err == nil {
		t.Fatal("expected permanent reconciliation error")
	}
	if listCalls != 1 {
		t.Fatalf("expected permanent error to stop retries, got %d calls", listCalls)
	}
}

func TestParseAssociationImportIDValidatesAndSortsIDs(t *testing.T) {
	identity, err := parseAssociationImportID(instanceID + ":" + queueID + ":" + quickConnectID2 + "," + quickConnectID1)
	if err != nil {
		t.Fatalf("unexpected import error: %v", err)
	}
	if identity.instanceID != instanceID || identity.queueID != queueID {
		t.Fatalf("unexpected identity: %#v", identity)
	}
	if !reflect.DeepEqual(identity.quickConnectIDs, []string{quickConnectID1, quickConnectID2}) {
		t.Fatalf("expected canonical ID ordering, got %v", identity.quickConnectIDs)
	}

	for _, invalid := range []string{
		"",
		"instance",
		instanceID + ":" + queueID,
		instanceID + "::" + quickConnectID1,
		instanceID + ":" + queueID + ":not-a-uuid",
		instanceID + ":" + queueID + ":" + quickConnectID1 + "," + quickConnectID1,
		instanceID + ":" + queueID + ":" + quickConnectID1 + ",",
		"not-a-uuid:" + queueID + ":" + quickConnectID1,
		instanceID + ":not-a-uuid:" + quickConnectID1,
	} {
		if _, err := parseAssociationImportID(invalid); err == nil {
			t.Errorf("expected import error for %q", invalid)
		}
	}
}

func TestImportStateSetsCanonicalAttributes(t *testing.T) {
	implementation := NewQueueQuickConnectAssociationsResource()
	state := associationsState(t, []string{quickConnectID1})
	response := &resource.ImportStateResponse{State: state}
	importable, ok := implementation.(resource.ResourceWithImportState)
	if !ok {
		t.Fatal("expected resource to support import")
	}
	importable.ImportState(
		context.Background(),
		resource.ImportStateRequest{ID: instanceID + ":" + queueID + ":" + quickConnectID2 + "," + quickConnectID1},
		response,
	)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected import state error: %v", response.Diagnostics)
	}
	var data queueQuickConnectAssociationsModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &data)...)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected imported state decode error: %v", response.Diagnostics)
	}
	if data.InstanceID.ValueString() != instanceID || data.QueueID.ValueString() != queueID {
		t.Fatalf("unexpected imported identity: %q/%q", data.InstanceID.ValueString(), data.QueueID.ValueString())
	}
	var ids []string
	response.Diagnostics.Append(data.QuickConnectIDs.ElementsAs(context.Background(), &ids, false)...)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected imported IDs decode error: %v", response.Diagnostics)
	}
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, []string{quickConnectID1, quickConnectID2}) {
		t.Fatalf("unexpected imported IDs: %v", ids)
	}
}

func TestQueueCoordinatorSerializesSameQueue(t *testing.T) {
	coordinator := newQueueCoordinator()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan struct{}, 2)
	operationError := errors.New("test operation completed")
	operation := func() error {
		entered <- struct{}{}
		<-release
		return operationError
	}
	key := queueKey{instanceID: "instance", queueID: "queue"}
	go func() { _ = coordinator.withLock(key, operation); done <- struct{}{} }()
	go func() { _ = coordinator.withLock(key, operation); done <- struct{}{} }()

	awaitSignal(t, entered, "first same-queue operation")
	assertNoSignal(t, entered, "overlapping same-queue operation")
	release <- struct{}{}
	awaitSignal(t, entered, "second serialized same-queue operation")
	release <- struct{}{}
	awaitSignal(t, done, "first completion")
	awaitSignal(t, done, "second completion")
}

func TestQueueCoordinatorAllowsIndependentQueues(t *testing.T) {
	coordinator := newQueueCoordinator()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan struct{}, 2)
	operationError := errors.New("test operation completed")
	operation := func() error {
		entered <- struct{}{}
		<-release
		return operationError
	}
	go func() {
		_ = coordinator.withLock(queueKey{instanceID: "instance", queueID: "queue-1"}, operation)
		done <- struct{}{}
	}()
	go func() {
		_ = coordinator.withLock(queueKey{instanceID: "instance", queueID: "queue-2"}, operation)
		done <- struct{}{}
	}()

	awaitSignal(t, entered, "first independent-queue operation")
	awaitSignal(t, entered, "second independent-queue operation")
	release <- struct{}{}
	release <- struct{}{}
	awaitSignal(t, done, "first completion")
	awaitSignal(t, done, "second completion")
}

func TestResourceFactorySharesProviderCoordinator(t *testing.T) {
	factory := QueueQuickConnectAssociationsResourceFactory()
	first, ok := factory().(*queueQuickConnectAssociationsResource)
	if !ok {
		t.Fatal("expected queue associations resource")
	}
	second, ok := factory().(*queueQuickConnectAssociationsResource)
	if !ok {
		t.Fatal("expected queue associations resource")
	}
	if first.coordinator != second.coordinator {
		t.Fatal("expected resources from one provider factory to share a coordinator")
	}
	otherProvider, ok := QueueQuickConnectAssociationsResourceFactory()().(*queueQuickConnectAssociationsResource)
	if !ok {
		t.Fatal("expected queue associations resource")
	}
	if first.coordinator == otherProvider.coordinator {
		t.Fatal("expected distinct provider factories to use distinct coordinators")
	}
}

func summariesOutput(ids map[string]struct{}) *awsconnect.ListQueueQuickConnectsOutput {
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	summaries := make([]connecttypes.QuickConnectSummary, 0, len(ordered))
	for _, id := range ordered {
		summaries = append(summaries, connecttypes.QuickConnectSummary{Id: aws.String(id)})
	}
	return &awsconnect.ListQueueQuickConnectsOutput{QuickConnectSummaryList: summaries}
}

func associationsState(t *testing.T, ids []string) tfsdk.State {
	t.Helper()
	response := &resource.SchemaResponse{}
	NewQueueQuickConnectAssociationsResource().Schema(context.Background(), resource.SchemaRequest{}, response)
	setElements := make([]tftypes.Value, 0, len(ids))
	for _, id := range ids {
		setElements = append(setElements, tftypes.NewValue(tftypes.String, id))
	}
	objectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"instance_id":       tftypes.String,
		"queue_id":          tftypes.String,
		"quick_connect_ids": tftypes.Set{ElementType: tftypes.String},
	}}
	return tfsdk.State{
		Raw: tftypes.NewValue(objectType, map[string]tftypes.Value{
			"instance_id":       tftypes.NewValue(tftypes.String, "instance"),
			"queue_id":          tftypes.NewValue(tftypes.String, "queue"),
			"quick_connect_ids": tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, setElements),
		}),
		Schema: response.Schema,
	}
}

func awaitSignal(t *testing.T, channel <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func assertNoSignal(t *testing.T, channel <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-channel:
		t.Fatalf("received unexpected %s", description)
	case <-time.After(50 * time.Millisecond):
	}
}
