// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// Every test here ends with cancel(), wg.Wait() and j.close(), in that order —
// the order the application shuts down in. The close() is not decoration: the
// scheduler is started with Start(), which takes no context, so cancelling ctx
// stops the worker but leaves the leader renewing its lease against the SQLite
// file in t.TempDir(). The test would then return, TempDir's RemoveAll would
// race a live writer, and the failure surfaces far from its cause as
// "TempDir RemoveAll cleanup: directory not empty" — after the 10-second
// busy_timeout below, which is the only reason it is visible at all.
func sqlTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "jobs.db")+"?_pragma=busy_timeout(10000)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// The wiring itself: selecting the sql provider has to BOOT. It did not — the
// provider leaves the scheduler nil and start() dereferenced it, so every
// application that chose it died on startup. The bench never saw it because it
// drives the provider package directly.
func TestJobsStart_SQLProviderBoots(t *testing.T) {
	j := newModuleJobs(slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := app.DefaultConfig()
	cfg.JobsProvider = "sql"
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); wg.Wait(); j.close() }()

	if err := j.start(ctx, &wg, &cfg, sqlTestDB(t)); err != nil {
		t.Fatalf("start with the sql provider: %v", err)
	}
	if j.manager == nil {
		t.Fatal("the sql provider did not install a manager")
	}
	// And it works as a queue: Runtime.Tasks hands this manager out.
	ran := make(chan struct{}, 1)
	if err := j.manager.HandleFunc("wired", func(context.Context, tasks.Task) error {
		ran <- struct{}{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := j.manager.EnqueueJSON("wired", map[string]string{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
	case <-time.After(10 * time.Second):
		t.Fatal("a job enqueued through the wired manager never ran")
	}
}

// Without a database there is no table to keep the queue in, and the refusal
// has to name a key that exists.
func TestJobsStart_SQLProviderNeedsADatabase(t *testing.T) {
	j := newModuleJobs(slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := app.DefaultConfig()
	cfg.JobsProvider = "sql"
	var wg sync.WaitGroup
	err := j.start(context.Background(), &wg, &cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "SQL database") {
		t.Fatalf("start without a database: %v, want a refusal that says so", err)
	}
	if strings.Contains(err.Error(), "database_url") {
		t.Errorf("the refusal names `database_url`, which is not a configuration key: %v", err)
	}
}

// Scheduled jobs run on the sql provider, under a leader election in the
// application's own database: every replica schedules, one fires.
func TestJobsStart_SQLProviderSchedulesCron(t *testing.T) {
	j := newModuleJobs(slog.New(slog.NewTextHandler(io.Discard, nil)))
	spec := Module[struct{}]{
		Name: "reports",
		Jobs: func(r JobRegistry, _ struct{}) {
			_ = r.Register("nightly", JobSpec{
				Every:   time.Hour,
				Handler: func(context.Context) error { return nil },
			})
		},
	}.Build()
	if err := j.collect(spec); err != nil {
		t.Fatal(err)
	}
	cfg := app.DefaultConfig()
	cfg.JobsProvider = "sql"
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); wg.Wait(); j.close() }()
	if err := j.start(ctx, &wg, &cfg, sqlTestDB(t)); err != nil {
		t.Fatalf("start with cron entries on the sql provider: %v", err)
	}
	if j.scheduler == nil {
		t.Fatal("the sql provider installed no scheduler, so no cron entry would ever fire")
	}
}

// NU-83: whatever displays the queue — Orbit's panel, most of all — needs an
// inspector, and the runtime handed out only the manager. It is an optional
// interface rather than a method on Runtime, because Runtime is published and
// growing it breaks every implementation outside this repository.
func TestRuntime_ExposesTheQueueInspector(t *testing.T) {
	j := newModuleJobs(slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := app.DefaultConfig()
	cfg.JobsProvider = "sql"
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); wg.Wait(); j.close() }()
	if err := j.start(ctx, &wg, &cfg, sqlTestDB(t)); err != nil {
		t.Fatal(err)
	}

	ref := &taskManagerRef{}
	ref.set(j.manager)
	ref.setInspector(j.inspector)
	rt := runtime{tasksRef: ref}

	inspector, ok := TaskInspectorFrom(rt)
	if !ok {
		t.Fatal("the runtime exposes no queue inspector: nothing can display the queue")
	}
	snap := inspector.InspectRuntime()
	if !snap.Enabled {
		t.Fatalf("the inspector answers disabled: %s", snap.Reason)
	}
}

// A runtime with no jobs configured says so, rather than handing out an
// inspector that answers nonsense.
func TestRuntime_NoInspectorWithoutAJobsRuntime(t *testing.T) {
	if _, ok := TaskInspectorFrom(runtime{tasksRef: &taskManagerRef{}}); ok {
		t.Error("a runtime with no jobs runtime handed out an inspector")
	}
}
