package conns

import (
	"context"
	"testing"
	"time"

	"github.com/aws/smithy-go/middleware"
	"github.com/aws/smithy-go/transport/http"
)

func TestConnectSchedulingAPIOptionInsertsImmediatelyAfterRetry(t *testing.T) {
	stack := middleware.NewStack("test", http.NewStackRequest)
	if err := stack.Finalize.Add(middleware.FinalizeMiddlewareFunc("Retry", func(
		ctx context.Context,
		in middleware.FinalizeInput,
		next middleware.FinalizeHandler,
	) (middleware.FinalizeOutput, middleware.Metadata, error) {
		return next.HandleFinalize(ctx, in)
	}), middleware.After); err != nil {
		t.Fatalf("unexpected Retry middleware error: %v", err)
	}
	if err := stack.Finalize.Add(middleware.FinalizeMiddlewareFunc("AfterRetry", func(
		ctx context.Context,
		in middleware.FinalizeInput,
		next middleware.FinalizeHandler,
	) (middleware.FinalizeOutput, middleware.Metadata, error) {
		return next.HandleFinalize(ctx, in)
	}), middleware.After); err != nil {
		t.Fatalf("unexpected trailing middleware error: %v", err)
	}

	if err := connectSchedulingAPIOption(newConnectScheduler())(stack); err != nil {
		t.Fatalf("unexpected scheduling middleware error: %v", err)
	}

	got := stack.Finalize.List()
	want := []string{"Retry", connectSchedulingMiddlewareID, "AfterRetry"}
	if len(got) != len(want) {
		t.Fatalf("expected middleware IDs %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected middleware IDs %v, got %v", want, got)
		}
	}
}

func TestConnectSchedulingMiddlewareUsesSmithyOperationName(t *testing.T) {
	scheduler := newConnectSchedulerWithOptions(0)
	middlewareUnderTest := &connectSchedulingMiddleware{scheduler: scheduler}
	next := middleware.FinalizeHandlerFunc(func(
		ctx context.Context,
		in middleware.FinalizeInput,
	) (middleware.FinalizeOutput, middleware.Metadata, error) {
		return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
	})

	ctx := middleware.WithOperationName(context.Background(), "ListQuickConnects")
	if _, _, err := middlewareUnderTest.HandleFinalize(ctx, middleware.FinalizeInput{}, next); err != nil {
		t.Fatalf("unexpected first middleware error: %v", err)
	}
	ctx = middleware.WithOperationName(context.Background(), "ListPhoneNumbersV2")
	if _, _, err := middlewareUnderTest.HandleFinalize(ctx, middleware.FinalizeInput{}, next); err != nil {
		t.Fatalf("unexpected second middleware error: %v", err)
	}

	if _, ok := scheduler.nextRequest["ListQuickConnects"]; !ok {
		t.Fatal("expected ListQuickConnects operation bucket")
	}
	if _, ok := scheduler.nextRequest["ListPhoneNumbersV2"]; !ok {
		t.Fatal("expected ListPhoneNumbersV2 operation bucket")
	}
}

func TestConnectSchedulingMiddlewarePropagatesCancellation(t *testing.T) {
	scheduler := newConnectSchedulerWithOptions(time.Hour)
	scheduler.reserve("ListQuickConnects", time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	middlewareUnderTest := &connectSchedulingMiddleware{scheduler: scheduler}
	_, _, err := middlewareUnderTest.HandleFinalize(
		middleware.WithOperationName(ctx, "ListQuickConnects"),
		middleware.FinalizeInput{},
		middleware.FinalizeHandlerFunc(func(context.Context, middleware.FinalizeInput) (middleware.FinalizeOutput, middleware.Metadata, error) {
			t.Fatal("expected canceled context not to invoke the next handler")
			return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
		}),
	)
	if err != context.Canceled {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
