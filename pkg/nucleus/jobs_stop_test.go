// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// TestRun_StopTimePerJobsProvider is NU-103's gate: an application stops as
// promptly with the durable queue as with the in-process one, and the queue
// is stopped while the database it lives in is still open.
//
// It was neither. With jobs_provider sql the stop took about nine seconds —
// the heartbeat noticed the shutdown only on its next tick, a third of the
// lease — against under a millisecond for the memory provider. And the jobs
// runtime stopped after core.Run returned, which is after the shutdown hooks
// had closed the database: the scheduler fired its ticks at a closed pool, and
// handing back its leadership lease, the write that spares the next replica a
// whole TTL of no schedule, could never succeed.
//
// The ceiling is one second, against a defect of nine: an order of magnitude
// between them, so a slow host does not make this test red while the defect
// always does. What stops has to be the whole application — HTTP server,
// shutdown hooks, database — booted through the public RunContext, because
// the defect was in the ORDER of those, which no test of the provider alone
// can see.
func TestRun_StopTimePerJobsProvider(t *testing.T) {
	const ceiling = time.Second
	for _, provider := range []string{"memory", "sql"} {
		t.Run(provider, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "stop.db")
			var runs atomic.Int64
			mod := Module[struct{}]{
				Name: "stopper",
				Jobs: func(r JobRegistry, _ struct{}) {
					_ = r.Register("tick", JobSpec{
						Every:   time.Second,
						Handler: func(context.Context) error { runs.Add(1); return nil },
					})
				},
			}

			cfg := app.DefaultConfig()
			cfg.Host = "127.0.0.1"
			cfg.Port = freeLocalPort(t)
			cfg.JobsProvider = provider
			cfg.Databases = map[string]app.DatabaseConfig{
				"default": {URL: "sqlite://" + dbPath},
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runDone := make(chan error, 1)
			go func() {
				runDone <- RunContext(ctx, App{
					Config:  cfg,
					Options: []app.Option{app.WithoutDefaults()},
					Modules: map[string]ModuleSpec{"stopper": mod.Build()},
				})
			}()

			// Serving, and the schedule has fired through the provider at
			// least once: the runtime being stopped is a live one.
			waitServing(t, fmt.Sprintf("http://127.0.0.1:%d/", cfg.Port), runDone)
			waitForRuns(t, &runs, 1, 15*time.Second)

			cancel()
			stopping := time.Now()
			select {
			case err := <-runDone:
				if err != nil {
					t.Fatalf("Run returned %v", err)
				}
			case <-time.After(30 * time.Second):
				t.Fatal("Run did not return within 30s of its context being cancelled")
			}
			took := time.Since(stopping)
			t.Logf("jobs_provider %s: the application stopped in %s", provider, took)
			if took > ceiling {
				t.Errorf("jobs_provider %s: the application took %s to stop, want under %s (NU-103)", provider, took, ceiling)
			}

			if provider != "sql" {
				return
			}
			// The scheduler handed its lease back, which it can only do while
			// the database is open: the jobs runtime stopped first.
			db, err := sql.Open("sqlite", dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			var holders int
			if err := db.QueryRow(`SELECT COUNT(*) FROM "nucleus_jobs_leader"`).Scan(&holders); err != nil {
				t.Fatalf("read the leadership lease: %v", err)
			}
			if holders != 0 {
				t.Errorf("the scheduler's leadership lease is still held after the stop (%d row): it was released against a closed database, so the next replica waits out the TTL", holders)
			}
			var running int
			if err := db.QueryRow(`SELECT COUNT(*) FROM "nucleus_jobs" WHERE status = 'running'`).Scan(&running); err != nil {
				t.Fatalf("read the queue: %v", err)
			}
			if running != 0 {
				t.Errorf("%d job(s) left leased by a process that is gone: the workers' last writes did not reach the database", running)
			}
		})
	}
}

// waitServing blocks until the application answers HTTP, failing if Run
// returns first.
func waitServing(t *testing.T, url string, runDone <-chan error) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("the application did not come up within 10s")
		}
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		select {
		case err := <-runDone:
			t.Fatalf("Run exited during startup: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
