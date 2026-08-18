package connect

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

type fakeAssociationClient struct {
	associate    func(context.Context, *awsconnect.AssociateQueueQuickConnectsInput) (*awsconnect.AssociateQueueQuickConnectsOutput, error)
	disassociate func(context.Context, *awsconnect.DisassociateQueueQuickConnectsInput) (*awsconnect.DisassociateQueueQuickConnectsOutput, error)
	list         func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error)
}

func (f *fakeAssociationClient) AssociateQueueQuickConnects(ctx context.Context, input *awsconnect.AssociateQueueQuickConnectsInput, _ ...func(*awsconnect.Options)) (*awsconnect.AssociateQueueQuickConnectsOutput, error) {
	return f.associate(ctx, input)
}

func (f *fakeAssociationClient) DisassociateQueueQuickConnects(ctx context.Context, input *awsconnect.DisassociateQueueQuickConnectsInput, _ ...func(*awsconnect.Options)) (*awsconnect.DisassociateQueueQuickConnectsOutput, error) {
	return f.disassociate(ctx, input)
}

func (f *fakeAssociationClient) ListQueueQuickConnects(ctx context.Context, input *awsconnect.ListQueueQuickConnectsInput, _ ...func(*awsconnect.Options)) (*awsconnect.ListQueueQuickConnectsOutput, error) {
	return f.list(ctx, input)
}

func newTestAssociationResource(client associationClient) *queueQuickConnectAssociationResource {
	return &queueQuickConnectAssociationResource{
		client:      client,
		coordinator: newQueueCoordinator(),
		reconciliation: reconciliationPolicy{
			attempts: 5,
			wait:     func(context.Context, int) error { return nil },
		},
	}
}

func TestAssociationResourceSchemaUsesRequiredReplacementIdentity(t *testing.T) {
	resourceImplementation := NewQueueQuickConnectAssociationResource()
	response := &resource.SchemaResponse{}
	resourceImplementation.Schema(context.Background(), resource.SchemaRequest{}, response)

	for _, name := range []string{"instance_id", "queue_id", "quick_connect_id"} {
		attribute, ok := response.Schema.Attributes[name].(resourceschema.StringAttribute)
		if !ok {
			t.Fatalf("expected %s to be a string attribute", name)
		}
		if !attribute.Required {
			t.Errorf("expected %s to be required", name)
		}
		if len(attribute.PlanModifiers) != 1 {
			t.Errorf("expected %s to have one replacement plan modifier, got %d", name, len(attribute.PlanModifiers))
		}
	}
}

func TestAssociationResourceConfigureAcceptsNilProviderData(t *testing.T) {
	resourceImplementation := NewQueueQuickConnectAssociationResource()
	response := &resource.ConfigureResponse{}
	configurable, ok := resourceImplementation.(resource.ResourceWithConfigure)
	if !ok {
		t.Fatal("expected configurable resource")
	}
	configurable.Configure(context.Background(), resource.ConfigureRequest{}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected nil provider data diagnostic: %v", response.Diagnostics)
	}
}

func TestAssociationResourceConfigureRejectsUnexpectedProviderData(t *testing.T) {
	resourceImplementation := NewQueueQuickConnectAssociationResource()
	response := &resource.ConfigureResponse{}
	configurable, ok := resourceImplementation.(resource.ResourceWithConfigure)
	if !ok {
		t.Fatal("expected configurable resource")
	}
	configurable.Configure(context.Background(), resource.ConfigureRequest{ProviderData: "unexpected"}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected unexpected provider data diagnostic")
	}
}

func TestAssociationExistsPaginatesUntilMatch(t *testing.T) {
	var tokens []string
	client := &fakeAssociationClient{
		list: func(_ context.Context, input *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			tokens = append(tokens, aws.ToString(input.NextToken))
			if input.NextToken == nil {
				return &awsconnect.ListQueueQuickConnectsOutput{
					QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String("other")}},
					NextToken:               aws.String("page-2"),
				}, nil
			}
			return &awsconnect.ListQueueQuickConnectsOutput{
				QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String("target")}},
			}, nil
		},
	}
	resource := newTestAssociationResource(client)

	exists, err := resource.associationExists(context.Background(), associationIdentity{instanceID: "instance", queueID: "queue", quickConnectID: "target"})
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if !exists {
		t.Fatal("expected association on second page")
	}
	if len(tokens) != 2 || tokens[0] != "" || tokens[1] != "page-2" {
		t.Fatalf("unexpected pagination tokens: %#v", tokens)
	}
}

