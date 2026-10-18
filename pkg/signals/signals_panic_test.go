// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package signals

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type nothing struct{}

var (
	boom  = Define[nothing]("boom")
	slow  = Define[nothing]("slow")
	flood = Define[nothing]("flood")
)

// NU-79: a panicking subscriber must not take down the operation that
// published the event. The async path has recovered since NU-35; the
// synchronous one — the one that runs inside a write or a request — did not.
func TestPublish_PanicBecomesAnError(t *testing.T) {
	bus := NewBus(discard())
	Subscribe(bus, boom, func(context.Context, nothing) error { panic("subscriber is broken") })

	var err error
	survived := func() (ok bool) {
		defer func() { ok = recover() == nil }()
		err = Publish(context.Background(), bus, boom, nothing{})
		return
	}()

	if !survived {
		t.Fatal("the panic unwound into the publisher")
	}
	if !errors.Is(err, ErrHandlerPanic) {
		t.Fatalf("Publish returned %v, want ErrHandlerPanic", err)
	}
}

// A handler that panics stops the chain, exactly like one that returns an
// error: the operation is aborted, not silently continued with a subscriber
// that never ran.
func TestPublish_PanicStopsTheChain(t *testing.T) {
	bus := NewBus(discard())
	ran := false
	Subscribe(bus, boom, func(context.Context, nothing) error { panic("first") })
	Subscribe(bus, boom, func(context.Context, nothing) error { ran = true; return nil })

	if err := Publish(context.Background(), bus, boom, nothing{}); !errors.Is(err, ErrHandlerPanic) {
		t.Fatalf("Publish returned %v", err)
	}
	if ran {
		t.Error("a handler after the panicking one ran; a panic has to abort like an error does")
	}
}

// NU-78: publishing asynchronously returns to the caller. The publisher is
// usually a write inside a request, and taking the concurrency slot on its
// goroutine handed the slowest subscriber's latency to the user.
func TestPublishAsync_DoesNotBlockThePublisher(t *testing.T) {
	bus := NewBus(discard())
	release := make(chan struct{})
	defer close(release)
	Subscribe(bus, slow, func(context.Context, nothing) error {
		<-release
		return nil
	})

	// Fill the running ceiling and more.
	for i := 0; i < asyncLimit*2; i++ {
		PublishAsync(context.Background(), bus, slow, nothing{})
	}

	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		PublishAsync(context.Background(), bus, slow, nothing{})
		done <- time.Since(start)
	}()
	select {
	case d := <-done:
		if d > time.Second {
			t.Errorf("PublishAsync took %v with slow handlers in flight", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("PublishAsync never returned: the publisher is blocked by its subscribers")
	}
}

// Not blocking must not mean unbounded: past the in-flight limit events are
// dropped, counted and logged, which is the honest failure for a bus whose
// contract already says errors do not propagate.
func TestPublishAsync_DropsBeyondTheInFlightLimit(t *testing.T) {
	bus := NewBus(discard())
	release := make(chan struct{})
	defer close(release)
	var mu sync.Mutex
	started := 0
	Subscribe(bus, flood, func(context.Context, nothing) error {
		mu.Lock()
		started++
		mu.Unlock()
		<-release
		return nil
	})

	for i := 0; i < admitLimit+500; i++ {
		PublishAsync(context.Background(), bus, flood, nothing{})
	}
	if bus.Dropped() == 0 {
		t.Fatal("nothing was dropped past the in-flight limit: the bound is not doing anything")
	}
	t.Logf("%d of %d dispatches dropped past the limit of %d", bus.Dropped(), admitLimit+500, admitLimit)
}
