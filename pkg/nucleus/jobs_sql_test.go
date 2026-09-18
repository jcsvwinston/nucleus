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
	defer func() { cancel(); wg.Wait() }()

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

// Scheduled jobs are refused rather than fired once per replica.
func TestJobsStart_SQLProviderRefusesCron(t *testing.T) {
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
	err := j.start(context.Background(), &wg, &cfg, sqlTestDB(t))
	if err == nil || !strings.Contains(err.Error(), "does not run scheduled jobs yet") {
		t.Fatalf("start with cron entries: %v, want the combination refused", err)
	}
}
