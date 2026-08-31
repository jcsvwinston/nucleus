package circuit

import (
	"context"
	"errors"
	"testing"
	"time"
)

// NF-9: the breaker used to change state in complete silence. This pins the
// OnStateChange hook: every transition of the closed/open/half-open machine
// fires exactly once, in order, and Opens() counts the trips.
func TestOnStateChangeFiresOnEveryTransition(t *testing.T) {
	now := time.Unix(0, 0)
	var transitions [][2]State
	br := New(Config{
		FailureThreshold: 1,
		Cooldown:         time.Second,
		Now:              func() time.Time { return now },
		OnStateChange: func(from, to State) {
			transitions = append(transitions, [2]State{from, to})
		},
	})

	boom := errors.New("boom")
	fail := func(context.Context) error { return boom }
	ok := func(context.Context) error { return nil }

	// closed → open
	if err := br.Do(context.Background(), fail); !errors.Is(err, boom) {
		t.Fatalf("Do fail: %v", err)
	}
	// open → half-open (cooldown elapsed) → open (probe fails)
	now = now.Add(2 * time.Second)
	if err := br.Do(context.Background(), fail); !errors.Is(err, boom) {
		t.Fatalf("Do probe fail: %v", err)
	}
	// open → half-open → closed (probe succeeds)
	now = now.Add(2 * time.Second)
	if err := br.Do(context.Background(), ok); err != nil {
		t.Fatalf("Do probe ok: %v", err)
	}

	want := [][2]State{
		{StateClosed, StateOpen},
		{StateOpen, StateHalfOpen},
		{StateHalfOpen, StateOpen},
		{StateOpen, StateHalfOpen},
		{StateHalfOpen, StateClosed},
	}
	if len(transitions) != len(want) {
		t.Fatalf("transitions = %v; want %v", transitions, want)
	}
	for i := range want {
		if transitions[i] != want[i] {
			t.Fatalf("transition %d = %v; want %v (all: %v)", i, transitions[i], want[i], transitions)
		}
	}

	if got := br.Opens(); got != 2 {
		t.Fatalf("Opens() = %d; want 2", got)
	}
}

// The callback runs outside the breaker lock: reading State()/Opens() from
// inside it must not deadlock.
func TestOnStateChangeMayReadBreakerState(t *testing.T) {
	done := make(chan struct{})
	var br *Breaker
	br = New(Config{
		FailureThreshold: 1,
		OnStateChange: func(from, to State) {
			_ = br.State()
			_ = br.Opens()
			close(done)
		},
	})
	_ = br.Do(context.Background(), func(context.Context) error { return errors.New("x") })
	select {
	case <-done:
	default:
		t.Fatal("OnStateChange did not run")
	}
}
