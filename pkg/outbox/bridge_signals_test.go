// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package outbox

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/signals"
)

// An event written inside a transaction reaches the same handlers as one
// published directly: the outbox is a TRANSPORT of the bus, not a third way of
// saying something happened.
func TestBusBridge_DeliversOntoTheBus(t *testing.T) {
	db := openOutboxTestDB(t)
	cfg := Config{TableName: "bus_bridge_outbox"}
	store, err := NewStore(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	bus := signals.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	got := make(chan signals.Event, 1)
	bus.On("invoice.paid", func(e signals.Event) error {
		got <- e
		return nil
	})

	bridge, err := NewBusBridge("bus", bus)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBridgeRegistry()
	if err := registry.Register(bridge); err != nil {
		t.Fatal(err)
	}
	router := NewRouter()
	router.AddRoute("invoice.*", "bus")

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnqueueTx(context.Background(), tx, Entry{
		Topic:   "invoice.paid",
		Payload: map[string]any{"id": "inv-1", "amount": 42},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	dcfg := DefaultDispatcherConfig()
	dcfg.LeaseOwner = "test"
	dcfg.Registry = registry
	dcfg.Router = router
	// No handler: routing through bridges is the whole delivery path, which
	// is what the constructor's godoc has always promised.
	d, err := NewDispatcher(store, nil, dcfg)
	if err != nil {
		t.Fatalf("a bridge-routed dispatcher still demands a handler: %v", err)
	}
	if _, err := d.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-got:
		if e.Signal != "invoice.paid" {
			t.Fatalf("the bus received %q", e.Signal)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the committed message never reached the bus")
	}
}

// A handler that refuses the event fails the DELIVERY, so the dispatcher
// retries it and eventually dead-letters it. An async emit would report
// success for work nobody did.
func TestBusBridge_HandlerErrorFailsTheDelivery(t *testing.T) {
	db := openOutboxTestDB(t)
	cfg := Config{TableName: "bus_bridge_fail_outbox"}
	store, err := NewStore(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	bus := signals.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	bus.On("invoice.paid", func(signals.Event) error { return errors.New("subscriber refused it") })

	bridge, _ := NewBusBridge("bus", bus)
	registry := NewBridgeRegistry()
	_ = registry.Register(bridge)
	router := NewRouter()
	router.AddRoute("invoice.*", "bus")

	if _, err := store.Enqueue(context.Background(), Entry{Topic: "invoice.paid", Payload: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	dcfg := DefaultDispatcherConfig()
	dcfg.LeaseOwner = "test"
	dcfg.Registry = registry
	dcfg.Router = router
	d, err := NewDispatcher(store, nil, dcfg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Retried != 1 {
		t.Fatalf("retried=%d delivered=%d, want the refusal to fail the delivery", res.Retried, res.Delivered)
	}
}

// A dispatcher with neither a handler nor bridge routing has no way to deliver
// anything, and says so instead of panicking on the first message.
func TestDispatcher_RefusesWithNoDeliveryPath(t *testing.T) {
	db := openOutboxTestDB(t)
	store, err := NewStore(db, Config{TableName: "no_path_outbox"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultDispatcherConfig()
	cfg.LeaseOwner = "test"
	if _, err := NewDispatcher(store, nil, cfg); !errors.Is(err, ErrHandlerMissing) {
		t.Fatalf("NewDispatcher with no delivery path: %v, want ErrHandlerMissing", err)
	}
}
