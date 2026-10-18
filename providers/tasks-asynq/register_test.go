// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package asynqprovider

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/internal/testredis"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// syncBuffer serialises the logger's writes (manager goroutines) against the
// test's reads: a bare bytes.Buffer shared between the two is exactly the
// race -race catches.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Importing the package is the wiring: init registers the provider under
// the name jobs_provider selects it by.
func TestInit_RegistersTheProviderWithPkgTasks(t *testing.T) {
	p, ok := tasks.LookupProvider(Name)
	if !ok {
		t.Fatalf("tasks.LookupProvider(%q) finds nothing: the module's init did not register", Name)
	}
	if _, isOurs := p.(provider); !isOurs {
		t.Fatalf("jobs_provider %q resolves to %T, not this module's provider", Name, p)
	}
}

// Open mirrors the validateSemantics rule at the runtime layer: asynq
// without jobs_redis_url is refused naming the key, for programmatic
// callers that never went through the configuration validator.
func TestOpen_RequiresRedisURL(t *testing.T) {
	_, err := provider{}.Open(tasks.ProviderOptions{Config: tasks.Config{RedisURL: "  "}})
	if err == nil || !strings.Contains(err.Error(), "jobs_redis_url") {
		t.Fatalf("want missing-redis-url error, got %v", err)
	}
	if !errors.Is(err, tasks.ErrRedisURLRequired) {
		t.Fatalf("the refusal does not wrap tasks.ErrRedisURLRequired: %v", err)
	}
}

// NF-1 wiring: with jobs_scheduler_lock the scheduler runs under leader
// election, and opting out (jobs_scheduler_lock: false) leaves a WARN in the
// boot log about per-replica duplication. These lines used to be logged by
// pkg/nucleus/jobs.go; the provider owns them now, so the boot log reads
// the same whichever module built the scheduler.
func TestOpen_SchedulerLockWiring(t *testing.T) {
	srv := testredis.Run(t)

	open := func(lock bool) (tasks.ProviderRuntime, *syncBuffer) {
		t.Helper()
		var buf syncBuffer
		rt, err := provider{}.Open(tasks.ProviderOptions{
			Config:        tasks.Config{RedisURL: "redis://" + srv.Addr()},
			SchedulerLock: lock,
			Logger:        slog.New(slog.NewTextHandler(&buf, nil)),
		})
		if err != nil {
			t.Fatalf("Open(lock=%v): %v", lock, err)
		}
		t.Cleanup(func() { _ = rt.Scheduler.Close(); _ = rt.Manager.Close() })
		return rt, &buf
	}

	rt, logs := open(true)
	if _, ok := rt.Scheduler.(*LeaderScheduler); !ok {
		t.Fatalf("with jobs_scheduler_lock the scheduler must be a LeaderScheduler, got %T", rt.Scheduler)
	}
	if !strings.Contains(logs.String(), "leader election") {
		t.Fatalf("lock-enabled boot log does not mention leader election:\n%s", logs.String())
	}
	if rt.Inspector == nil {
		t.Fatal("Open returned no inspector")
	}

	rt2, logs2 := open(false)
	if _, ok := rt2.Scheduler.(*Scheduler); !ok {
		t.Fatalf("with jobs_scheduler_lock disabled the plain scheduler must be used, got %T", rt2.Scheduler)
	}
	if !strings.Contains(logs2.String(), "once per replica") {
		t.Fatalf("opt-out boot log does not WARN about per-replica duplication:\n%s", logs2.String())
	}
}

// Inspect is what `nucleus doctor --check tasks` calls: a reachable server
// with a queue on it answers Enabled, an unreachable one answers a reason
// instead of a panic. (A reachable server with nothing on it is reported as
// nothing discovered, which InspectRuntime has always done.)
func TestInspect_ReportsReachabilityWithoutAWorker(t *testing.T) {
	srv := testredis.Run(t)
	url := "redis://" + srv.Addr()
	mgr, err := NewManager(tasks.Config{RedisURL: url}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	if _, err := mgr.EnqueueJSON("inspect:probe", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("EnqueueJSON: %v", err)
	}
	snap := provider{}.Inspect(tasks.ProviderOptions{Config: tasks.Config{RedisURL: url}})
	if !snap.Enabled {
		t.Fatalf("a reachable Redis with a queue must inspect as enabled: %s", snap.Reason)
	}
	if snap.TotalPending < 1 {
		t.Fatalf("the enqueued task is not in the snapshot: %+v", snap)
	}
	// 127.0.0.1:1 refuses connections immediately: configured-but-down.
	down := provider{}.Inspect(tasks.ProviderOptions{Config: tasks.Config{RedisURL: "redis://127.0.0.1:1/0"}})
	if down.Enabled || down.Reason == "" {
		t.Fatalf("an unreachable Redis must inspect as disabled with a reason: %+v", down)
	}
}

// TestProvider_ExecutesAScheduledJob proves the path end to end against
// miniredis: scheduler entry → Redis queue → worker → handler, through the
// runtime Open returns — what pkg/nucleus/jobs.go drives. The same wiring
// runs against a real Redis in the jobs-redis CI lane (see
// TestProvider_AgainstRealRedis).
func TestProvider_ExecutesAScheduledJob(t *testing.T) {
	mr := testredis.Run(t)
	runExecutionScenario(t, "redis://"+mr.Addr())
}

// TestProvider_AgainstRealRedis is the Redis-real lane for the provider: it
// runs only when NUCLEUS_JOBS_REDIS_URL is set (the jobs-redis CI job
// provides a redis:7 service container) and executes the identical scenario
// as the miniredis test, against a real server.
func TestProvider_AgainstRealRedis(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("NUCLEUS_JOBS_REDIS_URL"))
	if url == "" {
		t.Skip("NUCLEUS_JOBS_REDIS_URL not set; real-Redis jobs lane only")
	}
	runExecutionScenario(t, url)
}

func runExecutionScenario(t *testing.T, redisURL string) {
	t.Helper()
	rt, err := provider{}.Open(tasks.ProviderOptions{
		Config:        tasks.Config{RedisURL: redisURL, Concurrency: 2},
		SchedulerLock: true,
		Logger:        slog.New(slog.NewTextHandler(&syncBuffer{}, nil)),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	const taskType = "nucleus:module:billing:tick"
	var runs atomic.Int64
	if err := rt.Manager.HandleFunc(taskType, func(context.Context, tasks.Task) error { runs.Add(1); return nil }); err != nil {
		t.Fatalf("HandleFunc: %v", err)
	}
	if _, err := rt.Scheduler.RegisterJSON("@every 1s", taskType, nil, tasks.DefaultEnqueuePolicy()); err != nil {
		t.Fatalf("RegisterJSON: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := rt.Manager.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("worker terminated with error: %v", err)
		}
	}()
	if err := rt.Scheduler.Start(); err != nil {
		t.Fatalf("Scheduler.Start: %v", err)
	}
	defer func() {
		_ = rt.Scheduler.Close()
		cancel()
		wg.Wait()
		_ = rt.Manager.Close()
	}()

	// Scheduler tick (1s) + asynq's queue poll interval: allow a wide margin.
	waitForRuns(t, &runs, 2, 30*time.Second)
}

// waitForRuns polls counter until it reaches want or the deadline passes.
func waitForRuns(t *testing.T, counter *atomic.Int64, want int64, deadline time.Duration) {
	t.Helper()
	stop := time.Now().Add(deadline)
	for time.Now().Before(stop) {
		if counter.Load() >= want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("job ran %d times, want at least %d within %v", counter.Load(), want, deadline)
}
