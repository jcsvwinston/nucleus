// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	// A shared in-memory database, so two managers in one test see the same
	// queue the way two processes see one file.
	name := fmt.Sprintf("file:jobs_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", name)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(openTestDB(t), Config{Flavor: FlavorSQLite})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func runManager(t *testing.T, store *Store, cfg ManagerConfig) *Manager {
	t.Helper()
	cfg.Store = store
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 20 * time.Millisecond
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 2
	}
	if cfg.ShutdownGrace == 0 {
		// Tests block handlers on purpose; waiting out a real grace period
		// would make the suite take minutes.
		cfg.ShutdownGrace = 200 * time.Millisecond
	}
	m, err := NewManager(cfg, quiet())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = m.Run(ctx) }()
	t.Cleanup(func() { cancel(); _ = m.Close() })
	return m
}

// JOB-03: the provider exists and runs a job with nothing but the database the
// application already has.
func TestSQLProvider_RunsAJob(t *testing.T) {
	store := newStore(t)
	m := runManager(t, store, ManagerConfig{})
	ran := make(chan string, 1)
	if err := m.HandleFunc("greet", func(_ context.Context, task tasks.Task) error {
		ran <- string(task.Payload())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnqueueJSON("greet", map[string]string{"who": "world"}); err != nil {
		t.Fatal(err)
	}
	select {
	case payload := <-ran:
		if payload == "" {
			t.Fatal("the handler got an empty payload")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job never ran")
	}
}

// JOB-02, the point of the whole session: a job accepted before a crash runs
// after it. The first manager claims the job and is killed without a chance to
// finish — no graceful release, which is what a crash is — and a second
// manager picks it up once the lease expires.
func TestSQLProvider_SurvivesACrashMidJob(t *testing.T) {
	store := newStore(t)

	// A manager that claims the job and dies holding it.
	dying, err := NewManager(ManagerConfig{
		Store: store, Concurrency: 1, PollInterval: 10 * time.Millisecond,
		LeaseDuration: 300 * time.Millisecond, Owner: "the-dying-process",
	}, quiet())
	if err != nil {
		t.Fatal(err)
	}
	claimed := make(chan struct{})
	blocked := make(chan struct{})
	if err := dying.HandleFunc("survives", func(context.Context, tasks.Task) error {
		close(claimed)
		<-blocked // never returns: the process is about to be gone
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := dying.EnqueueJSON("survives", map[string]string{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	dyingCtx, killDying := context.WithCancel(context.Background())
	go func() { _ = dying.Run(dyingCtx) }()
	select {
	case <-claimed:
	case <-time.After(5 * time.Second):
		t.Fatal("the first manager never claimed the job")
	}
	// Abandon it: no Close, no release. The goroutine stays blocked, exactly
	// like a process that is gone.
	killDying()

	// A second process, started fresh against the same table.
	survivor := runManager(t, store, ManagerConfig{
		Concurrency: 1, PollInterval: 20 * time.Millisecond,
		LeaseDuration: 300 * time.Millisecond, Owner: "the-surviving-process",
	})
	ran := make(chan struct{}, 1)
	if err := survivor.HandleFunc("survives", func(context.Context, tasks.Task) error {
		ran <- struct{}{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
	case <-time.After(10 * time.Second):
		t.Fatal("the job did not survive the crash: no other worker ever picked it up")
	}
	close(blocked)
}

// JOB-04: named queues, served in the order they are configured — which is
// how priority is expressed.
//
// The claim is exercised directly rather than through a running manager: with
// workers polling, the first job enqueued can be picked up before the second
// one exists, which measures the race and not the order.
func TestSQLProvider_NamedQueuesAreServedInOrder(t *testing.T) {
	store := newStore(t)
	now := time.Now().UTC()

	// Enqueued bulk-first on purpose: the queue ORDER has to win over arrival.
	for _, spec := range []struct{ id, queue string }{
		{"bulk-1", "bulk"},
		{"urgent-1", "urgent"},
	} {
		if err := store.Enqueue(context.Background(), Job{
			ID: spec.id, Queue: spec.queue, TaskType: "work", Payload: []byte(`{}`),
			MaxAttempts: 1, AvailableAt: now, CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	claimed, err := store.Claim(context.Background(), "worker-1", []string{"urgent", "bulk"}, 1, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d jobs, want 1", len(claimed))
	}
	if claimed[0].ID != "urgent-1" {
		t.Errorf("claimed %q, want the urgent queue served before the bulk one", claimed[0].ID)
	}

	// And the second claim takes the bulk one: the order is a preference, not
	// a filter — a queue further down still gets served.
	next, err := store.Claim(context.Background(), "worker-1", []string{"urgent", "bulk"}, 1, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 1 || next[0].ID != "bulk-1" {
		t.Fatalf("second claim = %v, want the bulk job", next)
	}
}

// A named queue is only served to a worker configured for it: work put on a
// queue nobody works is not silently run by everyone.
func TestSQLProvider_AQueueNobodyWorksIsNotServed(t *testing.T) {
	store := newStore(t)
	now := time.Now().UTC()
	if err := store.Enqueue(context.Background(), Job{
		ID: "lonely-1", Queue: "nobody", TaskType: "work", Payload: []byte(`{}`),
		MaxAttempts: 1, AvailableAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.Claim(context.Background(), "worker-1", []string{"default", "urgent"}, 10, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed %d jobs from a queue this worker does not serve", len(claimed))
	}
}

// JOB-06: the retry curve travels with the job.
func TestSQLProvider_RetryCurveIsPerJob(t *testing.T) {
	store := newStore(t)
	m := runManager(t, store, ManagerConfig{Concurrency: 1})

	var mu sync.Mutex
	var at []time.Time
	if err := m.HandleFunc("flaky", func(context.Context, tasks.Task) error {
		mu.Lock()
		at = append(at, time.Now())
		mu.Unlock()
		return errors.New("not yet")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnqueueJSONWithPolicy("flaky", nil, tasks.EnqueuePolicy{
		MaxRetry:    2,
		BackoffBase: 150 * time.Millisecond,
		BackoffMax:  time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(6 * time.Second)
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
		t.Fatalf("the job ran %d times, want 3 attempts", len(at))
	}
	first := at[1].Sub(at[0])
	if first < 120*time.Millisecond {
		t.Errorf("the first retry waited %v, want the job's own BackoffBase of 150ms", first)
	}
	if second := at[2].Sub(at[1]); second <= first {
		t.Errorf("waits were %v then %v, want the curve to grow", first, second)
	}
}

// A job that runs out of attempts is kept in the dead letter, with its reason,
// and can be put back by the operator.
func TestSQLProvider_DeadLetterAndRequeue(t *testing.T) {
	store := newStore(t)
	m := runManager(t, store, ManagerConfig{Concurrency: 1})
	insp := NewInspector(store)

	var mu sync.Mutex
	fail := true
	runs := 0
	if err := m.HandleFunc("dies", func(context.Context, tasks.Task) error {
		mu.Lock()
		runs++
		shouldFail := fail
		mu.Unlock()
		if shouldFail {
			return errors.New("nope")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnqueueJSONWithPolicy("dies", nil, tasks.EnqueuePolicy{MaxRetry: 0}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, func() bool { return insp.InspectRuntime().TotalArchived == 1 })

	mu.Lock()
	fail = false
	before := runs
	mu.Unlock()

	res, err := insp.OperateQueue("default", tasks.QueueActionRetryArchived)
	if err != nil {
		t.Fatal(err)
	}
	if res.Affected != 1 {
		t.Fatalf("requeued %d, want 1", res.Affected)
	}
	waitFor(t, 5*time.Second, func() bool {
		snap := insp.InspectRuntime()
		return snap.TotalCompleted == 1 && snap.TotalArchived == 0
	})
	mu.Lock()
	defer mu.Unlock()
	if runs <= before {
		t.Error("the requeued job never ran again")
	}
}

// A job enqueued in a transaction that rolls back never exists — the oldest
// bug in background work is a job that refers to a row the transaction
// abandoned.
func TestSQLProvider_EnqueueInTransaction(t *testing.T) {
	store := newStore(t)
	m := runManager(t, store, ManagerConfig{Concurrency: 1})
	insp := NewInspector(store)

	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnqueueTx(context.Background(), tx, "never", nil, tasks.DefaultEnqueuePolicy()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if snap := insp.InspectRuntime(); snap.TotalSize != 0 {
		t.Errorf("the queue holds %d jobs after a rollback, want none", snap.TotalSize)
	}
}

// A job whose type nobody handles in THIS process is put back, not consumed:
// another replica may have the handler, and a worker deployed later will.
func TestSQLProvider_UnhandledTypeGoesBack(t *testing.T) {
	store := newStore(t)
	m := runManager(t, store, ManagerConfig{Concurrency: 1})
	insp := NewInspector(store)

	if _, err := m.EnqueueJSON("nobody.handles", nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	snap := insp.InspectRuntime()
	if snap.TotalPending != 1 {
		t.Fatalf("pending=%d archived=%d, want the job waiting for a worker that handles it",
			snap.TotalPending, snap.TotalArchived)
	}
}

// An engine this provider has not been exercised against is refused by name,
// rather than falling through to a default dialect.
func TestSQLProvider_RefusesAnUntestedEngine(t *testing.T) {
	for _, url := range []string{"sqlserver://host/db", "oracle://host/db"} {
		if _, err := NewStore(openTestDB(t), Config{DatabaseURL: url}); !errors.Is(err, ErrUnsupportedFlavor) {
			t.Errorf("NewStore(%q) = %v, want ErrUnsupportedFlavor", url, err)
		}
	}
}

func waitFor(t *testing.T, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", limit)
}

// A job whose type this replica does not handle must NOT burn its attempts.
// It used to: the claim charges one up front, Release did not give it back,
// and a worker polling once a second spent a three-attempt budget in three
// seconds — so the job died without ever having run, on a replica that was
// never going to run it.
func TestSQLProvider_UnhandledTypeDoesNotBurnAttempts(t *testing.T) {
	store := newStore(t)
	runManager(t, store, ManagerConfig{Concurrency: 1, PollInterval: 10 * time.Millisecond})

	now := time.Now().UTC()
	if err := store.Enqueue(context.Background(), Job{
		ID: "unhandled-1", Queue: "default", TaskType: "nobody.handles", Payload: []byte(`{}`),
		MaxAttempts: 3, AvailableAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	// Long enough for dozens of claim/release cycles.
	time.Sleep(600 * time.Millisecond)

	var attempts int
	var status string
	if err := store.db.QueryRow(
		`SELECT attempts, status FROM `+store.table+` WHERE id = 'unhandled-1'`).Scan(&attempts, &status); err != nil {
		t.Fatal(err)
	}
	t.Logf("after 600ms with no handler: attempts=%d status=%s", attempts, status)
	if attempts > 1 {
		t.Errorf("attempts=%d: claiming without running spent the job's budget", attempts)
	}
	if Status(status) != StatusPending {
		t.Errorf("status=%s, want the job still waiting for a worker that handles it", status)
	}
}

// A worker whose lease expired while it ran must not write the outcome over
// whoever owns the job now — that is how a finished job comes back from the
// dead, or a running one is marked done by a straggler.
func TestSQLProvider_OutcomeIsFencedByLeaseOwner(t *testing.T) {
	store := newStore(t)
	now := time.Now().UTC()
	if err := store.Enqueue(context.Background(), Job{
		ID: "fenced-1", Queue: "default", TaskType: "work", Payload: []byte(`{}`),
		MaxAttempts: 3, AvailableAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	// The first worker claims it, then its lease expires.
	first, err := store.Claim(context.Background(), "worker-a", []string{"default"}, 1, time.Millisecond, now)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: %v %v", first, err)
	}
	// A second worker takes it over.
	later := now.Add(time.Second)
	second, err := store.Claim(context.Background(), "worker-b", []string{"default"}, 1, time.Minute, later)
	if err != nil || len(second) != 1 {
		t.Fatalf("takeover claim: %v %v", second, err)
	}

	// Now the straggler finishes and tries to write its outcome.
	owned, err := store.Succeed(context.Background(), "worker-a", "fenced-1", later)
	if err != nil {
		t.Fatal(err)
	}
	if owned {
		t.Error("the straggler was allowed to mark the job done although it no longer held the lease")
	}
	var status, owner string
	if err := store.db.QueryRow(
		`SELECT status, lease_owner FROM `+store.table+` WHERE id = 'fenced-1'`).Scan(&status, &owner); err != nil {
		t.Fatal(err)
	}
	if Status(status) != StatusRunning || owner != "worker-b" {
		t.Errorf("status=%s owner=%s, want the job still running under its current owner", status, owner)
	}
}

// An orderly shutdown must not leave a handler running with an expiring lease:
// that is how the same job ends up running in two processes AT ONCE, which is
// not the duplicate at-least-once forgives — nothing died.
func TestSQLProvider_ShutdownKeepsTheLeaseAliveWhileHandlersRun(t *testing.T) {
	store := newStore(t)
	m, err := NewManager(ManagerConfig{
		Store: store, Concurrency: 1, PollInterval: 10 * time.Millisecond,
		LeaseDuration: 300 * time.Millisecond, ShutdownGrace: 2 * time.Second,
		Owner: "worker-a",
	}, quiet())
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	finish := make(chan struct{})
	if err := m.HandleFunc("slow", func(context.Context, tasks.Task) error {
		close(started)
		<-finish
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnqueueJSON("slow", nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = m.Run(ctx) }()
	<-started

	// Shutdown begins while the handler is still working.
	closed := make(chan struct{})
	go func() { cancel(); close(closed) }()

	// Well past the lease: if the heartbeat had stopped with the shutdown, the
	// job would be claimable by now.
	time.Sleep(700 * time.Millisecond)
	stolen, err := store.Claim(context.Background(), "worker-b", []string{"default"}, 1, time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(stolen) != 0 {
		t.Error("another worker claimed a job that is still running here: it would execute twice at once")
	}
	close(finish)
	<-closed
	_ = m.Close()
}

// JOB-13: the same logical job enqueued twice runs once. A double-clicked
// button, a webhook delivered twice or a retry loop upstream must not become
// duplicated work.
func TestSQLProvider_UniqueKeyCollapsesDuplicates(t *testing.T) {
	store := newStore(t)
	m := runManager(t, store, ManagerConfig{Concurrency: 1, PollInterval: 10 * time.Millisecond})

	release := make(chan struct{})
	var mu sync.Mutex
	runs := 0
	if err := m.HandleFunc("invoice", func(context.Context, tasks.Task) error {
		mu.Lock()
		runs++
		mu.Unlock()
		<-release
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	policy := tasks.EnqueuePolicy{MaxRetry: 0, UniqueKey: "invoice:42"}
	first, err := m.EnqueueJSONWithPolicy("invoice", map[string]int{"id": 42}, policy)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.EnqueueJSONWithPolicy("invoice", map[string]int{"id": 42}, policy)
	if err != nil {
		t.Fatalf("the second enqueue errored instead of collapsing: %v", err)
	}
	if second != first {
		t.Errorf("second enqueue returned %q, want the id of the job already queued (%q)", second, first)
	}
	if snap := NewInspector(store).InspectRuntime(); snap.TotalSize != 1 {
		t.Errorf("the queue holds %d jobs, want 1", snap.TotalSize)
	}

	close(release)
	waitFor(t, 5*time.Second, func() bool { return NewInspector(store).InspectRuntime().TotalCompleted == 1 })

	// The key is freed when the job finishes: the same work can be scheduled
	// again afterwards.
	third, err := m.EnqueueJSONWithPolicy("invoice", map[string]int{"id": 42}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Error("the key was still reserved after the job finished")
	}
}

// A job without a key is unconstrained: several of them queue up as usual.
func TestSQLProvider_NoUniqueKeyMeansNoConstraint(t *testing.T) {
	store := newStore(t)
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := store.Enqueue(context.Background(), Job{
			ID: fmt.Sprintf("plain-%d", i), Queue: "default", TaskType: "work",
			Payload: []byte(`{}`), MaxAttempts: 1, AvailableAt: now, CreatedAt: now,
		}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	if snap := NewInspector(store).InspectRuntime(); snap.TotalPending != 3 {
		t.Errorf("pending=%d, want 3 — a job without a unique key must not collide", snap.TotalPending)
	}
}
