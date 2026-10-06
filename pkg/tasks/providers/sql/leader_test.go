// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// Exactly one replica holds the lease. Without that, every replica fires every
// cron entry on every tick — the defect NF-1 fixed for asynq with a Redis
// lock, and the reason this provider refused cron until now.
func TestLeader_OnlyOneReplicaWins(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	var mu sync.Mutex
	winners := 0
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			got, err := store.AcquireLeadership(ctx, "scheduler", replicaName(n), time.Minute, now)
			if err != nil {
				t.Errorf("replica %d: %v", n, err)
				return
			}
			if got {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("%d replicas believe they are the leader, want exactly 1", winners)
	}
}

// The leader keeps it by renewing, and nobody else can take it meanwhile.
func TestLeader_RenewsAndIsNotStolen(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if got, err := store.AcquireLeadership(ctx, "scheduler", "replica-a", time.Minute, now); err != nil || !got {
		t.Fatalf("first acquire: got=%v err=%v", got, err)
	}
	// Renewal by the same owner succeeds...
	if got, err := store.AcquireLeadership(ctx, "scheduler", "replica-a", time.Minute, now.Add(time.Second)); err != nil || !got {
		t.Fatalf("renew: got=%v err=%v", got, err)
	}
	// ...and a second replica cannot take a live lease.
	if got, err := store.AcquireLeadership(ctx, "scheduler", "replica-b", time.Minute, now.Add(2*time.Second)); err != nil || got {
		t.Fatalf("steal attempt: got=%v err=%v, want the live lease left alone", got, err)
	}
}

// A leader that dies is replaced within one TTL, with no coordination.
func TestLeader_ExpiredLeaseIsTakenOver(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if got, err := store.AcquireLeadership(ctx, "scheduler", "the-dead-replica", time.Second, now); err != nil || !got {
		t.Fatalf("first acquire: got=%v err=%v", got, err)
	}
	// The clock moves past the lease; nobody renewed it.
	later := now.Add(2 * time.Second)
	if got, err := store.AcquireLeadership(ctx, "scheduler", "the-live-replica", time.Minute, later); err != nil || !got {
		t.Fatalf("takeover: got=%v err=%v, want the expired lease claimable", got, err)
	}
}

