package connect

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const maxQuickConnectIDsPerRequest = 50

var _ resource.Resource = &queueQuickConnectAssociationsResource{}
var _ resource.ResourceWithConfigure = &queueQuickConnectAssociationsResource{}
var _ resource.ResourceWithImportState = &queueQuickConnectAssociationsResource{}

type queueQuickConnectAssociationsResource struct {
	client         associationClient
	coordinator    *queueCoordinator
	reconciliation reconciliationPolicy
}

type reconciliationPolicy struct {
	attempts int
	wait     func(context.Context, int) error
}

type queueQuickConnectAssociationsModel struct {
	InstanceID      types.String `tfsdk:"instance_id"`
	QueueID         types.String `tfsdk:"queue_id"`
	QuickConnectIDs types.Set    `tfsdk:"quick_connect_ids"`
}

type associationIdentity struct {
	instanceID      string
	queueID         string
	quickConnectIDs []string
}

type quickConnectIDsValidator struct{}

func (quickConnectIDsValidator) Description(context.Context) string {
	return "must contain at least 1 UUID"
}

func (quickConnectIDsValidator) MarkdownDescription(context.Context) string {
	return "must contain at least 1 UUID"
}

func (quickConnectIDsValidator) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	var ids []types.String
	resp.Diagnostics.Append(req.ConfigValue.ElementsAs(ctx, &ids, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if len(ids) < 1 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid Quick Connect IDs",
			fmt.Sprintf("quick_connect_ids must contain at least 1 UUID; got %d", len(ids)),
		)
	}

	for _, id := range ids {
		if id.IsUnknown() {
			continue
		}

		if id.IsNull() {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid Quick Connect ID",
				"quick-connect ID must be a non-null UUID; replace the null value with a valid quick-connect ID",
			)
			continue
		}

		if !isUUID(id.ValueString()) {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid Quick Connect ID",
				fmt.Sprintf("quick-connect ID %q must be a UUID", id.ValueString()),
			)
		}
	}
}

func NewQueueQuickConnectAssociationsResource() resource.Resource {
	return &queueQuickConnectAssociationsResource{
		coordinator:    newQueueCoordinator(),
		reconciliation: defaultReconciliationPolicy(),
	}
}

func QueueQuickConnectAssociationsResourceFactory() func() resource.Resource {
	coordinator := newQueueCoordinator()
	return func() resource.Resource {
		return &queueQuickConnectAssociationsResource{
			coordinator:    coordinator,
			reconciliation: defaultReconciliationPolicy(),
		}
	}
}

func (r *queueQuickConnectAssociationsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connect_queue_quick_connect_associations"
}

func (r *queueQuickConnectAssociationsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replacementStringModifiers := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	replacementSetModifiers := []planmodifier.Set{setplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a set of Amazon Connect queue-to-quick-connect associations. Mutations for the same instance and queue are serialized within one provider process.",
		Attributes: map[string]schema.Attribute{
			"instance_id": schema.StringAttribute{
				MarkdownDescription: "Amazon Connect instance identifier.",
				Required:            true,
				PlanModifiers:       replacementStringModifiers,
			},
			"queue_id": schema.StringAttribute{
				MarkdownDescription: "Amazon Connect queue identifier.",
				Required:            true,
				PlanModifiers:       replacementStringModifiers,
			},
			"quick_connect_ids": schema.SetAttribute{
				MarkdownDescription: "The unique quick-connect UUIDs managed by this resource. Amazon Connect accepts at most 50 IDs per association request.",
				ElementType:         types.StringType,
				Required:            true,
				PlanModifiers:       replacementSetModifiers,
				Validators:          []validator.Set{quickConnectIDsValidator{}},
			},
		},
	}
}

func (r *queueQuickConnectAssociationsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *queueQuickConnectAssociationsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data queueQuickConnectAssociationsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}

	ids, diagnostics := quickConnectIDsFromModel(ctx, data)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	identity := associationIdentity{instanceID: data.InstanceID.ValueString(), queueID: data.QueueID.ValueString()}
	if err := r.createAssociations(ctx, identity, ids); err != nil {
		resp.Diagnostics.AddError(
			"Unable to Associate Queue Quick Connects",
			fmt.Sprintf("Could not associate quick connects %q with queue %q: %s", ids, identity.queueID, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *queueQuickConnectAssociationsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data queueQuickConnectAssociationsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}

	ownedIDs, diagnostics := quickConnectIDsFromModel(ctx, data)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	identity := associationIdentity{instanceID: data.InstanceID.ValueString(), queueID: data.QueueID.ValueString()}
	remoteIDs, err := r.listAssociations(ctx, identity)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Read Queue Quick Connect Associations",
			fmt.Sprintf("Could not list quick connects for queue %q: %s", identity.queueID, err),
		)
		return
	}

	presentIDs := intersectQuickConnectIDs(ownedIDs, remoteIDs)
	if len(presentIDs) == 0 {
		resp.State.RemoveResource(ctx)
		return
	}

	data.QuickConnectIDs, diagnostics = types.SetValueFrom(ctx, types.StringType, presentIDs)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *queueQuickConnectAssociationsResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
}

func (r *queueQuickConnectAssociationsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data queueQuickConnectAssociationsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}

	ownedIDs, diagnostics := quickConnectIDsFromModel(ctx, data)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	identity := associationIdentity{instanceID: data.InstanceID.ValueString(), queueID: data.QueueID.ValueString()}
	if err := r.deleteAssociations(ctx, identity, ownedIDs); err != nil {
		resp.Diagnostics.AddError(
			"Unable to Disassociate Queue Quick Connects",
			fmt.Sprintf("Could not disassociate quick connects %q from queue %q: %s", ownedIDs, identity.queueID, err),
		)
	}
}

func (r *queueQuickConnectAssociationsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	identity, err := parseAssociationImportID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected Import Identifier", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("instance_id"), identity.instanceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("queue_id"), identity.queueID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("quick_connect_ids"), identity.quickConnectIDs)...)
}

func (r *queueQuickConnectAssociationsResource) createAssociations(ctx context.Context, identity associationIdentity, declaredIDs []string) error {
	return r.coordinator.withLock(queueKey{instanceID: identity.instanceID, queueID: identity.queueID}, func() error {
		remoteIDs, err := r.listAssociations(ctx, identity)
		if err != nil {
			return err
		}
		missingIDs := missingQuickConnectIDs(declaredIDs, remoteIDs)
		if len(missingIDs) == 0 {
			return nil
		}

		if err := r.mutateAssociations(ctx, identity, missingIDs, true); err != nil {
			return err
		}
		return r.reconcileAssociations(ctx, identity, declaredIDs, true)
	})
}

func (r *queueQuickConnectAssociationsResource) deleteAssociations(ctx context.Context, identity associationIdentity, ownedIDs []string) error {
	return r.coordinator.withLock(queueKey{instanceID: identity.instanceID, queueID: identity.queueID}, func() error {
		remoteIDs, err := r.listAssociations(ctx, identity)
		if err != nil {
			return err
		}
		presentIDs := intersectQuickConnectIDs(ownedIDs, remoteIDs)
		if len(presentIDs) == 0 {
			return nil
		}

		if err := r.mutateAssociations(ctx, identity, presentIDs, false); err != nil {
			if isResourceNotFound(err) {
				return nil
			}
			return err
		}
		return r.reconcileAssociations(ctx, identity, presentIDs, false)
	})
}

