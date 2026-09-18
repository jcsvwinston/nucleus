// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package jobsbench

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
	memoryprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/memory"
)

// The ops probes cover what an orchestrator and an on-call operator need from
// an application that runs background work: separate liveness and readiness,
// a health answer per dependency, a profiler that is reachable but not public,
// and the one defect this arc inherits (NU-77).

// probeLiveness measures a liveness endpoint of its own — the one Kubernetes
// restarts a pod over.
func probeLiveness(t *testing.T, e *env) verdict {
	return e.unroutedVerdict(t, "/livez")
}

// probeReadiness measures a readiness endpoint of its own — the one that takes
// a pod out of the load balancer while it drains or waits on a dependency.
func probeReadiness(t *testing.T, e *env) verdict {
	return e.unroutedVerdict(t, "/readyz")
}

// probeHealthPerDependency measures /healthz answering per dependency rather
// than with one opaque word.
func probeHealthPerDependency(t *testing.T, e *env) verdict {
	srv := e.server()
	resp, err := srv.Client().Get(srv.URL("/healthz"))
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /healthz: %v", err)
	}
	var payload struct {
		Status string `json:"status"`
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Logf("/healthz is not JSON: %s", strings.TrimSpace(string(body)))
		return absent
	}
	names := make([]string, 0, len(payload.Checks))
	for _, c := range payload.Checks {
		names = append(names, c.Name+"="+c.Status)
	}
	t.Logf("/healthz says %q with %d checks: %s", payload.Status, len(payload.Checks), strings.Join(names, " "))
	if len(payload.Checks) > 0 {
		return present
	}
	return partial
}

// probeProfilerProtected measures the profiler an on-call engineer reaches for
// when a worker is burning CPU.
func probeProfilerProtected(t *testing.T, e *env) verdict {
	code := e.status(t, http.MethodGet, "/debug/pprof/", nil)
	switch code {
	case http.StatusNotFound:
		t.Log("there is no profiler to protect: /debug/pprof/ is not served")
		return absent
	case http.StatusOK:
		t.Log("the profiler is served to an unauthenticated client")
		return partial
	default:
		t.Logf("/debug/pprof/ answered %d", code)
		return present
	}
}

// probeJobMetrics measures whether the work the queue does produces any
// metric at all. It installs a real meter provider with a manual reader, runs
// a job through the provider an application gets by default, and collects: a
// series named for jobs, or nothing.
//
// The earlier version of this probe asked /metrics of a booted application and
// read a 404 — which measures that the Prometheus exporter is an opt-in module
// this package does not import, not whether jobs are instrumented. That is the
// AUD-05 mistake of the admin bench: an answer that looks like a measurement
// and is about something else.
func probeJobMetrics(t *testing.T, _ *env) verdict {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	m, err := memoryprovider.NewManager(tasks.Config{Concurrency: 1}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new memory manager: %v", err)
	}
	ran := make(chan struct{}, 1)
	if err := m.HandleFunc("bench.metrics", func(context.Context, tasks.Task) error {
		ran <- struct{}{}
		return nil
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = m.Run(ctx) }()
	if _, err := m.EnqueueJSON("bench.metrics", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("the job never ran, so this probe cannot say anything about its metrics")
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var names []string
	for _, scope := range collected.ScopeMetrics {
		for _, series := range scope.Metrics {
			names = append(names, series.Name)
		}
	}
	t.Logf("series recorded while a job ran on the default provider: %d %v", len(names), names)
	for _, n := range names {
		if strings.HasPrefix(n, "jobs.") {
			return present
		}
	}
	return absent
}

// probeSQLiteBusyTimeout measures NU-77 at its cause, deterministically: the
// DSN the framework builds for sqlite carries no busy_timeout, so two writers
// on one file fail instead of waiting. The race it produces is a coin flip on
// macOS and a red CI on Linux; the pragma is neither.
func probeSQLiteBusyTimeout(t *testing.T, _ *env) verdict {
	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.JWTSecret = strings.Repeat("jobsbench-probe-secret", 2)
	// A bare URL: what an application writes, and what the scaffold writes.
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "bare.db")},
	}
	srv := nucleustest.StartApp(t, nucleus.App{Config: cfg})
	db := srv.DB()
	if db == nil {
		t.Fatal("the application exposes no database handle")
	}
	var timeout int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil {
		t.Fatalf("read PRAGMA busy_timeout: %v", err)
	}
	t.Logf("busy_timeout on the connection the framework hands out: %d ms", timeout)
	if timeout > 0 {
		return present
	}
	return absent
}

// probeOutboxBootOrder measures the ordering half of NU-77: whether a module
// gets to create its schema before the outbox dispatcher starts reading. The
// measurement is taken from inside a module's OnStart — if the outbox table is
// already there, the dispatcher went first, which is exactly the contention
// the defect describes.
func probeOutboxBootOrder(t *testing.T, _ *env) verdict {
	cfg := benchConfig(t)
	cfg.Outbox = app.OutboxConfig{Enabled: true, TableName: "bench_boot_outbox"}

	var tableExisted bool
	var probed bool
	mod := nucleus.Module[struct{}]{
		Name: "benchboot",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			probed = true
			db := rt.DB()
			if db == nil {
				return nil
			}
			var name string
			err := db.QueryRow(
				`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`,
				"bench_boot_outbox").Scan(&name)
			tableExisted = err == nil && name != ""
			return nil
		},
	}
	nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{"benchboot": mod.Build()},
	})
	if !probed {
		t.Fatal("the module's OnStart never ran")
	}
	t.Logf("by the time the first module's OnStart ran, the outbox table existed: %v", tableExisted)
	if tableExisted {
		t.Log("the dispatcher reached the database before any module could migrate (NU-77)")
		return absent
	}
	return present
}
