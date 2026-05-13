package circuit

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeClock returns a controllable time source for tests so we never
// need real wall-clock waits.
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

func newTestBreaker(threshold int, cooldown time.Duration, halfOpenMax int, clock *fakeClock) *Breaker {
	return New(Config{
		FailureThreshold:      threshold,
		Cooldown:              cooldown,
		HalfOpenMaxConcurrent: halfOpenMax,
		Now:                   clock.Now,
	})
}

func TestBreaker_ClosedPassesThrough(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	b := newTestBreaker(3, time.Minute, 1, clock)

	for i := 0; i < 5; i++ {
		if err := b.Do(context.Background(), func(context.Context) error { return nil }); err != nil {
			t.Fatalf("call %d returned %v", i, err)
		}
	}
	if got := b.State(); got != StateClosed {
		t.Fatalf("expected closed after successes, got %s", got)
	}
}

func TestBreaker_TripsAfterThresholdConsecutiveFailures(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	b := newTestBreaker(3, time.Minute, 1, clock)
	bad := errors.New("boom")

	// First failure: still closed, still passes through next call.
	if err := b.Do(context.Background(), func(context.Context) error { return bad }); err != bad {
		t.Fatalf("expected bad error, got %v", err)
	}
	if got := b.State(); got != StateClosed {
		t.Fatalf("expected closed after 1 failure (threshold 3), got %s", got)
	}

	// Second failure.
	_ = b.Do(context.Background(), func(context.Context) error { return bad })

	// Third failure: trips.
	_ = b.Do(context.Background(), func(context.Context) error { return bad })
	if got := b.State(); got != StateOpen {
		t.Fatalf("expected open after threshold failures, got %s", got)
	}
}

func TestBreaker_ResetsFailureCountOnSuccess(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	b := newTestBreaker(3, time.Minute, 1, clock)
	bad := errors.New("boom")

	// Two failures, then a success.
	_ = b.Do(context.Background(), func(context.Context) error { return bad })
	_ = b.Do(context.Background(), func(context.Context) error { return bad })
	if err := b.Do(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("success call: %v", err)
	}
	// The two prior failures should no longer count — two more failures
	// must NOT trip the breaker.
	_ = b.Do(context.Background(), func(context.Context) error { return bad })
	_ = b.Do(context.Background(), func(context.Context) error { return bad })
	if got := b.State(); got != StateClosed {
		t.Fatalf("non-consecutive failures should not trip; state is %s", got)
	}
}

func TestBreaker_OpenShortCircuitsBeforeCooldown(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	b := newTestBreaker(1, time.Minute, 1, clock)
	bad := errors.New("boom")

	_ = b.Do(context.Background(), func(context.Context) error { return bad })
	if got := b.State(); got != StateOpen {
		t.Fatalf("expected open after first failure with threshold 1, got %s", got)
	}

	// Subsequent call must short-circuit with ErrOpen and not invoke fn.
	called := false
	err := b.Do(context.Background(), func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("expected ErrOpen, got %v", err)
	}
	if called {
		t.Fatal("fn must not be invoked while breaker is open")
	}
}

func TestBreaker_HalfOpenAdmitsOneProbeAfterCooldown(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	b := newTestBreaker(1, time.Minute, 1, clock)
	bad := errors.New("boom")

	_ = b.Do(context.Background(), func(context.Context) error { return bad })
	clock.Advance(2 * time.Minute) // beyond cooldown

	// First post-cooldown call: should be admitted (half-open probe)
	// and the breaker should be in half-open state during the call —
	// observable via the fn closure.
	var observedDuring State
	if err := b.Do(context.Background(), func(context.Context) error {
		observedDuring = b.State()
		return nil
	}); err != nil {
		t.Fatalf("probe call: %v", err)
	}
	if observedDuring != StateHalfOpen {
		t.Fatalf("expected half-open during probe, got %s", observedDuring)
	}
	if got := b.State(); got != StateClosed {
		t.Fatalf("expected closed after successful probe, got %s", got)
	}
}

func TestBreaker_HalfOpenFailureReopensWithFreshCooldown(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	b := newTestBreaker(1, time.Minute, 1, clock)
	bad := errors.New("boom")

	_ = b.Do(context.Background(), func(context.Context) error { return bad })
	clock.Advance(2 * time.Minute)

	// Probe fails — breaker re-opens.
	if err := b.Do(context.Background(), func(context.Context) error { return bad }); err != bad {
		t.Fatalf("probe call: expected bad, got %v", err)
	}
	if got := b.State(); got != StateOpen {
		t.Fatalf("expected re-open after failed probe, got %s", got)
	}

	// Next call short-circuits because openedAt was reset by trip().
	clock.Advance(30 * time.Second) // less than cooldown
	err := b.Do(context.Background(), func(context.Context) error { return nil })
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("expected ErrOpen during fresh cooldown, got %v", err)
	}
}

func TestBreaker_HalfOpenBudgetEnforced(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	b := newTestBreaker(1, time.Minute, 2, clock)
	bad := errors.New("boom")

	_ = b.Do(context.Background(), func(context.Context) error { return bad })
	clock.Advance(2 * time.Minute)

	// Two in-flight probes use up the half-open budget.
	gate1 := make(chan struct{})
	gate2 := make(chan struct{})
	done1 := make(chan error, 1)
	done2 := make(chan error, 1)
	go func() {
		done1 <- b.Do(context.Background(), func(context.Context) error {
			<-gate1
			return nil
		})
	}()
	go func() {
		done2 <- b.Do(context.Background(), func(context.Context) error {
			<-gate2
			return nil
		})
	}()

	// Spin until both probes have entered the breaker. Without this the
	// test can race ahead and exercise the third call before the half-
	// open budget is fully reserved.
	for {
		b.mu.Lock()
		ready := b.halfOpenInFlight == 2
		b.mu.Unlock()
		if ready {
			break
		}
	}

	// Third call must short-circuit: budget exhausted.
	if err := b.Do(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, ErrOpen) {
		t.Fatalf("expected ErrOpen when half-open budget is exhausted, got %v", err)
	}

	close(gate1)
	close(gate2)
	if err := <-done1; err != nil {
		t.Fatalf("probe 1: %v", err)
	}
	if err := <-done2; err != nil {
		t.Fatalf("probe 2: %v", err)
	}
	if got := b.State(); got != StateClosed {
		t.Fatalf("expected closed after both probes succeeded, got %s", got)
	}
}

func TestBreaker_ZeroConfigUsesDefaults(t *testing.T) {
	b := New(Config{})
	if b.failureThreshold != 1 {
		t.Errorf("FailureThreshold default = %d, want 1", b.failureThreshold)
	}
	if b.cooldown != 30*time.Second {
		t.Errorf("Cooldown default = %v, want 30s", b.cooldown)
	}
	if b.halfOpenMaxConcurrent != 1 {
		t.Errorf("HalfOpenMaxConcurrent default = %d, want 1", b.halfOpenMaxConcurrent)
	}
	if b.now == nil {
		t.Error("Now default should be non-nil")
	}
}

func TestState_String(t *testing.T) {
	cases := []struct {
		state State
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{State(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.state.String(); got != c.want {
			t.Errorf("State(%d).String() = %q, want %q", c.state, got, c.want)
		}
	}
}