func TestAssociationExistsPropagatesListError(t *testing.T) {
	expected := errors.New("list failed")
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			return nil, expected
		},
	}
	resource := newTestAssociationResource(client)

	_, err := resource.associationExists(context.Background(), associationIdentity{})
	if !errors.Is(err, expected) {
		t.Fatalf("expected %v, got %v", expected, err)
	}
}

func TestAssociationExistsTreatsResourceNotFoundAsAbsent(t *testing.T) {
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			return nil, fmt.Errorf("wrapped not found: %w", &connecttypes.ResourceNotFoundException{})
		},
	}
	resource := newTestAssociationResource(client)

	exists, err := resource.associationExists(context.Background(), associationIdentity{})
	if err != nil {
		t.Fatalf("unexpected not-found error: %v", err)
	}
	if exists {
		t.Fatal("expected not-found membership read to be absent")
	}
}

func TestAssociationExistsReturnsFalseAfterAllPages(t *testing.T) {
	listCalls := 0
	client := &fakeAssociationClient{
		list: func(_ context.Context, input *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			listCalls++
			if input.NextToken == nil {
				return &awsconnect.ListQueueQuickConnectsOutput{
					QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String("other-1")}},
					NextToken:               aws.String("page-2"),
				}, nil
			}
			return &awsconnect.ListQueueQuickConnectsOutput{
				QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String("other-2")}},
			}, nil
		},
	}
	resource := newTestAssociationResource(client)

	exists, err := resource.associationExists(context.Background(), associationIdentity{quickConnectID: "missing"})
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if exists {
		t.Fatal("expected missing association")
	}
	if listCalls != 2 {
		t.Fatalf("expected all pages to be read, got %d calls", listCalls)
	}
}

func TestCreateAssociationIsMembershipIdempotent(t *testing.T) {
	associateCalls := 0
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			return &awsconnect.ListQueueQuickConnectsOutput{
				QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String("quick-connect")}},
			}, nil
		},
		associate: func(context.Context, *awsconnect.AssociateQueueQuickConnectsInput) (*awsconnect.AssociateQueueQuickConnectsOutput, error) {
			associateCalls++
			return &awsconnect.AssociateQueueQuickConnectsOutput{}, nil
		},
	}
	resource := newTestAssociationResource(client)

	err := resource.createAssociation(context.Background(), associationIdentity{instanceID: "instance", queueID: "queue", quickConnectID: "quick-connect"})
	if err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if associateCalls != 0 {
		t.Fatalf("expected no associate call, got %d", associateCalls)
	}
}

func TestDeleteAssociationToleratesAbsentMembership(t *testing.T) {
	disassociateCalls := 0
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			return &awsconnect.ListQueueQuickConnectsOutput{}, nil
		},
		disassociate: func(context.Context, *awsconnect.DisassociateQueueQuickConnectsInput) (*awsconnect.DisassociateQueueQuickConnectsOutput, error) {
			disassociateCalls++
			return &awsconnect.DisassociateQueueQuickConnectsOutput{}, nil
		},
	}
	resource := newTestAssociationResource(client)

	err := resource.deleteAssociation(context.Background(), associationIdentity{instanceID: "instance", queueID: "queue", quickConnectID: "quick-connect"})
	if err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if disassociateCalls != 0 {
		t.Fatalf("expected no disassociate call, got %d", disassociateCalls)
	}
}

func TestDeleteAssociationToleratesMissingQueue(t *testing.T) {
	disassociateCalls := 0
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			return nil, &connecttypes.ResourceNotFoundException{}
		},
		disassociate: func(context.Context, *awsconnect.DisassociateQueueQuickConnectsInput) (*awsconnect.DisassociateQueueQuickConnectsOutput, error) {
			disassociateCalls++
			return nil, errors.New("unexpected disassociate call")
		},
	}
	resource := newTestAssociationResource(client)

	err := resource.deleteAssociation(context.Background(), associationIdentity{instanceID: "instance", queueID: "missing", quickConnectID: "quick-connect"})
	if err != nil {
		t.Fatalf("unexpected delete error for missing queue: %v", err)
	}
	if disassociateCalls != 0 {
		t.Fatalf("expected no disassociate call, got %d", disassociateCalls)
	}
}

