// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package dirqueue_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/internal/fixtures/inprocess/dirqueue"
	"github.com/jcsvwinston/nucleus/internal/testsqlite"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/outbox"
)

func init() { testsqlite.Register() }

// The example is tested the way an application runs it: mounted on an
// application whose outbox is on, and reached through the dispatcher that
// application starts. Nothing here calls Send on a message the outbox did
// not lease.

func startApp(t *testing.T, dir string) *nucleustest.Server {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.LogLevel = "error"
	cfg.Databases = nucleustest.TempSQLite(t)
	cfg.Storage.Provider = "memory"
	cfg.Outbox = app.OutboxConfig{Enabled: true, TableName: "nucleus_outbox", LeaseDuration: 30 * time.Second, MaxRetries: 5, RetryBackoff: time.Second}
	return nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Options: []app.Option{app.WithOpenAuthz()},
		Modules: map[string]nucleus.ModuleSpec{"dirqueue": dirqueue.Module(dirqueue.Config{Dir: dir, Pattern: "orders.*"})},
	})
}

func delivered(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		if err := db.QueryRow("SELECT status FROM nucleus_outbox WHERE id = ?", id).Scan(&status); err == nil && status == string(outbox.StatusDelivered) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("message %s is %q, never delivered", id, status)
}

// TestDeliversTheApplicationsOutbox: a message the application enqueues on
// a routed topic is written into the directory queue by the module's
// bridge, and the outbox marks it delivered.
func TestDeliversTheApplicationsOutbox(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	srv := startApp(t, dir)
	rt := srv.Runtime()
	msg, err := rt.Outbox().Enqueue(context.Background(), outbox.Entry{Topic: "orders.created", Payload: map[string]any{"order_id": 42}})
	if err != nil {
		t.Fatal(err)
	}
	delivered(t, rt.DB(), msg.ID)

	raw, err := os.ReadFile(filepath.Join(dir, "orders.created", "new", msg.ID+".json"))
	if err != nil {
		t.Fatalf("the message is not in the queue: %v", err)
	}
	var got dirqueue.Message
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != msg.ID || got.Topic != "orders.created" || string(got.Payload) != `{"order_id":42}` || got.Attempts != 1 {
		t.Fatalf("queued %+v (payload %s)", got, got.Payload)
	}
}

// TestRefusalIsPermanent: what cannot name a file never will; the bridge
// says so with outbox.Permanent, which sends the message to the dead letter
// on that attempt.
func TestRefusalIsPermanent(t *testing.T) {
	b := &dirqueue.Bridge{Dir: t.TempDir()}
	err := b.Send(context.Background(), outbox.Message{ID: "m1", Topic: "../escape", Payload: []byte(`{}`)})
	if !outbox.IsPermanent(err) {
		t.Fatalf("want a permanent refusal, got %v", err)
	}
}

// TestModuleConformance holds the module to the framework's checks, on an
// application whose outbox is on — the module needs one, and says so in
// OnStart when it is not.
func TestModuleConformance(t *testing.T) {
	config := filepath.Join(t.TempDir(), "nucleus.yml")
	if err := os.WriteFile(config, []byte("log_level: error\noutbox:\n  enabled: true\nmodules:\n  dirqueue:\n    dir: "+filepath.Join(t.TempDir(), "queue")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nucleustest.CheckModuleIn(t, nucleus.New().FromConfigFile(config), dirqueue.Module(dirqueue.Config{}))
}

// TestWithoutAnOutboxTheModuleSaysWhy: mounted on an application with the
// outbox off, the module fails its start with the key to set.
func TestWithoutAnOutboxTheModuleSaysWhy(t *testing.T) {
	cfg := app.DefaultConfig()
	cfg.LogLevel = "error"
	cfg.Databases = nucleustest.TempSQLite(t)
	cfg.Storage.Provider = "memory"
	cfg.StateDir = t.TempDir()
	checks := nucleus.CheckModule(context.Background(), nucleus.App{Config: cfg}, dirqueue.Module(dirqueue.Config{Dir: t.TempDir()}))
	for _, c := range checks {
		if c.Name == "start" {
			if c.Err == nil || !strings.Contains(c.Err.Error(), "outbox.enabled: true") {
				t.Fatalf("start check: %v", c.Err)
			}
			return
		}
	}
	t.Fatalf("no start check in %+v", checks)
}
