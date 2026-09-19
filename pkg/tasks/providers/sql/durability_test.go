// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// The gate of arc A7: ten thousand jobs, the process killed while it is
// working on them, and nothing lost.
//
// "Nothing lost" is defined BEFORE it is measured, because the definition is
// where this kind of test usually cheats:
//
//   - every accepted job either ran at least once or is still in the queue,
//     claimable. None may be missing, and none may be stuck in a state nobody
//     will pick up again;
//   - duplicates are COUNTED and reported, not failed. The queue promises
//     at-least-once, and a job whose worker died mid-run legitimately runs
//     again — pretending otherwise would be promising exactly-once, which
//     nothing over a network can honestly give.
//
// The child is killed with SIGKILL, which is what a crash is: no defer runs,
// no lease is released, nothing is flushed. And the test asserts that the kill
// landed while work was IN FLIGHT — otherwise it would pass by killing an idle
// process, which measures nothing.
const (
	durabilityJobs     = 10000
	durabilityChildEnv = "NUCLEUS_DURABILITY_CHILD"
)

func TestSQLProvider_GateDurabilityUnderCrash(t *testing.T) {
	if os.Getenv(durabilityChildEnv) != "" {
		// Running as the child: work the queue until killed.
		runDurabilityChild(t)
		return
	}
	if testing.Short() {
		t.Skip("the durability gate takes ~20s; skipped under -short")
	}

	dir := t.TempDir()
	dbPath := dir + "/gate.db"
	dsn := dbPath + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	store, err := NewStore(db, Config{Flavor: FlavorSQLite, TableName: "gate_jobs"})
	if err != nil {
		t.Fatal(err)
	}

	// Enqueue everything up front: these are the accepted jobs, and the
	// contract is about them.
	now := time.Now().UTC()
	for i := 0; i < durabilityJobs; i++ {
		if err := store.Enqueue(context.Background(), Job{
			ID: fmt.Sprintf("gate-%05d", i), Queue: "default", TaskType: "gate.work",
			Payload: []byte(fmt.Sprintf(`{"n":%d}`, i)), MaxAttempts: 5,
			AvailableAt: now, CreatedAt: now,
		}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	if snap := NewInspector(store).InspectRuntime(); snap.TotalPending != durabilityJobs {
		t.Fatalf("enqueued %d jobs, the queue holds %d", durabilityJobs, snap.TotalPending)
	}

	// A child process works the queue, and is killed mid-flight.
	child := exec.Command(os.Args[0], "-test.run", "TestSQLProvider_GateDurabilityUnderCrash", "-test.v")
	child.Env = append(os.Environ(), durabilityChildEnv+"=1", "NUCLEUS_DURABILITY_DSN="+dsn)
	var childOut strings.Builder
	child.Stdout = &childOut
	child.Stderr = &childOut
	if err := child.Start(); err != nil {
		t.Fatalf("start the worker process: %v", err)
	}

	// Wait until work is genuinely in flight, then kill. Killing an idle
	// process would make this test pass while measuring nothing.
	insp := NewInspector(store)
	killed := false
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		snap := insp.InspectRuntime()
		if snap.TotalCompleted > 100 && snap.TotalActive > 0 {
			if err := child.Process.Kill(); err != nil {
				t.Fatalf("kill the worker: %v", err)
			}
			t.Logf("killed the worker with %d done and %d in flight", snap.TotalCompleted, snap.TotalActive)
			killed = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = child.Wait()
	if !killed {
		t.Fatalf("never caught the worker with jobs in flight; child output:\n%s", childOut.String())
	}

	// What the crash left behind.
	afterCrash := insp.InspectRuntime()
	t.Logf("after the crash: pending=%d active=%d done=%d dead=%d",
		afterCrash.TotalPending, afterCrash.TotalActive, afterCrash.TotalCompleted, afterCrash.TotalArchived)
	if afterCrash.TotalActive == 0 {
		t.Log("note: no job was left leased by the crash, so the recovery path is exercised only if the kill landed mid-handler")
	}

	// A second process picks up where the first left off.
	survivor, err := NewManager(ManagerConfig{
		Store: store, Concurrency: 8, PollInterval: 10 * time.Millisecond,
		LeaseDuration: time.Second, ShutdownGrace: time.Second, Owner: "survivor",
	}, quiet())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	runs := map[string]int{}
	if err := survivor.HandleFunc("gate.work", func(_ context.Context, task tasks.Task) error {
		mu.Lock()
		runs[string(task.Payload())]++
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = survivor.Run(ctx) }()

	// The gate measures whether work SURVIVES a crash, not how fast a runner
	// drains it. A fixed wall-clock budget measures the runner: ten thousand
	// jobs on SQLite at concurrency 8 drain in well under a minute on an idle
	// machine and took longer than two on a loaded CI host, where this failed
	// with 6773 done and nothing wrong. So the budget follows PROGRESS: as
	// long as the queue keeps completing work the wait is extended, and it is
	// a stall — the shape of an actual durability defect, where a job is
	// claimed by nobody and never runs — that fails it.
	const stallBudget = 60 * time.Second
	drained := false
	lastProgress := time.Now()
	completed := -1
	for time.Since(lastProgress) < stallBudget {
		snap := insp.InspectRuntime()
		if snap.TotalPending == 0 && snap.TotalActive == 0 {
			drained = true
			break
		}
		if snap.TotalCompleted != completed {
			completed = snap.TotalCompleted
			lastProgress = time.Now()
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	_ = survivor.Close()
	if !drained {
		snap := insp.InspectRuntime()
		t.Fatalf("the queue stopped making progress for %s with work left: pending=%d active=%d done=%d dead=%d",
			stallBudget, snap.TotalPending, snap.TotalActive, snap.TotalCompleted, snap.TotalArchived)
	}

	// THE ASSERTION. Every accepted job is accounted for, and none died.
	final := insp.InspectRuntime()
	t.Logf("final: done=%d dead=%d pending=%d", final.TotalCompleted, final.TotalArchived, final.TotalPending)
	if final.TotalArchived != 0 {
		t.Errorf("%d job(s) ended in the dead letter: a crash must not fail work", final.TotalArchived)
	}
	if total := final.TotalCompleted + final.TotalArchived + final.TotalPending; total != durabilityJobs {
		t.Fatalf("%d jobs accounted for, %d were accepted: %d are missing",
			total, durabilityJobs, durabilityJobs-total)
	}

	// Duplicates are the price of at-least-once: reported, never failed.
	mu.Lock()
	defer mu.Unlock()
	duplicates := 0
	for _, count := range runs {
		if count > 1 {
			duplicates += count - 1
		}
	}
	t.Logf("at-least-once cost: %d duplicate execution(s) out of %d jobs", duplicates, durabilityJobs)
}

// runDurabilityChild is the process that gets killed.
func runDurabilityChild(t *testing.T) {
	dsn := os.Getenv("NUCLEUS_DURABILITY_DSN")
	if dsn == "" {
		t.Fatal("the child needs NUCLEUS_DURABILITY_DSN")
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db, Config{Flavor: FlavorSQLite, TableName: "gate_jobs"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(ManagerConfig{
		Store: store, Concurrency: 8, PollInterval: 5 * time.Millisecond,
		LeaseDuration: time.Second, ShutdownGrace: time.Second, Owner: "doomed-" + strconv.Itoa(os.Getpid()),
	}, quiet())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.HandleFunc("gate.work", func(context.Context, tasks.Task) error {
		// Slow enough that some are genuinely in flight when the kill lands.
		time.Sleep(time.Millisecond)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Runs until killed. There is no clean exit on purpose: a crash is not a
	// shutdown, and the whole point is that nothing gets to tidy up.
	_ = m.Run(context.Background())
	select {}
}
