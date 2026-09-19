// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package signals

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// NU-79: a panicking subscriber must not take down the operation that emitted
// the event. The async path has recovered since NU-35; the synchronous one —
// the one that runs inside a model save or a request — did not.
func TestEmit_PanicBecomesAnError(t *testing.T) {
	bus := NewBus(discard())
	bus.On("boom", func(Event) error { panic("subscriber is broken") })

	var err error
	survived := func() (ok bool) {
		defer func() { ok = recover() == nil }()
		err = bus.Emit(Event{Signal: "boom"})
		return
	}()

	if !survived {
		t.Fatal("the panic unwound into the emitter")
	}
	if !errors.Is(err, ErrHandlerPanic) {
		t.Fatalf("Emit returned %v, want ErrHandlerPanic", err)
	}
}

// A handler that panics stops the chain, exactly like one that returns an
// error: the operation is aborted, not silently continued with a subscriber
// that never ran.
func TestEmit_PanicStopsTheChain(t *testing.T) {
	bus := NewBus(discard())
	ran := false
	bus.On("boom", func(Event) error { panic("first") })
	bus.On("boom", func(Event) error { ran = true; return nil })

	if err := bus.Emit(Event{Signal: "boom"}); !errors.Is(err, ErrHandlerPanic) {
		t.Fatalf("Emit returned %v", err)
	}
	if ran {
		t.Error("a handler after the panicking one ran; a panic has to abort like an error does")
	}
}

// NU-78: emitting asynchronously returns to the caller. The emitter is usually
// a model save inside a request, and taking the concurrency slot on its
// goroutine handed the slowest subscriber's latency to the user.
func TestEmitAsync_DoesNotBlockTheEmitter(t *testing.T) {
	bus := NewBus(discard())
	release := make(chan struct{})
	defer close(release)
	bus.On("slow", func(Event) error {
		<-release
		return nil
	})

	// Fill the running ceiling and more.
	for i := 0; i < asyncLimit*2; i++ {
		bus.EmitAsync(Event{Signal: "slow"})
	}

	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		bus.EmitAsync(Event{Signal: "slow"})
		done <- time.Since(start)
	}()
	select {
	case d := <-done:
		if d > time.Second {
			t.Errorf("EmitAsync took %v with slow handlers in flight", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("EmitAsync never returned: the emitter is blocked by its subscribers")
	}
}

// Not blocking must not mean unbounded: past the in-flight limit events are
// dropped, counted and logged, which is the honest failure for a bus whose
// contract already says errors do not propagate.
func TestEmitAsync_DropsBeyondTheInFlightLimit(t *testing.T) {
	bus := NewBus(discard())
	release := make(chan struct{})
	defer close(release)
	var mu sync.Mutex
	started := 0
	bus.On("flood", func(Event) error {
		mu.Lock()
		started++
		mu.Unlock()
		<-release
		return nil
	})

	for i := 0; i < admitLimit+500; i++ {
		bus.EmitAsync(Event{Signal: "flood"})
	}
	if bus.Dropped() == 0 {
		t.Fatal("nothing was dropped past the in-flight limit: the bound is not doing anything")
	}
	t.Logf("%d of %d dispatches dropped past the limit of %d", bus.Dropped(), admitLimit+500, admitLimit)
}
