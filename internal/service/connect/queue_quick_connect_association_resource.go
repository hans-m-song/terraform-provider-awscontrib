package connect

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &queueQuickConnectAssociationResource{}
var _ resource.ResourceWithConfigure = &queueQuickConnectAssociationResource{}
var _ resource.ResourceWithImportState = &queueQuickConnectAssociationResource{}

type queueQuickConnectAssociationResource struct {
	client         associationClient
	coordinator    *queueCoordinator
	reconciliation reconciliationPolicy
}

type reconciliationPolicy struct {
	attempts int
	wait     func(context.Context, int) error
}

type queueQuickConnectAssociationModel struct {
	InstanceID     types.String `tfsdk:"instance_id"`
	QueueID        types.String `tfsdk:"queue_id"`
	QuickConnectID types.String `tfsdk:"quick_connect_id"`
}

type associationIdentity struct {
	instanceID     string
	queueID        string
	quickConnectID string
}

func NewQueueQuickConnectAssociationResource() resource.Resource {
	return &queueQuickConnectAssociationResource{
		coordinator:    newQueueCoordinator(),
		reconciliation: defaultReconciliationPolicy(),
	}
}

func QueueQuickConnectAssociationResourceFactory() func() resource.Resource {
	coordinator := newQueueCoordinator()
	return func() resource.Resource {
		return &queueQuickConnectAssociationResource{
			coordinator:    coordinator,
			reconciliation: defaultReconciliationPolicy(),
		}
	}
}

func (r *queueQuickConnectAssociationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connect_queue_quick_connect_association"
}

func (r *queueQuickConnectAssociationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replacementModifiers := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one Amazon Connect queue-to-quick-connect association. Mutations for the same instance and queue are serialized within one provider process.",
		Attributes: map[string]schema.Attribute{
			"instance_id": schema.StringAttribute{
				MarkdownDescription: "Amazon Connect instance identifier.",
				Required:            true,
				PlanModifiers:       replacementModifiers,
			},
			"queue_id": schema.StringAttribute{
				MarkdownDescription: "Amazon Connect queue identifier.",
				Required:            true,
				PlanModifiers:       replacementModifiers,
			},
			"quick_connect_id": schema.StringAttribute{
				MarkdownDescription: "Amazon Connect quick-connect identifier.",
				Required:            true,
				PlanModifiers:       replacementModifiers,
			},
		},
	}
}

func (r *queueQuickConnectAssociationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *queueQuickConnectAssociationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data queueQuickConnectAssociationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}

	identity := identityFromModel(data)
	if err := r.createAssociation(ctx, identity); err != nil {
		resp.Diagnostics.AddError("Unable to Associate Queue Quick Connect", fmt.Sprintf("Could not associate quick connect %q with queue %q: %s", identity.quickConnectID, identity.queueID, err))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *queueQuickConnectAssociationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data queueQuickConnectAssociationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}

	identity := identityFromModel(data)
	exists, err := r.associationExists(ctx, identity)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Queue Quick Connect Association", fmt.Sprintf("Could not list quick connects for queue %q: %s", identity.queueID, err))
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *queueQuickConnectAssociationResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
}

func (r *queueQuickConnectAssociationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data queueQuickConnectAssociationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}

	identity := identityFromModel(data)
	if err := r.deleteAssociation(ctx, identity); err != nil {
		resp.Diagnostics.AddError("Unable to Disassociate Queue Quick Connect", fmt.Sprintf("Could not disassociate quick connect %q from queue %q: %s", identity.quickConnectID, identity.queueID, err))
	}
}

func (r *queueQuickConnectAssociationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	identity, err := parseAssociationImportID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected Import Identifier", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("instance_id"), identity.instanceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("queue_id"), identity.queueID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("quick_connect_id"), identity.quickConnectID)...)
}

func (r *queueQuickConnectAssociationResource) createAssociation(ctx context.Context, identity associationIdentity) error {
	return r.coordinator.withLock(queueKey{instanceID: identity.instanceID, queueID: identity.queueID}, func() error {
		exists, err := r.associationExists(ctx, identity)
		if err != nil || exists {
			return err
		}

		_, err = r.client.AssociateQueueQuickConnects(ctx, &awsconnect.AssociateQueueQuickConnectsInput{
			InstanceId:      aws.String(identity.instanceID),
			QueueId:         aws.String(identity.queueID),
			QuickConnectIds: []string{identity.quickConnectID},
		})
		if err != nil {
			return err
		}

		return r.reconcileAssociation(ctx, identity, true)
	})
}

func (r *queueQuickConnectAssociationResource) deleteAssociation(ctx context.Context, identity associationIdentity) error {
	return r.coordinator.withLock(queueKey{instanceID: identity.instanceID, queueID: identity.queueID}, func() error {
		exists, err := r.associationExists(ctx, identity)
		if err != nil || !exists {
			return err
		}

		_, err = r.client.DisassociateQueueQuickConnects(ctx, &awsconnect.DisassociateQueueQuickConnectsInput{
			InstanceId:      aws.String(identity.instanceID),
			QueueId:         aws.String(identity.queueID),
			QuickConnectIds: []string{identity.quickConnectID},
		})
		if isResourceNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}

		return r.reconcileAssociation(ctx, identity, false)
	})
}

func (r *queueQuickConnectAssociationResource) associationExists(ctx context.Context, identity associationIdentity) (bool, error) {
	var nextToken *string
	for {
		output, err := r.client.ListQueueQuickConnects(ctx, &awsconnect.ListQueueQuickConnectsInput{
			InstanceId: aws.String(identity.instanceID),
			QueueId:    aws.String(identity.queueID),
			NextToken:  nextToken,
		})
		if err != nil {
			if isResourceNotFound(err) {
				return false, nil
			}
			return false, err
		}
		for _, quickConnect := range output.QuickConnectSummaryList {
			if aws.ToString(quickConnect.Id) == identity.quickConnectID {
				return true, nil
			}
		}
		if output.NextToken == nil || aws.ToString(output.NextToken) == "" {
			return false, nil
		}
		nextToken = output.NextToken
	}
}

func (r *queueQuickConnectAssociationResource) reconcileAssociation(ctx context.Context, identity associationIdentity, expected bool) error {
	policy := r.reconciliation
	if policy.attempts < 1 || policy.wait == nil {
		policy = defaultReconciliationPolicy()
	}

	var lastTransientError error
	for attempt := 1; attempt <= policy.attempts; attempt++ {
		exists, err := r.associationExists(ctx, identity)
		if err == nil {
			lastTransientError = nil
			if exists == expected {
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
	return fmt.Errorf("association membership did not converge after %d observations: expected membership %t", policy.attempts, expected)
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
	return errors.As(err, &throttling)
}

func identityFromModel(data queueQuickConnectAssociationModel) associationIdentity {
	return associationIdentity{
		instanceID:     data.InstanceID.ValueString(),
		queueID:        data.QueueID.ValueString(),
		quickConnectID: data.QuickConnectID.ValueString(),
	}
}

func parseAssociationImportID(importID string) (associationIdentity, error) {
	parts := strings.Split(importID, ",")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return associationIdentity{}, fmt.Errorf("expected import identifier with format instance_id,queue_id,quick_connect_id; got %q", importID)
	}
	return associationIdentity{instanceID: parts[0], queueID: parts[1], quickConnectID: parts[2]}, nil
}
