package conns

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestConnectSchedulerReservesEachOperationIndependently(t *testing.T) {
	scheduler := newConnectSchedulerWithOptions(500 * time.Millisecond)
	base := time.Unix(100, 0)

	if waitFor := scheduler.reserve("ListQuickConnects", base); waitFor != 0 {
		t.Fatalf("expected first reservation to be immediate, got %s", waitFor)
	}
	if waitFor := scheduler.reserve("ListQuickConnects", base.Add(100*time.Millisecond)); waitFor != 400*time.Millisecond {
		t.Fatalf("expected same-operation reservation to wait 400ms, got %s", waitFor)
	}
	if waitFor := scheduler.reserve("ListPhoneNumbersV2", base.Add(100*time.Millisecond)); waitFor != 0 {
		t.Fatalf("expected independent operation reservation to be immediate, got %s", waitFor)
	}
	if waitFor := scheduler.reserve("ListQuickConnects", base.Add(500*time.Millisecond)); waitFor != 0 {
		t.Fatalf("expected reservation at the interval boundary to be immediate, got %s", waitFor)
	}
}

func TestConnectSchedulerWaitHonorsContextCancellation(t *testing.T) {
	scheduler := newConnectSchedulerWithOptions(time.Hour)
	scheduler.reserve("ListQuickConnects", time.Now())

	entered := make(chan struct{})
	scheduler.waitForTimer = func(ctx context.Context, _ time.Duration) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- scheduler.wait(ctx, "ListQuickConnects")
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not start its cancellable wait")
	}
	cancel()

	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestConnectSchedulerLimitsInFlightAttempts(t *testing.T) {
	scheduler := newConnectSchedulerWithOptions(0)

	firstRelease, err := scheduler.acquire(context.Background(), "First")
	if err != nil {
		t.Fatalf("unexpected first acquire error: %v", err)
	}
	secondRelease, err := scheduler.acquire(context.Background(), "Second")
	if err != nil {
		t.Fatalf("unexpected second acquire error: %v", err)
	}

	thirdAcquired := make(chan struct{})
	var thirdRelease func()
	var thirdErr error
	go func() {
		thirdRelease, thirdErr = scheduler.acquire(context.Background(), "Third")
		close(thirdAcquired)
	}()

	select {
	case <-thirdAcquired:
		t.Fatal("third attempt acquired a slot while two attempts were active")
	case <-time.After(25 * time.Millisecond):
	}

	firstRelease()
	select {
	case <-thirdAcquired:
	case <-time.After(time.Second):
		t.Fatal("third attempt did not acquire a slot after a release")
	}
	if thirdErr != nil {
		t.Fatalf("unexpected third acquire error: %v", thirdErr)
	}

	secondRelease()
	thirdRelease()
}

func TestConnectSchedulerCanceledPacingDoesNotConsumeSlot(t *testing.T) {
	scheduler := newConnectSchedulerWithOptions(time.Hour)
	firstRelease, err := scheduler.acquire(context.Background(), "ListQuickConnects")
	if err != nil {
		t.Fatalf("unexpected first acquire error: %v", err)
	}
	defer firstRelease()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	scheduler.waitForTimer = func(ctx context.Context, _ time.Duration) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}

	result := make(chan error, 1)
	go func() {
		_, acquireErr := scheduler.acquire(ctx, "ListQuickConnects")
		result <- acquireErr
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not begin its wait")
	}
	cancel()

	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}

	if len(scheduler.inFlight) != 1 {
		t.Fatalf("expected only the first attempt to retain a slot, got %d slots", len(scheduler.inFlight))
	}
}

func TestConnectSchedulerPacingDoesNotBlockIndependentOperationSlot(t *testing.T) {
	scheduler := newConnectSchedulerWithOptions(time.Hour)
	firstRelease, err := scheduler.acquire(context.Background(), "ListQueueQuickConnects")
	if err != nil {
		t.Fatalf("unexpected first acquire error: %v", err)
	}
	defer firstRelease()

	waiting := make(chan struct{})
	secondContext, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	scheduler.waitForTimer = func(ctx context.Context, _ time.Duration) error {
		close(waiting)
		<-ctx.Done()
		return ctx.Err()
	}
	secondResult := make(chan error, 1)
	go func() {
		_, acquireErr := scheduler.acquire(secondContext, "ListQueueQuickConnects")
		secondResult <- acquireErr
	}()
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("second same-operation attempt did not begin pacing")
	}

	independentRelease, err := scheduler.acquire(context.Background(), "ListPhoneNumbersV2")
	if err != nil {
		t.Fatalf("independent operation was blocked by pacing: %v", err)
	}
	independentRelease()

	cancelSecond()
	if err := <-secondResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected paced attempt cancellation, got %v", err)
	}
}

func TestConnectSchedulerConcurrentReservations(t *testing.T) {
	scheduler := newConnectSchedulerWithOptions(0)
	const workers = 32

	var waitGroup sync.WaitGroup
	waitGroup.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer waitGroup.Done()
			release, err := scheduler.acquire(context.Background(), "ListQuickConnects")
			if err != nil {
				t.Errorf("unexpected acquire error: %v", err)
				return
			}
			release()
		}()
	}
	waitGroup.Wait()
}
