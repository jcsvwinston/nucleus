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
	"github.com/jcsvwinston/nucleus/pkg/outbox"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
	memoryprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/memory"
)

// The ops probes cover what an orchestrator and an on-call operator need from
// an application that runs background work: separate liveness and readiness,
// a health answer per dependency, a profiler that is reachable but not public,
// and the one defect this arc inherits (NU-77).

// probeLiveness measures a liveness endpoint of its own — the one an
// orchestrator restarts a pod over, which must NOT fail because a dependency
// is down.
func probeLiveness(t *testing.T, e *env) verdict {
	code := e.status(t, http.MethodGet, "/livez", nil)
	t.Logf("GET /livez answered %d", code)
	if code == http.StatusNotFound {
		return absent
	}
	if code != http.StatusOK {
		return partial
	}
	return present
}

// probeReadiness measures a readiness endpoint of its own — the one that takes
// an instance out of the load balancer while it drains or waits on a
// dependency, without the orchestrator killing it.
func probeReadiness(t *testing.T, e *env) verdict {
	srv := e.server()
	resp, err := srv.Client().Get(srv.URL("/readyz"))
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return absent
	}
	body, _ := io.ReadAll(resp.Body)
	var payload struct {
		Status string `json:"status"`
		Checks []struct {
			Name string `json:"name"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Logf("/readyz is not JSON: %s", strings.TrimSpace(string(body)))
		return partial
	}
	t.Logf("GET /readyz answered %d, status %q with %d dependency checks",
		resp.StatusCode, payload.Status, len(payload.Checks))
	if resp.StatusCode != http.StatusOK || len(payload.Checks) == 0 {
		return partial
	}
	return present
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
	// Off by default: an application that has not asked for it serves nothing
	// there, which is the right answer for a surface that exposes process
	// memory.
	if code := e.status(t, http.MethodGet, "/debug/pprof/", nil); code != http.StatusNotFound {
		t.Logf("a default application answers %d at /debug/pprof/, so the profiler is on without being asked for", code)
		return partial
	}

	// Turned on, it exists AND it is not public: the bootstrap allow-list
	// covers /healthz and /readyz, never this.
	cfg := benchConfig(t)
	cfg.ProfilingEnabled = true
	srv := nucleustest.StartApp(t, nucleus.App{Config: cfg})
	resp, err := srv.Client().Get(srv.URL("/debug/pprof/"))
	if err != nil {
		t.Fatalf("GET /debug/pprof/: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	t.Logf("with profiling_enabled, an unauthenticated GET /debug/pprof/ answers %d", resp.StatusCode)
	switch resp.StatusCode {
	case http.StatusNotFound:
		t.Log("profiling_enabled did not mount anything")
		return absent
	case http.StatusOK:
		t.Log("the profiler answered an unauthenticated client: heap and goroutine dumps are public")
		return partial
	default:
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

// probeOutboxBootOrder measures the ordering half of NU-77: whether the
// dispatcher is already polling the database while modules are still starting.
//
// The first version of this probe asked whether the outbox TABLE existed
// during a module's OnStart, and that measures the wrong thing — creating the
// table once costs nothing and races nobody. What cost an application its boot
// was the POLLING: a dispatcher reading and leasing every second while a
// module migrated, which on SQLite is one writer too many.
//
// So the question is when delivery begins. An application that has not been
// started does not deliver; it still ACCEPTS messages, which is the safe half.
func probeOutboxBootOrder(t *testing.T, _ *env) verdict {
	cfg := benchConfig(t)
	cfg.Outbox = app.OutboxConfig{Enabled: true, TableName: "bench_boot_outbox"}

	application, err := app.New(&cfg)
	if err != nil {
		t.Fatalf("build the application: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = application.Shutdown(ctx)
	})
	if application.Outbox == nil {
		t.Log("the outbox is enabled in configuration but the application exposes none")
		return absent
	}

	// Accepted before anything is started: an enqueue is not delivery.
	if _, err := application.Outbox.Enqueue(context.Background(), outbox.Entry{
		Topic:   "bench.boot",
		Payload: map[string]string{"k": "v"},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// A dispatcher that was already polling would have leased this by now.
	time.Sleep(1500 * time.Millisecond)
	snap := application.Outbox.Snapshot(context.Background())
	t.Logf("1.5s after app.New, with nothing started: pending=%d processing=%d",
		snap.Pending, snap.Processing)
	if snap.Processing > 0 || snap.Pending == 0 {
		t.Log("the dispatcher is polling before the application was started: it races whatever a module does in OnStart")
		return absent
	}
	return present
}
