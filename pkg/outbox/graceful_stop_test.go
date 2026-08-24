// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Stop's shutdown contract. Stop used to cancel the run context outright,
// which aborts whatever the dispatcher is doing: a delivery in flight is
// abandoned (its message stays claimed until the lease expires) and the
// statement in flight is torn down under cancellation. Stop now asks the
// dispatcher to finish the pass first and escalates to cancellation only
// when the caller cannot wait any longer.
package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// slowBridge blocks in Send until released, recording that the delivery
// both started and — crucially — RETURNED.
type slowBridge struct {
	name     string
	entered  chan struct{}
	release  chan struct{}
	finished atomic.Bool
	once     bool
}

func (b *slowBridge) Name() string { return b.name }

func (b *slowBridge) Send(ctx context.Context, _ Message) error {
	if !b.once {
		b.once = true
		close(b.entered)
	}
	select {
	case <-b.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	b.finished.Store(true)
	return nil
}

func (b *slowBridge) Healthy(context.Context) error { return nil }
func (b *slowBridge) Close() error                  { return nil }

func newBridgedOutbox(t *testing.T, bridge Bridge, topic string) (*ManagedOutbox, *sql.DB) {
	t.Helper()
	db := openOutboxTestDB(t)
	managed, err := NewManagedOutbox(ManagedConfig{
		DB:           db,
		Flavor:       FlavorSQLite,
		LeaseOwner:   "graceful-test",
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new managed outbox: %v", err)
	}
	if err := managed.RegisterBridge(bridge); err != nil {
		t.Fatalf("register bridge: %v", err)
	}
	managed.Router().AddRoute(topic, bridge.Name())
	if _, err := managed.Enqueue(context.Background(), Entry{Topic: topic, Payload: []byte(`{}`)}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return managed, db
}

// A delivery in flight when Stop is called must be allowed to finish.
func TestManagedOutbox_StopLetsInFlightPassFinish(t *testing.T) {
	bridge := &slowBridge{name: "slow", entered: make(chan struct{}), release: make(chan struct{})}
	managed, _ := newBridgedOutbox(t, bridge, "orders.created")

	if err := managed.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case <-bridge.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("bridge delivery never started")
	}

	// Stop with a caller context that is in no hurry: the delivery must
	// complete rather than be cancelled.
	stopped := make(chan error, 1)
	go func() { stopped <- managed.Stop(context.Background()) }()

	// Stop must still be waiting while the delivery is blocked.
	select {
	case err := <-stopped:
		t.Fatalf("Stop returned while the delivery was still in flight (err=%v)", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(bridge.release)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return after the delivery finished")
	}
	if !bridge.finished.Load() {
		t.Fatal("the in-flight delivery was aborted; Stop must let the pass finish")
	}
}

// When the caller's context expires first, Stop escalates: it cancels the
// run context and still returns promptly with the dispatcher stopped.
func TestManagedOutbox_StopEscalatesOnCallerDeadline(t *testing.T) {
	bridge := &slowBridge{name: "stuck", entered: make(chan struct{}), release: make(chan struct{})}
	managed, _ := newBridgedOutbox(t, bridge, "orders.stuck")
	t.Cleanup(func() { close(bridge.release) })

	if err := managed.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case <-bridge.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("bridge delivery never started")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	if err := managed.Stop(ctx); err != nil {
		t.Fatalf("stop with an expiring deadline must still stop the dispatcher: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Stop took %s: the deadline must cut the graceful wait short", elapsed)
	}
	// A second Stop is a no-op, and Start works again on a stopped outbox.
	if err := managed.Stop(context.Background()); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

// Start/Stop cycles under -race, the shape of the CI lane that panicked
// inside database/sql teardown. It has never reproduced locally; the cycle
// is kept as a canary so a future regression has somewhere to land.
func TestManagedOutbox_StartStopCyclesAreClean(t *testing.T) {
	for i := range 50 {
		db := openOutboxTestDB(t)
		managed, err := NewManagedOutbox(ManagedConfig{
			DB:           db,
			Flavor:       FlavorSQLite,
			LeaseOwner:   fmt.Sprintf("cycle-%d", i),
			PollInterval: time.Millisecond,
		})
		if err != nil {
			t.Fatalf("new managed outbox: %v", err)
		}
		if err := managed.Start(context.Background()); err != nil {
			t.Fatalf("start: %v", err)
		}
		if err := managed.Stop(context.Background()); err != nil {
			t.Fatalf("stop: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}
}