func (r *queueQuickConnectAssociationsResource) mutateAssociations(ctx context.Context, identity associationIdentity, ids []string, associate bool) error {
	for start := 0; start < len(ids); start += maxQuickConnectIDsPerRequest {
		end := start + maxQuickConnectIDsPerRequest
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		if associate {
			if _, err := r.client.AssociateQueueQuickConnects(ctx, &awsconnect.AssociateQueueQuickConnectsInput{
				InstanceId:      aws.String(identity.instanceID),
				QueueId:         aws.String(identity.queueID),
				QuickConnectIds: batch,
			}); err != nil {
				return err
			}
			continue
		}

		if _, err := r.client.DisassociateQueueQuickConnects(ctx, &awsconnect.DisassociateQueueQuickConnectsInput{
			InstanceId:      aws.String(identity.instanceID),
			QueueId:         aws.String(identity.queueID),
			QuickConnectIds: batch,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *queueQuickConnectAssociationsResource) listAssociations(ctx context.Context, identity associationIdentity) (map[string]struct{}, error) {
	remoteIDs := make(map[string]struct{})
	var nextToken *string
	for {
		output, err := r.client.ListQueueQuickConnects(ctx, &awsconnect.ListQueueQuickConnectsInput{
			InstanceId: aws.String(identity.instanceID),
			QueueId:    aws.String(identity.queueID),
			NextToken:  nextToken,
		})
		if err != nil {
			if isResourceNotFound(err) {
				return remoteIDs, nil
			}
			return nil, err
		}
		if output == nil {
			return nil, errors.New("amazon connect returned an empty queue quick-connect list response")
		}
		for _, quickConnect := range output.QuickConnectSummaryList {
			if id := aws.ToString(quickConnect.Id); id != "" {
				remoteIDs[id] = struct{}{}
			}
		}
		if output.NextToken == nil || aws.ToString(output.NextToken) == "" {
			return remoteIDs, nil
		}
		nextToken = output.NextToken
	}
}

func (r *queueQuickConnectAssociationsResource) reconcileAssociations(ctx context.Context, identity associationIdentity, ids []string, expectedPresent bool) error {
	policy := r.reconciliation
	if policy.attempts < 1 || policy.wait == nil {
		policy = defaultReconciliationPolicy()
	}

	expectedIDs := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		expectedIDs[id] = struct{}{}
	}
	var lastTransientError error
	for attempt := 1; attempt <= policy.attempts; attempt++ {
		remoteIDs, err := r.listAssociations(ctx, identity)
		if err == nil {
			lastTransientError = nil
			if associationsMatch(remoteIDs, expectedIDs, expectedPresent) {
				return nil
			}
		} else {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			if !isTransientReadError(err) {
				return err
			}
			lastTransientError = err
		}

		if attempt < policy.attempts {
			if err := policy.wait(ctx, attempt); err != nil {
				return err
			}
		}
	}

	if lastTransientError != nil {
		return fmt.Errorf("association membership did not converge after %d observations; last transient read error: %w", policy.attempts, lastTransientError)
	}
	return fmt.Errorf("association membership did not converge after %d observations: expected membership %t for %d quick connects", policy.attempts, expectedPresent, len(ids))
}

func defaultReconciliationPolicy() reconciliationPolicy {
	return reconciliationPolicy{
		attempts: 5,
		wait: func(ctx context.Context, attempt int) error {
			timer := time.NewTimer(time.Duration(attempt) * 250 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

func quickConnectIDsFromModel(ctx context.Context, data queueQuickConnectAssociationsModel) ([]string, diag.Diagnostics) {
	var ids []string
	diagnostics := data.QuickConnectIDs.ElementsAs(ctx, &ids, false)
	if diagnostics.HasError() {
		return nil, diagnostics
	}
	sort.Strings(ids)
	return ids, nil
}

func intersectQuickConnectIDs(ownedIDs []string, remoteIDs map[string]struct{}) []string {
	presentIDs := make([]string, 0, len(ownedIDs))
	for _, id := range ownedIDs {
		if _, ok := remoteIDs[id]; ok {
			presentIDs = append(presentIDs, id)
		}
	}
	sort.Strings(presentIDs)
	return presentIDs
}

func missingQuickConnectIDs(declaredIDs []string, remoteIDs map[string]struct{}) []string {
	missingIDs := make([]string, 0, len(declaredIDs))
	for _, id := range declaredIDs {
		if _, ok := remoteIDs[id]; !ok {
			missingIDs = append(missingIDs, id)
		}
	}
	sort.Strings(missingIDs)
	return missingIDs
}

func associationsMatch(remoteIDs, expectedIDs map[string]struct{}, expectedPresent bool) bool {
	for id := range expectedIDs {
		_, present := remoteIDs[id]
		if present != expectedPresent {
			return false
		}
	}
	return true
}

func isResourceNotFound(err error) bool {
	if err == nil {
		return false
	}
	var resourceNotFound *connecttypes.ResourceNotFoundException
	return errors.As(err, &resourceNotFound)
}

func isTransientReadError(err error) bool {
	var internalService *connecttypes.InternalServiceException
	if errors.As(err, &internalService) {
		return true
	}
	var throttling *connecttypes.ThrottlingException
	if errors.As(err, &throttling) {
		return true
	}
	var tooManyRequests *connecttypes.TooManyRequestsException
	return errors.As(err, &tooManyRequests)
}

func parseAssociationImportID(importID string) (associationIdentity, error) {
	parts := strings.SplitN(importID, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return associationIdentity{}, fmt.Errorf("expected import identifier with format instance_id:queue_id:quick_connect_id[,quick_connect_id...]; got %q", importID)
	}
	if !isUUID(parts[0]) {
		return associationIdentity{}, fmt.Errorf("instance import ID %q must be a UUID", parts[0])
	}
	if !isUUID(parts[1]) {
		return associationIdentity{}, fmt.Errorf("queue import ID %q must be a UUID", parts[1])
	}

	ids := strings.Split(parts[2], ",")
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" || !isUUID(id) {
			return associationIdentity{}, fmt.Errorf("quick-connect import ID %q must be a UUID", id)
		}
		if _, duplicate := seen[id]; duplicate {
			return associationIdentity{}, fmt.Errorf("quick-connect import ID %q is duplicated", id)
		}
		seen[id] = struct{}{}
	}
	if len(ids) < 1 {
		return associationIdentity{}, fmt.Errorf("quick-connect import must contain at least 1 UUID; got %d", len(ids))
	}
	sort.Strings(ids)
	return associationIdentity{instanceID: parts[0], queueID: parts[1], quickConnectIDs: ids}, nil
}

func isUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') && (character < 'A' || character > 'F') {
			return false
		}
	}
	return true
}
