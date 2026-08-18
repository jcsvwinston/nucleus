// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-FW-10 (external coverage demo, 2026-08-18): attachOutbox starts the
// dispatcher — whose Run does an IMMEDIATE initial dispatch pass — before
// the extensions loop runs, and Extension.Attach is the only supported way
// to register bridges. Any message already durable in the table (the whole
// point of an outbox) gets leased by that first pass with an empty route
// registry and fails with "no bridge route matched", consuming a retry and
// dirtying attempts/last_error. Observed by the demo:
//
//	t=9s   pending    att=1  err=outbox: no bridge route matched message topic "workorder.completed"
//	t=10s  delivered  att=2  err=-
//
// Code reading rules out the second candidate cause (a registry captured by
// value): Dispatcher.RunOnce consults cfg.Router.Match/cfg.Registry live on
// every pass — which is exactly why the SECOND attempt delivers.
package app

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/outbox"
)

type recordingBridge struct {
	name string
	ch   chan outbox.Message
}

func (b *recordingBridge) Name() string { return b.name }
func (b *recordingBridge) Send(_ context.Context, msg outbox.Message) error {
	select {
	case b.ch <- msg:
	default:
	}
	return nil
}

func (b *recordingBridge) Close() error { return nil }

func (b *recordingBridge) Healthy(context.Context) error { return nil }

// bridgeExtension registers its bridge in Attach — the documented way.
type bridgeExtension struct {
	bridge *recordingBridge
	topic  string
}

func (e *bridgeExtension) Name() string { return "bridge-ext" }
func (e *bridgeExtension) Attach(a *App) error {
	if a.Outbox == nil {
		return nil
	}
	// Real extensions do real work in Attach (dial a broker, read secrets…).
	// The pause makes the ordering bug OBSERVABLE deterministically: with the
	// dispatcher already started, its immediate initial pass and the first
	// tick land in this window and lease the pending message with an empty
	// route registry.
	time.Sleep(1500 * time.Millisecond)
	if err := a.Outbox.RegisterBridge(e.bridge); err != nil {
		return err
	}
	a.Outbox.AddRoute(e.topic, e.bridge.name)
	return nil
}
func (e *bridgeExtension) Shutdown(context.Context) error { return nil }

// A message already pending in the table when the app boots (durability is
// the outbox's contract) must be delivered on the FIRST attempt: the
// dispatcher must not poll before the extensions have attached their
// bridges.
func TestOutboxDispatcherStartsAfterExtensionsAttach(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	const topic = "workorder.completed"
	dbPath := filepath.Join(dir, "app.db")

	// Pre-seed the durable pending message, as a previous process run would.
	seedDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := outbox.NewStore(seedDB, outbox.Config{})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := store.Enqueue(context.Background(), outbox.Entry{
		Topic:   topic,
		Payload: map[string]string{"order": "42"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = seedDB.Close()

	cfg := DefaultConfig()
	cfg.Databases = map[string]DatabaseConfig{"default": {URL: "sqlite://" + dbPath}}
	cfg.Outbox.Enabled = true
	cfg.Outbox.MaxRetries = 5

	bridge := &recordingBridge{name: "rec", ch: make(chan outbox.Message, 4)}
	a, err := New(&cfg,
		WithOpenAuthz(),
		WithExtensions(&bridgeExtension{bridge: bridge, topic: topic}),
	)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Shutdown(ctx)
	})

	select {
	case <-bridge.ch:
	case <-time.After(10 * time.Second):
		t.Fatal("the pre-seeded message was never delivered to the bridge")
	}

	// The bridge channel fires inside Send, BEFORE the dispatcher commits the
	// delivered status — and sqlite may briefly report BUSY while it does.
	// Poll until the row reads as delivered.
	appDB := a.DefaultDB()
	var attempts int
	var lastError sql.NullString
	var status string
	// Give the dispatcher room to commit without a tight read loop competing
	// for sqlite's single writer, then poll gently.
	time.Sleep(1500 * time.Millisecond)
	deadline := time.Now().Add(8 * time.Second)
	for {
		err := appDB.QueryRow(
			`SELECT attempts, last_error, status FROM nucleus_outbox WHERE id = ?`, msg.ID,
		).Scan(&attempts, &lastError, &status)
		if err == nil && status == "delivered" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("message never read as delivered: err=%v status=%q attempts=%d", err, status, attempts)
		}
		time.Sleep(500 * time.Millisecond)
	}

	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 — the dispatcher polled before the extension registered its bridge, leasing and failing the message with an empty route registry (QCD-FW-10); last_error=%q status=%q", attempts, lastError.String, status)
	}
}
