package conns

import (
	"context"

	"github.com/aws/smithy-go/middleware"
)

const connectSchedulingMiddlewareID = "awscontribConnectScheduler"

type connectSchedulingMiddleware struct {
	scheduler *connectScheduler
}

func (m *connectSchedulingMiddleware) ID() string {
	return connectSchedulingMiddlewareID
}

func (m *connectSchedulingMiddleware) HandleFinalize(
	ctx context.Context,
	in middleware.FinalizeInput,
	next middleware.FinalizeHandler,
) (middleware.FinalizeOutput, middleware.Metadata, error) {
	release, err := m.scheduler.acquire(ctx, middleware.GetOperationName(ctx))
	if err != nil {
		return middleware.FinalizeOutput{}, middleware.Metadata{}, err
	}
	defer release()

	return next.HandleFinalize(ctx, in)
}

func connectSchedulingAPIOption(scheduler *connectScheduler) func(*middleware.Stack) error {
	return func(stack *middleware.Stack) error {
		return stack.Finalize.Insert(
			&connectSchedulingMiddleware{scheduler: scheduler},
			"Retry",
			middleware.After,
		)
	}
}
