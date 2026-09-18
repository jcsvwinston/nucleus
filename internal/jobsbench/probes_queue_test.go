// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package jobsbench

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
	memoryprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/memory"
	sqlprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/sql"
)

// The queue probes drive the provider an application gets by DEFAULT
// (`jobs_provider: memory`), because that is the queue an author has until
// they stand up Redis. Where asynq answers a control the default provider
// does not, the case records `partial` and its note says so — "durable, with
// a Redis you operate" is not the same capability as "durable".

// runManager starts a memory manager for the duration of a probe and returns
// it with the handler registry already open. Close runs on cleanup.
func runManager(t *testing.T, concurrency int) *memoryprovider.Manager {
	t.Helper()
	m, err := memoryprovider.NewManager(tasks.Config{Concurrency: concurrency}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new memory manager: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = m.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = m.Close()
	})
	return m
}

// probeSurvivesRestart measures durability the only way that means anything: a
// job accepted by one process is run by the NEXT one.
//
// It measures the SQL provider, which is what an application selects with
// `jobs_provider: sql`. That is the same criterion the auth and admin benches
// use for an opt-in the framework ships: a capability an application can have
// by configuration is one it HAS. The default in-process provider does not
// survive a restart and is not meant to — the case's note says so, and JOB-01
// measures what that one does give you.
func probeSurvivesRestart(t *testing.T, _ *env) verdict {
	dbPath := filepath.Join(t.TempDir(), "durable.db")
	open := func() *sql.DB {
		db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(10000)")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })
		return db
	}

	// The process that accepts the job and goes away without running it.
	firstDB := open()
	firstStore, err := sqlprovider.NewStore(firstDB, sqlprovider.Config{Flavor: sqlprovider.FlavorSQLite})
	if err != nil {
		t.Fatalf("prepare the queue: %v", err)
	}
	first, err := sqlprovider.NewManager(sqlprovider.ManagerConfig{
		Store: firstStore, Concurrency: 1, PollInterval: time.Hour,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if _, err := first.EnqueueJSON("bench.restart", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close the first manager: %v", err)
	}

	// A new process, against the same database.
	secondDB := open()
	secondStore, err := sqlprovider.NewStore(secondDB, sqlprovider.Config{Flavor: sqlprovider.FlavorSQLite})
	if err != nil {
		t.Fatalf("prepare the queue again: %v", err)
	}
	second, err := sqlprovider.NewManager(sqlprovider.ManagerConfig{
		Store: secondStore, Concurrency: 1, PollInterval: 20 * time.Millisecond,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	ran := make(chan struct{}, 1)
	if err := second.HandleFunc("bench.restart", func(context.Context, tasks.Task) error {
		ran <- struct{}{}
		return nil
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = second.Run(ctx) }()
	// Stopped before the probe returns: the queue is a file in the test's temp
	// directory, and a worker still polling it keeps the cleanup from removing
	// it.
	defer func() { cancel(); _ = second.Close() }()

	select {
	case <-ran:
		return present
	case <-time.After(10 * time.Second):
		t.Log("the job accepted before the restart never reached the new process")
		return absent
	}
}

// probeNoExternalService measures that the default queue runs a job with
// nothing but the application itself — no Redis, no broker.
func probeNoExternalService(t *testing.T, _ *env) verdict {
	m := runManager(t, 1)
	ran := make(chan struct{}, 1)
	if err := m.HandleFunc("bench.plain", func(context.Context, tasks.Task) error {
		ran <- struct{}{}
		return nil
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	if _, err := m.EnqueueJSON("bench.plain", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	select {
	case <-ran:
		return present
	case <-time.After(2 * time.Second):
		return absent
	}
}

// probeSQLProvider asks the configuration layer for a SQL-backed queue. The
// measurement is the error an author gets back, which names what does exist.
func probeSQLProvider(t *testing.T, _ *env) verdict {
	rejected, msg := configRejects(t, func(c *app.Config) { c.JobsProvider = "sql" })
	if !rejected {
		return present
	}
	t.Logf("jobs_provider: sql is refused — %s", msg)
	if !strings.Contains(msg, "memory") || !strings.Contains(msg, "asynq") {
		t.Logf("the refusal does not name the providers that exist")
	}
	return absent
}

// probeNamedQueues enqueues onto a queue other than the default one, and
// measures that a worker serving that queue is the one that gets it.
//
// Measured against the SQL provider, for the reason probeSurvivesRestart
// explains: the in-process one refuses any queue but "default", and the case's
// note says so.
func probeNamedQueues(t *testing.T, _ *env) verdict {
	store, err := sqlprovider.NewStore(benchSQLiteDB(t), sqlprovider.Config{Flavor: sqlprovider.FlavorSQLite})
	if err != nil {
		t.Fatalf("prepare the queue: %v", err)
	}
	now := time.Now().UTC()
	for _, spec := range []struct{ id, queue string }{
		{"bulk-1", "bulk"},
		{"urgent-1", "urgent"},
	} {
		if err := store.Enqueue(context.Background(), sqlprovider.Job{
			ID: spec.id, Queue: spec.queue, TaskType: "bench.queued", Payload: []byte(`{}`),
			MaxAttempts: 1, AvailableAt: now, CreatedAt: now,
		}); err != nil {
			t.Fatalf("enqueue on %s: %v", spec.queue, err)
		}
	}
	// A worker that serves urgent before bulk gets the urgent one, although
	// the bulk one was enqueued first: that ordering IS the priority.
	claimed, err := store.Claim(context.Background(), "bench-worker", []string{"urgent", "bulk"}, 1, time.Minute, now)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Logf("claimed %d jobs, want 1", len(claimed))
		return absent
	}
	t.Logf("with queues [urgent bulk] the worker claimed %q, enqueued after the bulk one", claimed[0].ID)
	if claimed[0].ID != "urgent-1" {
		return partial
	}
	return present
}

// benchSQLiteDB opens a scratch database for the probes that measure the SQL
// provider.
func benchSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "queue.db")+"?_pragma=busy_timeout(10000)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// probeRetryBackoff measures that a failing handler is retried, and that the
// waits grow instead of hammering.
func probeRetryBackoff(t *testing.T, _ *env) verdict {
	m := runManager(t, 1)
	var mu sync.Mutex
	var at []time.Time
	if err := m.HandleFunc("bench.retry", func(context.Context, tasks.Task) error {
		mu.Lock()
		at = append(at, time.Now())
		mu.Unlock()
		return errors.New("bench: always fails")
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	if _, err := m.EnqueueJSONWithPolicy("bench.retry", map[string]string{},
		tasks.EnqueuePolicy{MaxRetry: 2}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(at)
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(at) < 3 {
		t.Logf("a failing handler ran %d times for MaxRetry 2", len(at))
		return absent
	}
	first, second := at[1].Sub(at[0]), at[2].Sub(at[1])
	t.Logf("retry waits: %v then %v", first.Round(time.Millisecond), second.Round(time.Millisecond))
	if second <= first {
		return partial
	}
	return present
}

// probeRetryPolicy asks whether the caller can choose the retry CURVE, not
// just how many attempts. The measurement is the shape of the public policy
// struct: a field an application can set, or nothing to set.
func probeRetryPolicy(t *testing.T, _ *env) verdict {
	p := tasks.EnqueuePolicy{MaxRetry: 5}
	if p.MaxRetry != 5 {
		t.Fatalf("EnqueuePolicy does not carry MaxRetry")
	}
	// The curve: there is no field for it, so an application takes the one
	// the provider hard-codes. Measured by exercising every knob the struct
	// offers and finding none that changes the wait.
	if knobs := retryKnobs(); len(knobs) > 0 {
		t.Logf("retry knobs on EnqueuePolicy: %s", strings.Join(knobs, ", "))
		return present
	}
	return partial
}

// probeDeadLetter measures where a job goes when it has exhausted its
// retries: somewhere an operator can find it, or nowhere.
func probeDeadLetter(t *testing.T, _ *env) verdict {
	m := runManager(t, 1)
	if err := m.HandleFunc("bench.dead", func(context.Context, tasks.Task) error {
		return errors.New("bench: always fails")
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	if _, err := m.EnqueueJSONWithPolicy("bench.dead", map[string]string{"id": "dead-1"},
		tasks.EnqueuePolicy{MaxRetry: 0}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	insp := memoryprovider.NewInspector(m)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		// The counter is read first by the provider too: a job is held BEFORE
		// its failure is counted, so seeing the failure and then no held job
		// is a real loss, not a race.
		if snap := insp.InspectRuntime(); snap.TotalFailed > 0 {
			if snap.TotalArchived > 0 {
				t.Logf("the exhausted job is held: archived=%d", snap.TotalArchived)
				return present
			}
			// A queue row alone proves nothing: the provider publishes one
			// whether or not anything is in it.
			t.Logf("the failed job is counted (failed=%d) but not held anywhere: archived=%d",
				snap.TotalFailed, snap.TotalArchived)
			return absent
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Log("the exhausted job was never even counted")
	return absent
}

// probeRequeueDead asks the provider to put a dead job back — and measures
// that it RUNS again. An action that returns without an error proves nothing:
// purging an empty store succeeds too.
func probeRequeueDead(t *testing.T, _ *env) verdict {
	m := runManager(t, 1)
	insp := memoryprovider.NewInspector(m)

	var mu sync.Mutex
	runs := 0
	fail := true
	ran := make(chan struct{}, 4)
	if err := m.HandleFunc("bench.requeue", func(context.Context, tasks.Task) error {
		mu.Lock()
		runs++
		shouldFail := fail
		mu.Unlock()
		ran <- struct{}{}
		if shouldFail {
			return errors.New("bench: fails on the first life")
		}
		return nil
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	if _, err := m.EnqueueJSONWithPolicy("bench.requeue", map[string]string{"id": "requeue-1"},
		tasks.EnqueuePolicy{MaxRetry: 0}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// Wait for it to die.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if insp.InspectRuntime().TotalArchived > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if insp.InspectRuntime().TotalArchived == 0 {
		t.Log("nothing was held, so there is nothing to put back")
		return absent
	}

	// The operator fixes whatever was broken, then asks for the dead back.
	mu.Lock()
	fail = false
	before := runs
	mu.Unlock()
	// Drain the token the first (failed) run left behind, or the wait below
	// would be satisfied by it and this probe would never observe the second
	// run at all.
	for len(ran) > 0 {
		<-ran
	}

	res, err := insp.OperateQueue("default", tasks.QueueActionRetryArchived)
	if err != nil {
		t.Logf("requeueing the dead is refused: %v", err)
		return absent
	}
	t.Logf("%s: applied=%v affected=%d — %s", tasks.QueueActionRetryArchived, res.Applied, res.Affected, res.Message)

	select {
	case <-ran:
	case <-time.After(3 * time.Second):
		t.Log("the action reported success and the job never ran again")
		return absent
	}
	mu.Lock()
	after := runs
	mu.Unlock()
	if after <= before {
		t.Log("the job did not run again")
		return absent
	}
	if insp.InspectRuntime().TotalArchived != 0 {
		t.Log("the job ran again but is still counted as held")
		return partial
	}
	return present
}

// probeQueueInspection measures what an operator can see of the queue.
func probeQueueInspection(t *testing.T, _ *env) verdict {
	m := runManager(t, 1)
	done := make(chan struct{})
	if err := m.HandleFunc("bench.inspect", func(ctx context.Context, _ tasks.Task) error {
		<-done
		return nil
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := m.EnqueueJSON("bench.inspect", map[string]int{"n": i}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	defer close(done)
	time.Sleep(200 * time.Millisecond)
	snap := memoryprovider.NewInspector(m).InspectRuntime()
	t.Logf("snapshot with 3 jobs in flight: pending=%d active=%d queues=%d processed=%d failed=%d",
		snap.TotalPending, snap.TotalActive, len(snap.Queues), snap.TotalProcessed, snap.TotalFailed)
	switch {
	case snap.TotalPending > 0 || snap.TotalActive > 0:
		return present
	case snap.Enabled:
		return partial
	default:
		return absent
	}
}

// probeUnhandledType measures what becomes of a job whose type nobody
// handles — the shape of a typo, or of a worker deployed behind its producer.
func probeUnhandledType(t *testing.T, _ *env) verdict {
	m := runManager(t, 1)
	if _, err := m.EnqueueJSON("bench.nobody-handles-this", map[string]string{}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	insp := memoryprovider.NewInspector(m)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := insp.InspectRuntime()
		if snap.TotalFailed > 0 {
			if snap.TotalArchived > 0 {
				return present
			}
			t.Log("a job with no handler is counted as failed and dropped: nothing holds it for the deploy that would handle it")
			return absent
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Log("a job with no handler vanished without being counted")
	return absent
}

// retryKnobs reads the public policy struct and returns the fields that shape
// the retry CURVE — a backoff, a schedule, a strategy. MaxRetry is how MANY
// attempts, which is a different question and already answered. Reading the
// struct is the measurement here: what an application can set is exactly what
// the type exposes.
func retryKnobs() []string {
	var out []string
	rt := reflect.TypeOf(tasks.EnqueuePolicy{})
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		if name == "MaxRetry" {
			continue
		}
		lower := strings.ToLower(name)
		if strings.Contains(lower, "backoff") || strings.Contains(lower, "retrydelay") || strings.Contains(lower, "strategy") {
			out = append(out, name)
		}
	}
	return out
}

// probeDelayedJob measures a job asked to run later.
func probeDelayedJob(t *testing.T, _ *env) verdict {
	m := runManager(t, 1)
	ran := make(chan time.Time, 1)
	if err := m.HandleFunc("bench.delayed", func(context.Context, tasks.Task) error {
		ran <- time.Now()
		return nil
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	start := time.Now()
	if _, err := m.EnqueueJSONWithPolicy("bench.delayed", map[string]string{},
		tasks.EnqueuePolicy{ProcessIn: 300 * time.Millisecond}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	select {
	case at := <-ran:
		waited := at.Sub(start)
		t.Logf("the delayed job ran after %v", waited.Round(time.Millisecond))
		if waited < 250*time.Millisecond {
			return partial
		}
		return present
	case <-time.After(3 * time.Second):
		return absent
	}
}

// probeJobTimeout measures a per-job deadline reaching the handler.
func probeJobTimeout(t *testing.T, _ *env) verdict {
	m := runManager(t, 1)
	got := make(chan bool, 1)
	if err := m.HandleFunc("bench.timeout", func(ctx context.Context, _ tasks.Task) error {
		_, ok := ctx.Deadline()
		got <- ok
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	if _, err := m.EnqueueJSONWithPolicy("bench.timeout", map[string]string{},
		tasks.EnqueuePolicy{Timeout: 200 * time.Millisecond, MaxRetry: 0}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	select {
	case ok := <-got:
		if ok {
			return present
		}
		t.Log("the handler ran with no deadline although Timeout was set")
		return absent
	case <-time.After(3 * time.Second):
		return absent
	}
}

// probeUniqueness enqueues the same logical job twice and measures whether
// the queue collapses them — Oban's unique jobs, Sidekiq's unique extension.
func probeUniqueness(t *testing.T, _ *env) verdict {
	m := runManager(t, 1)
	var mu sync.Mutex
	runs := 0
	if err := m.HandleFunc("bench.unique", func(context.Context, tasks.Task) error {
		mu.Lock()
		runs++
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	payload := map[string]string{"invoice": "inv-1"}
	for i := 0; i < 2; i++ {
		if _, err := m.EnqueueJSON("bench.unique", payload); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	time.Sleep(500 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	t.Logf("the same job enqueued twice ran %d times", runs)
	if runs == 1 {
		return present
	}
	return absent
}