func TestDeleteAssociationToleratesResourceRemovedBeforeMutation(t *testing.T) {
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			return &awsconnect.ListQueueQuickConnectsOutput{
				QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String("quick-connect")}},
			}, nil
		},
		disassociate: func(context.Context, *awsconnect.DisassociateQueueQuickConnectsInput) (*awsconnect.DisassociateQueueQuickConnectsOutput, error) {
			return nil, &connecttypes.ResourceNotFoundException{}
		},
	}
	resource := newTestAssociationResource(client)

	err := resource.deleteAssociation(context.Background(), associationIdentity{instanceID: "instance", queueID: "queue", quickConnectID: "quick-connect"})
	if err != nil {
		t.Fatalf("unexpected delete error after concurrent removal: %v", err)
	}
}

func TestDeleteAssociationSendsOneQuickConnect(t *testing.T) {
	associated := true
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			if !associated {
				return &awsconnect.ListQueueQuickConnectsOutput{}, nil
			}
			return &awsconnect.ListQueueQuickConnectsOutput{
				QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String("quick-connect")}},
			}, nil
		},
		disassociate: func(_ context.Context, input *awsconnect.DisassociateQueueQuickConnectsInput) (*awsconnect.DisassociateQueueQuickConnectsOutput, error) {
			if aws.ToString(input.InstanceId) != "instance" || aws.ToString(input.QueueId) != "queue" {
				t.Fatalf("unexpected disassociation scope: %#v", input)
			}
			if len(input.QuickConnectIds) != 1 || input.QuickConnectIds[0] != "quick-connect" {
				t.Fatalf("unexpected quick-connect IDs: %#v", input.QuickConnectIds)
			}
			associated = false
			return &awsconnect.DisassociateQueueQuickConnectsOutput{}, nil
		},
	}
	resource := newTestAssociationResource(client)

	if err := resource.deleteAssociation(context.Background(), associationIdentity{instanceID: "instance", queueID: "queue", quickConnectID: "quick-connect"}); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
}

func TestCreateAssociationSendsOneQuickConnect(t *testing.T) {
	associated := false
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			if associated {
				return &awsconnect.ListQueueQuickConnectsOutput{
					QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String("quick-connect")}},
				}, nil
			}
			return &awsconnect.ListQueueQuickConnectsOutput{}, nil
		},
		associate: func(_ context.Context, input *awsconnect.AssociateQueueQuickConnectsInput) (*awsconnect.AssociateQueueQuickConnectsOutput, error) {
			if aws.ToString(input.InstanceId) != "instance" || aws.ToString(input.QueueId) != "queue" {
				t.Fatalf("unexpected association scope: %#v", input)
			}
			if len(input.QuickConnectIds) != 1 || input.QuickConnectIds[0] != "quick-connect" {
				t.Fatalf("unexpected quick-connect IDs: %#v", input.QuickConnectIds)
			}
			associated = true
			return &awsconnect.AssociateQueueQuickConnectsOutput{}, nil
		},
	}
	resource := newTestAssociationResource(client)

	if err := resource.createAssociation(context.Background(), associationIdentity{instanceID: "instance", queueID: "queue", quickConnectID: "quick-connect"}); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
}

func TestCreateAssociationReconcilesEventualMembership(t *testing.T) {
	listCalls := 0
	waits := 0
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			listCalls++
			if listCalls < 4 {
				return &awsconnect.ListQueueQuickConnectsOutput{}, nil
			}
			return &awsconnect.ListQueueQuickConnectsOutput{
				QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String("quick-connect")}},
			}, nil
		},
		associate: func(context.Context, *awsconnect.AssociateQueueQuickConnectsInput) (*awsconnect.AssociateQueueQuickConnectsOutput, error) {
			return &awsconnect.AssociateQueueQuickConnectsOutput{}, nil
		},
	}
	resource := newTestAssociationResource(client)
	resource.reconciliation.wait = func(context.Context, int) error {
		waits++
		return nil
	}

	err := resource.createAssociation(context.Background(), associationIdentity{instanceID: "instance", queueID: "queue", quickConnectID: "quick-connect"})
	if err != nil {
		t.Fatalf("unexpected reconciliation error: %v", err)
	}
	if listCalls != 4 || waits != 2 {
		t.Fatalf("expected pre-read plus three observations and two waits, got %d lists and %d waits", listCalls, waits)
	}
}