// Stopping cleanly gives the lease up instead of making everyone wait it out —
// and a former leader cannot release the lease of the one that replaced it.
func TestLeader_ReleaseIsFencedByOwner(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := store.AcquireLeadership(ctx, "scheduler", "replica-a", time.Minute, now); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseLeadership(ctx, "scheduler", "replica-b"); err != nil {
		t.Fatal(err)
	}
	// replica-b's release did nothing: replica-a still leads.
	if got, err := store.AcquireLeadership(ctx, "scheduler", "replica-c", time.Minute, now.Add(time.Second)); err != nil || got {
		t.Fatalf("after a foreign release: got=%v err=%v, want replica-a still holding it", got, err)
	}
	if err := store.ReleaseLeadership(ctx, "scheduler", "replica-a"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.AcquireLeadership(ctx, "scheduler", "replica-c", time.Minute, now.Add(2*time.Second)); err != nil || !got {
		t.Fatalf("after the owner released: got=%v err=%v, want the lease free", got, err)
	}
}

// End to end: three replicas on one database, the same entry on each. Every
// tick reaches the queue from the leader, and never from a follower.
//
// It waits for events, each under a ceiling, and never for a window (NU-116).
// The window it used to read was also not measuring what it said: cron rounds
// an @every below one second up to one second and fires on the whole second,
// so "@every 200ms" over "~1s" was ONE tick, landing anywhere in that second.
// When it landed before the election had finished, or too late for the job to
// run before the window closed, the test reported that the leader never fired:
// three runs in two hundred under -race, and in CI with and without it. Nor
// could the bound of eight runs it checked ever trip: with the election
// switched off, all three replicas fired that one tick, which made three runs,
// and the test passed.
func TestScheduler_OnlyTheLeaderFires(t *testing.T) {
	store := newStore(t)
	m := runManager(t, store, ManagerConfig{Concurrency: 1, PollInterval: 10 * time.Millisecond})
	insp := NewInspector(store)
	fired := &firings{}
	if err := m.HandleFunc("tick", fired.handle); err != nil {
		t.Fatal(err)
	}

	// A lease far longer than the test, so leadership cannot move while it
	// runs: every tick has one rightful leader, and a run from anybody else
	// is a second replica firing, not a handover.
	schedulers := make([]*Scheduler, 0, 3)
	for i := 0; i < 3; i++ {
		schedulers = append(schedulers, startTickScheduler(t, m, store, replicaName(i), time.Minute))
	}

	waitFor(t, 10*time.Second, func() bool {
		for _, sch := range schedulers {
			if sch.Leading() {
				return true
			}
		}
		return false
	})
	var leaders []string
	for i, sch := range schedulers {
		if sch.Leading() {
			leaders = append(leaders, replicaName(i))
		}
	}
	if len(leaders) != 1 {
		t.Fatalf("%v believe they lead, want exactly one replica", leaders)
	}
	leader := leaders[0]

	// Two ticks fired by the leader. They are whole seconds, and every
	// follower's cron ticked on them too and had its chance to fire.
	waitFor(t, 15*time.Second, func() bool { return fired.count(leader) >= 2 })

	// Stop every replica — Close waits for a tick that is still enqueueing —
	// and let the queue drain, so every tick that reached it has run before
	// the count is read.
	for _, sch := range schedulers {
		_ = sch.Close()
	}
	waitFor(t, 10*time.Second, func() bool {
		snap := insp.InspectRuntime()
		return snap.TotalPending == 0 && snap.TotalActive == 0
	})

	got := fired.all()
	t.Logf("three replicas, one @every 1s entry, %s leading: runs by replica %v", leader, got)
	for replica, n := range got {
		if replica != leader {
			t.Errorf("%s fired %d tick(s) while %s held the lease: the entry is firing on more than one replica", replica, n, leader)
		}
	}
}

// A leader whose renewal is late stops firing when its lease runs out, not
// when the late renewal finally comes back (NU-116).
//
// It used to go on firing. The flag each tick checked only changed when a
// renewal returned, and a renewal can return later than the lease lasts — a
// database slow to answer, a process starved of CPU or paused. By then another
// replica had taken the expired lease over, as it should, and both fired every
// tick until the late renewal gave up five seconds on.
//
// Replica a's renewals are held up by taking the only connection its store
// has; replica b reaches the same database through a store of its own. Both
// enqueue through one manager on b's connections, so a tick that a fires is a
// run like any other, and the payload says who fired it.
func TestScheduler_ALeaderWhoseRenewalIsLateStopsFiring(t *testing.T) {
	dsn := t.TempDir() + "/leader.db?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
	open := func() (*sql.DB, *Store) {
		t.Helper()
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })
		store, err := NewStore(db, Config{Flavor: FlavorSQLite})
		if err != nil {
			t.Fatalf("new store: %v", err)
		}
		return db, store
	}
	dbA, storeA := open()
	dbA.SetMaxOpenConns(1)
	_, storeB := open()

	m := runManager(t, storeB, ManagerConfig{Concurrency: 1, PollInterval: 10 * time.Millisecond})
	fired := &firings{}
	if err := m.HandleFunc("tick", fired.handle); err != nil {
		t.Fatal(err)
	}

	const ttl = time.Second
	a := startTickScheduler(t, m, storeA, "replica-a", ttl)
	waitFor(t, 10*time.Second, a.Leading)

	// From here a cannot renew: each renewal waits for the connection until
	// its own five-second timeout, and its lease runs out within one second.
	conn, err := dbA.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	b := startTickScheduler(t, m, storeB, "replica-b", ttl)
	waitFor(t, 10*time.Second, b.Leading)
	if a.Leading() {
		t.Fatal("replica-a still reports leading after its lease ran out and replica-b took it over: both fire every tick")
	}

	// And the ticks agree with it. a's last rightful tick was enqueued before
	// its lease ran out, so before any tick of b's, and the queue is served
	// in order: by b's first run it has run. Two ticks later a must not have
	// fired again.
	waitFor(t, 10*time.Second, func() bool { return fired.count("replica-b") >= 1 })
	before := fired.count("replica-a")
	waitFor(t, 10*time.Second, func() bool { return fired.count("replica-b") >= 3 })
	if after := fired.count("replica-a"); after != before {
		t.Fatalf("replica-a fired %d tick(s) after replica-b took the lease over: both replicas are firing the entry", after-before)
	}
}

// startTickScheduler starts one replica's scheduler with the "tick" entry,
// every second — cron's floor: an @every below a second is rounded up to it —
// and the replica's name as the payload, so a run says who fired it.
func startTickScheduler(t *testing.T, m *Manager, store *Store, owner string, ttl time.Duration) *Scheduler {
	t.Helper()
	sch, err := NewScheduler(SchedulerConfig{
		Manager: m, Store: store, LeaderTTL: ttl, Owner: owner, Logger: quiet(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sch.RegisterJSON("@every 1s", "tick", map[string]string{"replica": owner}, tasks.EnqueuePolicy{MaxRetry: 0}); err != nil {
		t.Fatal(err)
	}
	if err := sch.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sch.Close() })
	return sch
}

// firings counts the runs of "tick" by the replica that fired them.
type firings struct {
	mu sync.Mutex
	by map[string]int
}

func (f *firings) handle(_ context.Context, task tasks.Task) error {
	var payload struct {
		Replica string `json:"replica"`
	}
	// An unreadable payload is counted under "", which no replica is called:
	// it shows up as a run nobody should have fired.
	_ = json.Unmarshal(task.Payload(), &payload)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.by == nil {
		f.by = map[string]int{}
	}
	f.by[payload.Replica]++
	return nil
}

func (f *firings) count(replica string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.by[replica]
}

func (f *firings) all() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]int, len(f.by))
	for replica, n := range f.by {
		out[replica] = n
	}
	return out
}

func replicaName(n int) string { return "replica-" + string(rune('a'+n)) }
