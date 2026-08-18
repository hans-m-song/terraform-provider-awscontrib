package connect

import (
	"context"

	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
)

type associationClient interface {
	AssociateQueueQuickConnects(context.Context, *awsconnect.AssociateQueueQuickConnectsInput, ...func(*awsconnect.Options)) (*awsconnect.AssociateQueueQuickConnectsOutput, error)
	DisassociateQueueQuickConnects(context.Context, *awsconnect.DisassociateQueueQuickConnectsInput, ...func(*awsconnect.Options)) (*awsconnect.DisassociateQueueQuickConnectsOutput, error)
	ListQueueQuickConnects(context.Context, *awsconnect.ListQueueQuickConnectsInput, ...func(*awsconnect.Options)) (*awsconnect.ListQueueQuickConnectsOutput, error)
}

type clientFactory interface {
	Connect() *awsconnect.Client
}
