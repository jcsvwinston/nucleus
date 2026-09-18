// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package memoryprovider

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

func quietManager(t *testing.T) (*Manager, context.CancelFunc) {
	t.Helper()
	m, err := NewManager(tasks.Config{Concurrency: 1}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = m.Run(ctx) }()
	t.Cleanup(func() { cancel(); _ = m.Close() })
	return m, cancel
}

// NU-80: a job whose type nobody handles waits for a handler instead of being
// discarded, and the handler arriving is what releases it.
func TestHold_UnhandledTypeWaitsForItsHandler(t *testing.T) {
	m, _ := quietManager(t)

	if _, err := m.EnqueueJSON("late.consumer", map[string]string{"n": "1"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return m.holds.count(kindWaiting) == 1 })

	ran := make(chan struct{}, 1)
	if err := m.HandleFunc("late.consumer", func(context.Context, tasks.Task) error {
		ran <- struct{}{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("the held job did not run when its handler registered")
	}
	waitFor(t, time.Second, func() bool { return m.holds.count(kindWaiting) == 0 })
}

// The lost-wakeup window: a handler registered between the failed lookup and
// the hold would have found the store empty. holdUnhandled re-checks the map
// after holding, so the job cannot be left waiting for a consumer that has
// already arrived.
func TestHold_HandlerRegisteredDuringTheWindow(t *testing.T) {
	m, _ := quietManager(t)

	ran := make(chan struct{}, 1)
	if err := m.HandleFunc("raced.type", func(context.Context, tasks.Task) error {
		ran <- struct{}{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Simulate the window: the worker looked up BEFORE the handler existed,
	// and reaches the hold after it does.
	m.holdUnhandled(enqueuedTask{
		id:     "raced-1",
		task:   &Task{taskType: "raced.type", payload: []byte(`{}`)},
		policy: tasks.DefaultEnqueuePolicy(),
		ctx:    context.Background(),
	})
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("a job held in the lost-wakeup window was never re-dispatched")
	}
}

// A job that exhausts its retries is kept with the reason and the last error,
// and the counter is not moved before the job is held — the invariant the
// snapshot reader depends on.
func TestHold_ExhaustedRetriesAreKept(t *testing.T) {
	m, _ := quietManager(t)

	if err := m.HandleFunc("always.fails", func(context.Context, tasks.Task) error {
		return errors.New("nope")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnqueueJSONWithPolicy("always.fails", nil, tasks.EnqueuePolicy{MaxRetry: 0}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool { return m.holds.count(kindDead) == 1 })

	held := m.holds.takeAll(kindDead)[kindDead]
	if len(held) != 1 {
		t.Fatalf("held %d jobs, want 1", len(held))
	}
	if held[0].reason != reasonRetriesExhausted {
		t.Errorf("reason %q, want %q", held[0].reason, reasonRetriesExhausted)
	}
	if held[0].lastErr != "nope" {
		t.Errorf("lastErr %q, want the handler's error", held[0].lastErr)
	}
	if held[0].attempts != 1 {
		t.Errorf("attempts %d, want 1", held[0].attempts)
	}
}

// The snapshot never reports a failure it cannot show a job for. The provider
// holds before counting and the inspector reads the counters first; this
// hammers both together.
//
// It is a canary, and worth knowing what it can and cannot catch: it fails
// reliably if the two orders are inverted (hold after counting, or store read
// before counters) because the window is then open on every single failure,
// and it says nothing about a window one instruction wide. The invariant is
// kept by the comments at both ends, not by this test alone.
func TestHold_SnapshotNeverShowsFailureWithoutTheJob(t *testing.T) {
	m, _ := quietManager(t)
	insp := NewInspector(m)

	if err := m.HandleFunc("fails.fast", func(context.Context, tasks.Task) error {
		return errors.New("nope")
	}); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var bad atomic.Int64
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			snap := insp.InspectRuntime()
			if snap.TotalFailed > 0 && snap.TotalArchived == 0 {
				bad.Add(1)
			}
		}
	}()
	for i := 0; i < 50; i++ {
		if _, err := m.EnqueueJSONWithPolicy("fails.fast", nil, tasks.EnqueuePolicy{MaxRetry: 0}); err != nil {
			t.Fatal(err)
		}
	}
	<-done
	if n := bad.Load(); n > 0 {
		t.Errorf("%d snapshots reported a failure with nothing held", n)
	}
}

// Purging the dead letter must not throw away work that is only waiting for
// its consumer.
func TestHold_PurgeKeepsWhatIsWaitingForAHandler(t *testing.T) {
	m, _ := quietManager(t)

	if _, err := m.EnqueueJSON("nobody.handles", nil); err != nil {
		t.Fatal(err)
	}
	if err := m.HandleFunc("dies", func(context.Context, tasks.Task) error {
		return errors.New("nope")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnqueueJSONWithPolicy("dies", nil, tasks.EnqueuePolicy{MaxRetry: 0}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool {
		return m.holds.count(kindWaiting) == 1 && m.holds.count(kindDead) == 1
	})

	res, err := NewInspector(m).OperateQueue("default", tasks.QueueActionPurgeArchived)
	if err != nil {
		t.Fatal(err)
	}
	if res.Affected != 1 {
		t.Errorf("purged %d, want 1 (the dead one)", res.Affected)
	}
	if got := m.holds.count(kindWaiting); got != 1 {
		t.Errorf("%d jobs waiting for a handler after the purge, want 1 kept", got)
	}
	if got := m.holds.count(kindDead); got != 0 {
		t.Errorf("%d dead jobs after the purge, want 0", got)
	}
}

// Requeueing onto a stopped manager would move held jobs onto a channel nobody
// reads. It is refused, and the jobs stay held.
func TestHold_RequeueRefusedOnAStoppedManager(t *testing.T) {
	m, err := NewManager(tasks.Config{Concurrency: 1}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	m.holds.hold(kindDead, heldJob{
		id: "dead-1", task: &Task{taskType: "t", payload: []byte(`{}`)},
		policy: tasks.DefaultEnqueuePolicy(), ctx: context.Background(), reason: reasonRetriesExhausted,
	})
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := NewInspector(m).OperateQueue("default", tasks.QueueActionRetryArchived); !errors.Is(err, ErrManagerStopped) {
		t.Fatalf("OperateQueue on a stopped manager: %v, want ErrManagerStopped", err)
	}
	if got := m.holds.count(kindDead); got != 1 {
		t.Errorf("%d dead jobs after the refused requeue, want the job still held", got)
	}
}

// An action this provider cannot perform is refused BY NAME, and the wording
// carries "unsupported" because Orbit's panel classifies the error by
// substring: that is what makes it a 400 rather than a 500.
func TestHold_UnsupportedActionsAreRefusedByName(t *testing.T) {
	m, _ := quietManager(t)
	insp := NewInspector(m)

	for _, action := range []string{tasks.QueueActionPause, tasks.QueueActionUnpause, tasks.QueueActionRetry, tasks.QueueActionArchiveRetry} {
		res, err := insp.OperateQueue("default", action)
		if !errors.Is(err, ErrUnsupportedQueueAction) {
			t.Errorf("%s: %v, want ErrUnsupportedQueueAction", action, err)
		}
		if res.Action != "" {
			t.Errorf("%s: a refused action must not report a result", action)
		}
		if !strings.Contains(err.Error(), "unsupported") {
			t.Errorf("%s: %q does not say 'unsupported', so the panel answers 500 instead of 400", action, err)
		}
	}
}

// Shutdown is a receipt: what was still queued is held and logged, not
// swallowed.
func TestHold_CloseDrainsWhatWasStillQueued(t *testing.T) {
	m, err := NewManager(tasks.Config{Concurrency: 1}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	// Never started: nothing consumes the channel.
	for i := 0; i < 3; i++ {
		if _, err := m.EnqueueJSON("queued.at.shutdown", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if got := m.holds.count(kindDead); got != 3 {
		t.Fatalf("%d jobs held after Close, want 3", got)
	}
	for _, job := range m.holds.takeAll(kindDead)[kindDead] {
		if job.reason != reasonQueueClosed {
			t.Errorf("reason %q, want %q", job.reason, reasonQueueClosed)
		}
	}
}

// The store is bounded: a leak of jobs must not become a leak of the process's
// memory. What the bound pushes out is counted.
func TestHold_StoreIsBounded(t *testing.T) {
	store := newHoldStore(2)
	for i := 0; i < 5; i++ {
		store.hold(kindDead, heldJob{id: string(rune('a' + i)), task: &Task{taskType: "t"}})
	}
	if got := store.count(kindDead); got != 2 {
		t.Errorf("held %d, want the capacity of 2", got)
	}
	if got := store.evictedCount(kindDead); got != 3 {
		t.Errorf("evicted %d, want 3", got)
	}
	// The oldest went first.
	remaining := store.takeAll(kindDead)[kindDead]
	if remaining[0].id != "d" || remaining[1].id != "e" {
		t.Errorf("kept %q and %q, want the two newest", remaining[0].id, remaining[1].id)
	}
}

// The two stores have separate budgets: a mistyped task type cannot evict the
// jobs that actually died.
func TestHold_WaitingCannotEvictTheDead(t *testing.T) {
	store := newHoldStore(2)
	store.hold(kindDead, heldJob{id: "dead", task: &Task{taskType: "real"}})
	for i := 0; i < 10; i++ {
		store.hold(kindWaiting, heldJob{id: "typo", task: &Task{taskType: "mistyped"}})
	}
	if got := store.count(kindDead); got != 1 {
		t.Errorf("%d dead jobs left, want the one that died to survive the typo storm", got)
	}
}

// A requeued job keeps the VALUES of the context it was enqueued with — this
// framework carries the tenant there — and loses its cancellation.
func TestHold_RequeueKeepsContextValuesAndDropsCancellation(t *testing.T) {
	type ctxKey string
	const tenant ctxKey = "tenant"

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), tenant, "acme"))
	detached := detachContext(ctx)
	cancel()

	if err := detached.Err(); err != nil {
		t.Errorf("the detached context was cancelled with the request: %v", err)
	}
	if got := detached.Value(tenant); got != "acme" {
		t.Errorf("tenant %v, want it preserved — a requeued job would run against the wrong keyspace", got)
	}
	if detachContext(nil) == nil {
		t.Error("a nil context must still yield a usable one")
	}
}

func waitFor(t *testing.T, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", limit)
}

// Requeueing everything must not turn a job that is only waiting for its
// handler into a dead one — the next purge would delete it.
func TestHold_RetryArchivedKeepsTheTwoStoresApart(t *testing.T) {
	// No workers: the channel is filled and stays full, so nothing can be
	// requeued and every held job has to go back where it came from.
	m, err := NewManager(tasks.Config{Concurrency: 1}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	insp := NewInspector(m)

	for i := 0; i < cap(m.queue); i++ {
		m.queue <- enqueuedTask{id: "filler", task: &Task{taskType: "filler"}, policy: tasks.DefaultEnqueuePolicy()}
	}
	m.holds.hold(kindWaiting, heldJob{id: "waiting-1", task: &Task{taskType: "waits"}, reason: reasonNoHandler, counted: true})
	m.holds.hold(kindDead, heldJob{id: "dead-1", task: &Task{taskType: "died"}, reason: reasonRetriesExhausted, counted: true})

	if _, err := insp.OperateQueue("default", tasks.QueueActionRetryArchived); err != nil {
		t.Fatal(err)
	}
	if got := m.holds.count(kindWaiting); got != 1 {
		t.Errorf("%d jobs waiting after the requeue, want the waiting one back where it was", got)
	}
	if got := m.holds.count(kindDead); got != 1 {
		t.Errorf("%d dead jobs after the requeue, want 1 — and not the waiting one filed among them", got)
	}
}

// A job put back must not evict the jobs already held: the returning ones are
// older, so the overflow comes off the newest end.
func TestHold_PutBackDoesNotDestroyWhatIsAlreadyHeld(t *testing.T) {
	store := newHoldStore(3)
	for _, id := range []string{"new-1", "new-2", "new-3"} {
		store.hold(kindDead, heldJob{id: id, task: &Task{taskType: "t"}})
	}
	store.putBack(kindDead, []heldJob{{id: "old-1", task: &Task{taskType: "t"}}})

	kept := store.takeAll(kindDead)[kindDead]
	if len(kept) != 3 {
		t.Fatalf("held %d, want the capacity of 3", len(kept))
	}
	if kept[0].id != "old-1" {
		t.Errorf("first held job is %q, want the returned one to survive", kept[0].id)
	}
	if kept[1].id != "new-1" {
		t.Errorf("second held job is %q, want the oldest of the existing ones", kept[1].id)
	}
}

// Requeueing a held job counts as the SAME job, not a new failure: an operator
// pressing the button must not inflate total_failed_today.
func TestHold_RequeueDoesNotRecountTheFailure(t *testing.T) {
	m, _ := quietManager(t)
	insp := NewInspector(m)

	if _, err := m.EnqueueJSON("still.nobody", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool { return m.holds.count(kindWaiting) == 1 })
	failedOnce := insp.InspectRuntime().TotalFailed
	if failedOnce != 1 {
		t.Fatalf("TotalFailed is %d after one unhandled job, want 1", failedOnce)
	}

	for i := 0; i < 3; i++ {
		if _, err := insp.OperateQueue("default", tasks.QueueActionRetryArchived); err != nil {
			t.Fatal(err)
		}
		waitFor(t, 2*time.Second, func() bool { return m.holds.count(kindWaiting) == 1 })
	}
	if got := insp.InspectRuntime().TotalFailed; got != failedOnce {
		t.Errorf("TotalFailed is %d after three requeues of the same job, want it to stay at %d", got, failedOnce)
	}
}

// A queue name this provider does not have is a BAD REQUEST, and the panel
// decides that by substring: the error has to say "unsupported".
func TestHold_UnknownQueueIsRefusedAsABadRequest(t *testing.T) {
	m, _ := quietManager(t)

	_, err := NewInspector(m).OperateQueue("critical", tasks.QueueActionRetryArchived)
	if !errors.Is(err, ErrUnsupportedQueueAction) {
		t.Fatalf("%v, want ErrUnsupportedQueueAction", err)
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("%q does not say 'unsupported', so the panel answers 500 for a bad queue name", err)
	}
}

// Enqueueing onto a stopped manager is refused rather than accepted with an id
// nobody will honour — which also keeps a concurrent producer from feeding the
// shutdown drain forever.
func TestHold_EnqueueAfterCloseIsRefused(t *testing.T) {
	m, err := NewManager(tasks.Config{Concurrency: 1}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnqueueJSON("after.close", nil); !errors.Is(err, ErrManagerStopped) {
		t.Fatalf("enqueue after Close: %v, want ErrManagerStopped", err)
	}
}

// A requeued job can still be stopped: held contexts lose their cancellation
// so they can outlive the request, and the requeue ties them to the manager.
func TestHold_RequeuedJobIsCancelledWhenTheManagerStops(t *testing.T) {
	m, cancelRun := quietManager(t)

	started := make(chan context.Context, 1)
	if err := m.HandleFunc("long.runner", func(ctx context.Context, _ tasks.Task) error {
		started <- ctx
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	m.holds.hold(kindDead, heldJob{
		id: "dead-1", task: &Task{taskType: "long.runner", payload: []byte(`{}`)},
		policy: tasks.DefaultEnqueuePolicy(), ctx: context.Background(),
		reason: reasonRetriesExhausted, counted: true,
	})
	if _, err := NewInspector(m).OperateQueue("default", tasks.QueueActionRetryArchived); err != nil {
		t.Fatal(err)
	}

	var jobCtx context.Context
	select {
	case jobCtx = <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the requeued job never ran")
	}
	cancelRun()
	select {
	case <-jobCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the requeued job's context outlived the manager: nothing could stop it")
	}
}

// Starting and stopping concurrently is what `go Run(ctx)` plus Close() does,
// and a WaitGroup whose Add races another goroutine's Wait is a data race.
// This is the -race regression test for it.
func TestHold_RunAndCloseAreSerialized(t *testing.T) {
	for i := 0; i < 20; i++ {
		m, err := NewManager(tasks.Config{Concurrency: 4}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		go func() { _ = m.Run(ctx) }()
		cancel()
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// A delayed job is not in the channel yet, so the shutdown drain cannot see
// it: it holds itself instead of vanishing while the receipt claims to account
// for everything.
func TestHold_DelayedJobIsHeldAtShutdown(t *testing.T) {
	m, err := NewManager(tasks.Config{Concurrency: 1}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnqueueJSONWithPolicy("later", nil, tasks.EnqueuePolicy{ProcessIn: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool { return m.holds.count(kindDead) == 1 })
}

// The tenant test above exercises detachContext directly. This one goes
// through the Manager: a job enqueued inside a request, held when the request
// is over, and requeued — the handler must still see the request's values,
// because in this framework that is where the tenant lives.
func TestHold_RequeuedJobKeepsTheEnqueuingRequestsValues(t *testing.T) {
	type ctxKey string
	const tenant ctxKey = "tenant"

	m, _ := quietManager(t)

	// The request that enqueues the job, and then ends.
	reqCtx, endRequest := context.WithCancel(context.WithValue(context.Background(), tenant, "acme"))
	if _, err := m.EnqueueJSONCtx(reqCtx, "tenanted", map[string]string{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool { return m.holds.count(kindWaiting) == 1 })
	endRequest()

	seen := make(chan string, 1)
	if err := m.HandleFunc("tenanted", func(ctx context.Context, _ tasks.Task) error {
		v, _ := ctx.Value(tenant).(string)
		seen <- v
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-seen:
		if got != "acme" {
			t.Errorf("the requeued job ran with tenant %q, want \"acme\" — it would write to the wrong keyspace", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the held job never ran after its handler registered")
	}
}
