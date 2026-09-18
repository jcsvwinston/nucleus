// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package jobsbench

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/jcsvwinston/nucleus/pkg/outbox"
	"github.com/jcsvwinston/nucleus/pkg/signals"
)

// The event probes measure the three paths an application has today for "this
// happened, tell whoever cares": pkg/signals (model lifecycle, with a Redis
// relay), the observability bus behind nucleus.EventBus (SQL and HTTP events,
// in-process), and pkg/outbox (transactional, to external bridges). A7's
// promise is ONE typed bus with the outbox as its transactional transport, so
// every control here is also a question about which of the three answers it.

// probeInProcessBus measures a handler receiving an emitted event.
func probeInProcessBus(t *testing.T, _ *env) verdict {
	bus := signals.NewBus(slog.New(slog.DiscardHandler))
	got := make(chan signals.Event, 1)
	bus.On("bench.happened", func(e signals.Event) error {
		got <- e
		return nil
	})
	if err := bus.Emit(signals.Event{Signal: "bench.happened", Payload: "x"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	select {
	case <-got:
		return present
	case <-time.After(time.Second):
		return absent
	}
}

// probeTypedPayload measures whether a handler receives its payload TYPED, or
// an `any` it has to assert. The measurement is the type the bus moves.
func probeTypedPayload(t *testing.T, _ *env) verdict {
	field, ok := reflect.TypeOf(signals.Event{}).FieldByName("Payload")
	if !ok {
		t.Fatalf("signals.Event has no Payload field")
	}
	t.Logf("signals.Event.Payload is %s", field.Type)
	if field.Type.Kind() == reflect.Interface {
		return absent
	}
	return present
}

// probeHandlerPanic measures what a panicking handler does to the emitter —
// in Oban and Sidekiq one bad handler costs its own job, not the process.
func probeHandlerPanic(t *testing.T, _ *env) verdict {
	bus := signals.NewBus(slog.New(slog.DiscardHandler))
	bus.On("bench.panic", func(signals.Event) error { panic("bench: handler panics") })

	survived := make(chan bool, 1)
	go func() {
		defer func() { survived <- recover() == nil }()
		_ = bus.Emit(signals.Event{Signal: "bench.panic"})
	}()
	select {
	case ok := <-survived:
		if ok {
			return present
		}
		t.Log("a panicking handler takes down whatever emitted the event")
		return absent
	case <-time.After(2 * time.Second):
		return absent
	}
}

// probeAsyncBounded measures that a burst of asynchronous emissions cannot
// spawn an unbounded number of goroutines. The burst runs in a goroutine of
// its own because the emitter BLOCKS once the ceiling is reached — which is
// what EVT-12 measures.
func probeAsyncBounded(t *testing.T, _ *env) verdict {
	bus := signals.NewBus(slog.New(slog.DiscardHandler))
	release := make(chan struct{})
	var mu sync.Mutex
	inFlight, peak := 0, 0
	bus.On("bench.async", func(signals.Event) error {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()
		<-release
		mu.Lock()
		inFlight--
		mu.Unlock()
		return nil
	})
	const burst = 400
	go func() {
		for i := 0; i < burst; i++ {
			bus.EmitAsync(signals.Event{Signal: "bench.async"})
		}
	}()
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	observed := peak
	mu.Unlock()
	close(release)
	t.Logf("a burst of %d async emissions peaked at %d handlers in flight", burst, observed)
	if observed > 0 && observed < burst {
		return present
	}
	return absent
}

// probeAsyncNonBlocking measures whether emitting asynchronously returns to
// the caller. It matters because the caller is usually a request or a model
// save: an emit that waits for a slow handler has handed the handler's latency
// to the user.
func probeAsyncNonBlocking(t *testing.T, _ *env) verdict {
	bus := signals.NewBus(slog.New(slog.DiscardHandler))
	release := make(chan struct{})
	defer close(release)
	bus.On("bench.block", func(signals.Event) error {
		<-release
		return nil
	})

	// Fill whatever ceiling the bus keeps, then time one more emission.
	const fill = 128
	go func() {
		for i := 0; i < fill; i++ {
			bus.EmitAsync(signals.Event{Signal: "bench.block"})
		}
	}()
	time.Sleep(300 * time.Millisecond)

	returned := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		bus.EmitAsync(signals.Event{Signal: "bench.block"})
		returned <- time.Since(start)
	}()
	select {
	case d := <-returned:
		t.Logf("EmitAsync returned in %v with slow handlers in flight", d.Round(time.Millisecond))
		return present
	case <-time.After(time.Second):
		t.Log("EmitAsync never returned: with the concurrency ceiling full, the emitter waits for a handler to finish — the caller of an asynchronous emit is blocked by its subscribers")
		return absent
	}
}

// probeCrossReplicaRelay measures an event emitted in one process reaching a
// handler in another. Two buses, two relays, one Redis between them: the
// second bus is what a second replica is.
func probeCrossReplicaRelay(t *testing.T, _ *env) verdict {
	srv := miniredis.RunT(t)
	discard := slog.New(slog.DiscardHandler)

	publisher, err := signals.NewRedisRelay(signals.RedisRelayConfig{RedisURL: "redis://" + srv.Addr()}, discard)
	if err != nil {
		t.Logf("building the publishing relay: %v", err)
		return absent
	}
	defer func() { _ = publisher.Close() }()

	subscriber, err := signals.NewRedisRelay(signals.RedisRelayConfig{RedisURL: "redis://" + srv.Addr()}, discard)
	if err != nil {
		t.Logf("building the subscribing relay: %v", err)
		return absent
	}
	defer func() { _ = subscriber.Close() }()

	remote := signals.NewBus(discard)
	got := make(chan signals.Event, 1)
	remote.On("bench.remote", func(e signals.Event) error {
		got <- e
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// ForwardToBus blocks until the context is cancelled — it IS the receive
	// loop, not a subscription that returns one — so the second replica runs
	// in a goroutine of its own, the way an application would have to.
	forwardErr := make(chan error, 1)
	go func() { forwardErr <- subscriber.ForwardToBus(ctx, "bench.remote", remote) }()
	time.Sleep(200 * time.Millisecond)

	if err := publisher.Publish(ctx, signals.Event{
		Signal:    "bench.remote",
		ModelName: "Invoice",
		Payload:   map[string]string{"id": "inv-1"},
	}); err != nil {
		t.Logf("publishing: %v", err)
		return absent
	}
	select {
	case e := <-got:
		t.Logf("the event crossed: signal=%s model=%s", e.Signal, e.ModelName)
		return present
	case <-time.After(2 * time.Second):
		t.Log("the event was published but never reached the handler on the other bus")
		return absent
	}
}

// probeOutboxTransactional measures the outbox enqueueing inside the caller's
// transaction: the write and the message commit together or not at all.
func probeOutboxTransactional(t *testing.T, e *env) verdict {
	db := benchDB(t)
	store, err := outbox.NewStore(db, outbox.Config{TableName: "bench_outbox"})
	if err != nil {
		t.Fatalf("new outbox store: %v", err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := store.EnqueueTx(context.Background(), tx, outbox.Entry{
		Topic:   "bench.topic",
		Payload: map[string]string{"k": "v"},
	}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("enqueue in tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	snap := outbox.InspectRuntime(db, outbox.Config{TableName: "bench_outbox"})
	t.Logf("after rollback the outbox holds pending=%d", snap.Pending)
	if snap.Pending == 0 {
		return present
	}
	t.Log("a message survived the rollback of the transaction that wrote it")
	return absent
}

// probeOutboxIsBusTransport measures whether the outbox and the in-process bus
// are the same bus: an event emitted on one reaching a handler on the other.
func probeOutboxIsBusTransport(t *testing.T, _ *env) verdict {
	db := benchDB(t)
	store, err := outbox.NewStore(db, outbox.Config{TableName: "bench_outbox_bus"})
	if err != nil {
		t.Fatalf("new outbox store: %v", err)
	}
	bus := signals.NewBus(slog.New(slog.DiscardHandler))
	got := make(chan signals.Event, 1)
	bus.On("bench.topic", func(e signals.Event) error {
		got <- e
		return nil
	})
	if _, err := store.Enqueue(context.Background(), outbox.Entry{
		Topic:   "bench.topic",
		Payload: map[string]string{"k": "v"},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	select {
	case <-got:
		return present
	case <-time.After(500 * time.Millisecond):
		t.Log("an outbox message on topic bench.topic reaches no handler of the same signal: the two are separate buses")
		return absent
	}
}

// probeOutboxRetries measures a delivery that FAILS: the message has to
// survive it, come back with the attempt counted and the reason kept, and be
// owed to a later pass rather than dropped.
func probeOutboxRetries(t *testing.T, _ *env) verdict {
	db := benchDB(t)
	cfg := outbox.Config{TableName: "bench_outbox_retry"}
	store, err := outbox.NewStore(db, cfg)
	if err != nil {
		t.Fatalf("new outbox store: %v", err)
	}
	if _, err := store.Enqueue(context.Background(), outbox.Entry{
		Topic:   "bench.fails",
		Payload: map[string]string{"k": "v"},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	dcfg := outbox.DefaultDispatcherConfig()
	dcfg.LeaseOwner = "jobsbench"
	dcfg.MaxAttempts = 3
	dcfg.BaseDelay = 10 * time.Millisecond
	dispatcher, err := outbox.NewDispatcher(store, func(context.Context, outbox.Message) error {
		return errors.New("bench: the bridge is down")
	}, dcfg)
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
	res, err := dispatcher.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("dispatch pass: %v", err)
	}
	snap := outbox.InspectRuntime(db, cfg)
	var attempts int
	var lastErr string
	if err := db.QueryRow(`SELECT attempts, last_error FROM bench_outbox_retry`).Scan(&attempts, &lastErr); err != nil {
		t.Fatalf("read the message back: %v", err)
	}
	t.Logf("after a failed delivery: attempted=%d retried=%d failed=%d; the row keeps attempts=%d last_error=%q; pending=%d",
		res.Attempted, res.Retried, res.Failed, attempts, lastErr, snap.Pending)
	if res.Retried == 1 && attempts == 1 && lastErr != "" && snap.Pending == 1 {
		return present
	}
	return absent
}

// probeOutboxRequeue measures putting a failed message back.
func probeOutboxRequeue(t *testing.T, _ *env) verdict {
	db := benchDB(t)
	cfg := outbox.Config{TableName: "bench_outbox_requeue"}
	store, err := outbox.NewStore(db, cfg)
	if err != nil {
		t.Fatalf("new outbox store: %v", err)
	}
	msg, err := store.Enqueue(context.Background(), outbox.Entry{
		Topic:   "bench.requeue",
		Payload: map[string]string{"k": "v"},
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := db.Exec(`UPDATE bench_outbox_requeue SET status = 'failed' WHERE id = ?`, msg.ID); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	n, err := store.RequeueFailed(context.Background(), msg.ID)
	if err != nil {
		t.Logf("requeue: %v", err)
		return absent
	}
	t.Logf("requeued %d failed message(s)", n)
	if n == 1 {
		return present
	}
	return absent
}

// probeOutboxPerTopic measures whether an operator can ask about ONE topic —
// NU-76, found by the admin bench while writing the mail delivery view.
func probeOutboxPerTopic(t *testing.T, _ *env) verdict {
	db := benchDB(t)
	cfg := outbox.Config{TableName: "bench_outbox_topic"}
	store, err := outbox.NewStore(db, cfg)
	if err != nil {
		t.Fatalf("new outbox store: %v", err)
	}
	for _, topic := range []string{"mail", "mail", "webhooks"} {
		if _, err := store.Enqueue(context.Background(), outbox.Entry{
			Topic:   topic,
			Payload: map[string]string{"k": "v"},
		}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	snap := outbox.InspectRuntime(db, cfg)
	if byTopic := topicCounts(snap); byTopic {
		return present
	}
	t.Logf("the snapshot counts every topic at once (pending=%d): asking about mail alone needs SQL of your own (NU-76)", snap.Pending)
	return absent
}

// topicCounts reports whether the runtime snapshot breaks its counts down by
// topic. It reads the published struct, which is the whole public answer.
func topicCounts(snap outbox.RuntimeSnapshot) bool {
	rt := reflect.TypeOf(snap)
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		if name == "Topics" || name == "ByTopic" || name == "PerTopic" {
			return true
		}
	}
	return false
}

// probeOutboxLastError measures whether the reason a delivery failed survives
// where an operator looks — the other half of NU-76.
func probeOutboxLastError(t *testing.T, _ *env) verdict {
	rt := reflect.TypeOf(outbox.RuntimeSnapshot{})
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		if name == "LastError" || name == "LastFailure" {
			return present
		}
	}
	t.Log("the snapshot carries counts but not the last delivery error: what a stuck topic needs is the reason, and it is not there")
	return absent
}

// benchDB opens a scratch SQLite database for the outbox probes, with the
// busy_timeout the framework does not set (NU-77, measured by OPS-05).
func benchDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"/outbox.db?_pragma=busy_timeout(10000)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping sqlite: %v", err)
	}
	return db
}
