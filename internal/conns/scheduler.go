package conns

import (
	"context"
	"sync"
	"time"
)

const (
	connectRequestInterval = 500 * time.Millisecond
	connectMaxInFlight     = 2
)

type connectScheduler struct {
	mu           sync.Mutex
	nextRequest  map[string]time.Time
	inFlight     chan struct{}
	interval     time.Duration
	now          func() time.Time
	waitForTimer func(context.Context, time.Duration) error
}

func newConnectScheduler() *connectScheduler {
	return newConnectSchedulerWithOptions(connectRequestInterval)
}

func newConnectSchedulerWithOptions(interval time.Duration) *connectScheduler {
	return &connectScheduler{
		nextRequest:  make(map[string]time.Time),
		inFlight:     make(chan struct{}, connectMaxInFlight),
		interval:     interval,
		now:          time.Now,
		waitForTimer: waitForDuration,
	}
}

func (s *connectScheduler) acquire(ctx context.Context, operation string) (func(), error) {
	if err := s.wait(ctx, operation); err != nil {
		return nil, err
	}

	select {
	case s.inFlight <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	var releaseOnce sync.Once
	return func() {
		releaseOnce.Do(func() {
			<-s.inFlight
		})
	}, nil
}

func (s *connectScheduler) wait(ctx context.Context, operation string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		waitFor := s.reserve(operation, s.now())
		if waitFor <= 0 {
			return nil
		}

		if err := s.waitForTimer(ctx, waitFor); err != nil {
			return err
		}
	}
}

func (s *connectScheduler) reserve(operation string, now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.nextRequest[operation]
	if next.IsZero() || !next.After(now) {
		s.nextRequest[operation] = now.Add(s.interval)
		return 0
	}

	return next.Sub(now)
}

func waitForDuration(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