func TestDeleteAssociationReconcilesEventualAbsence(t *testing.T) {
	listCalls := 0
	waits := 0
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			listCalls++
			if listCalls < 4 {
				return &awsconnect.ListQueueQuickConnectsOutput{
					QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String("quick-connect")}},
				}, nil
			}
			return &awsconnect.ListQueueQuickConnectsOutput{}, nil
		},
		disassociate: func(context.Context, *awsconnect.DisassociateQueueQuickConnectsInput) (*awsconnect.DisassociateQueueQuickConnectsOutput, error) {
			return &awsconnect.DisassociateQueueQuickConnectsOutput{}, nil
		},
	}
	resource := newTestAssociationResource(client)
	resource.reconciliation.wait = func(context.Context, int) error {
		waits++
		return nil
	}

	err := resource.deleteAssociation(context.Background(), associationIdentity{instanceID: "instance", queueID: "queue", quickConnectID: "quick-connect"})
	if err != nil {
		t.Fatalf("unexpected reconciliation error: %v", err)
	}
	if listCalls != 4 || waits != 2 {
		t.Fatalf("expected pre-read plus three observations and two waits, got %d lists and %d waits", listCalls, waits)
	}
}

func TestReconcileAssociationRetriesTypedTransientReads(t *testing.T) {
	listCalls := 0
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			listCalls++
			switch listCalls {
			case 1:
				return nil, &connecttypes.InternalServiceException{}
			case 2:
				return nil, &connecttypes.ThrottlingException{}
			default:
				return &awsconnect.ListQueueQuickConnectsOutput{
					QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String("quick-connect")}},
				}, nil
			}
		},
	}
	resource := newTestAssociationResource(client)

	err := resource.reconcileAssociation(context.Background(), associationIdentity{quickConnectID: "quick-connect"}, true)
	if err != nil {
		t.Fatalf("unexpected transient reconciliation error: %v", err)
	}
	if listCalls != 3 {
		t.Fatalf("expected three observations, got %d", listCalls)
	}
}

func TestReconcileAssociationDoesNotRetryPermanentReadError(t *testing.T) {
	listCalls := 0
	waits := 0
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			listCalls++
			return nil, &connecttypes.InvalidParameterException{}
		},
	}
	resource := newTestAssociationResource(client)
	resource.reconciliation.wait = func(context.Context, int) error {
		waits++
		return nil
	}

	err := resource.reconcileAssociation(context.Background(), associationIdentity{}, true)
	if err == nil {
		t.Fatal("expected permanent read error")
	}
	if listCalls != 1 || waits != 0 {
		t.Fatalf("expected immediate failure, got %d lists and %d waits", listCalls, waits)
	}
}

func TestReconcileAssociationBoundsObservationMismatch(t *testing.T) {
	listCalls := 0
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			listCalls++
			return &awsconnect.ListQueueQuickConnectsOutput{}, nil
		},
	}
	resource := newTestAssociationResource(client)
	resource.reconciliation.attempts = 3

	err := resource.reconcileAssociation(context.Background(), associationIdentity{}, true)
	if err == nil {
		t.Fatal("expected bounded reconciliation error")
	}
	if listCalls != 3 {
		t.Fatalf("expected three observations, got %d", listCalls)
	}
}

func TestReconcileAssociationPreservesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			return &awsconnect.ListQueueQuickConnectsOutput{}, nil
		},
	}
	resource := newTestAssociationResource(client)
	resource.reconciliation.wait = func(ctx context.Context, _ int) error { return ctx.Err() }

	err := resource.reconcileAssociation(ctx, associationIdentity{}, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestCreateAssociationKeepsQueueLockThroughReconciliation(t *testing.T) {
	listEntered := make(chan struct{}, 4)
	releaseFirstReconciliation := make(chan struct{})
	firstAssociated := make(chan struct{})
	var listMutex sync.Mutex
	listCalls := 0
	client := &fakeAssociationClient{
		list: func(context.Context, *awsconnect.ListQueueQuickConnectsInput) (*awsconnect.ListQueueQuickConnectsOutput, error) {
			listMutex.Lock()
			listCalls++
			call := listCalls
			listMutex.Unlock()
			listEntered <- struct{}{}
			switch call {
			case 1:
				return &awsconnect.ListQueueQuickConnectsOutput{}, nil
			case 2:
				<-releaseFirstReconciliation
				return &awsconnect.ListQueueQuickConnectsOutput{
					QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String("quick-connect-1")}},
				}, nil
			case 3:
				return &awsconnect.ListQueueQuickConnectsOutput{
					QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String("quick-connect-1")}},
				}, nil
			default:
				return &awsconnect.ListQueueQuickConnectsOutput{
					QuickConnectSummaryList: []connecttypes.QuickConnectSummary{{Id: aws.String("quick-connect-1")}, {Id: aws.String("quick-connect-2")}},
				}, nil
			}
		},
		associate: func(_ context.Context, input *awsconnect.AssociateQueueQuickConnectsInput) (*awsconnect.AssociateQueueQuickConnectsOutput, error) {
			if input.QuickConnectIds[0] == "quick-connect-1" {
				firstAssociated <- struct{}{}
			}
			return &awsconnect.AssociateQueueQuickConnectsOutput{}, nil
		},
	}
	coordinator := newQueueCoordinator()
	first := newTestAssociationResource(client)
	first.coordinator = coordinator
	second := newTestAssociationResource(client)
	second.coordinator = coordinator
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)

	go func() {
		firstDone <- first.createAssociation(context.Background(), associationIdentity{instanceID: "instance", queueID: "queue", quickConnectID: "quick-connect-1"})
	}()
	awaitSignal(t, listEntered, "first pre-read")
	awaitSignal(t, firstAssociated, "first association")
	awaitSignal(t, listEntered, "first reconciliation read")

	go func() {
		secondDone <- second.createAssociation(context.Background(), associationIdentity{instanceID: "instance", queueID: "queue", quickConnectID: "quick-connect-2"})
	}()
	assertNoSignal(t, listEntered, "second pre-read before first reconciliation completes")
	releaseFirstReconciliation <- struct{}{}
	if err := <-firstDone; err != nil {
		t.Fatalf("unexpected first create error: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("unexpected second create error: %v", err)
	}
}

func TestParseAssociationImportID(t *testing.T) {
	identity, err := parseAssociationImportID("instance,queue,quick-connect")
	if err != nil {
		t.Fatalf("unexpected import error: %v", err)
	}
	if identity != (associationIdentity{instanceID: "instance", queueID: "queue", quickConnectID: "quick-connect"}) {
		t.Fatalf("unexpected identity: %#v", identity)
	}

	for _, invalid := range []string{"", "instance", "instance,queue", "instance,,quick-connect", "instance,queue,quick-connect,extra"} {
		if _, err := parseAssociationImportID(invalid); err == nil {
			t.Errorf("expected import error for %q", invalid)
		}
	}
}

func TestQueueCoordinatorSerializesSameQueue(t *testing.T) {
	coordinator := newQueueCoordinator()
	testContext := context.Background()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan struct{}, 2)
	operation := func() error {
		entered <- struct{}{}
		<-release
		return context.Cause(testContext)
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
	testContext := context.Background()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan struct{}, 2)
	operation := func() error {
		entered <- struct{}{}
		<-release
		return context.Cause(testContext)
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

func TestQueueCoordinatorCleansUnusedLocks(t *testing.T) {
	coordinator := newQueueCoordinator()
	var waitGroup sync.WaitGroup
	for index := 0; index < 10; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_ = coordinator.withLock(queueKey{instanceID: "instance", queueID: "queue"}, func() error { return nil })
		}()
	}
	waitGroup.Wait()
	if len(coordinator.locks) != 0 {
		t.Fatalf("expected lock cleanup, got %d retained locks", len(coordinator.locks))
	}
}

func TestResourceFactorySharesProviderCoordinator(t *testing.T) {
	factory := QueueQuickConnectAssociationResourceFactory()
	first, ok := factory().(*queueQuickConnectAssociationResource)
	if !ok {
		t.Fatal("expected queue association resource")
	}
	second, ok := factory().(*queueQuickConnectAssociationResource)
	if !ok {
		t.Fatal("expected queue association resource")
	}
	if first.coordinator != second.coordinator {
		t.Fatal("expected resources from one provider factory to share a coordinator")
	}

	otherProvider, ok := QueueQuickConnectAssociationResourceFactory()().(*queueQuickConnectAssociationResource)
	if !ok {
		t.Fatal("expected queue association resource")
	}
	if first.coordinator == otherProvider.coordinator {
		t.Fatal("expected distinct provider factories to use distinct coordinators")
	}
}
