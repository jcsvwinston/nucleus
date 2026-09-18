// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

import (
	"context"
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

// End to end: two schedulers, one database, one entry each. Only the leader's
// ticks reach the queue.
func TestScheduler_OnlyTheLeaderFires(t *testing.T) {
	store := newStore(t)
	m := runManager(t, store, ManagerConfig{Concurrency: 1, PollInterval: 10 * time.Millisecond})

	var mu sync.Mutex
	runs := 0
	if err := m.HandleFunc("tick", func(context.Context, tasks.Task) error {
		mu.Lock()
		runs++
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	schedulers := make([]*Scheduler, 0, 3)
	for i := 0; i < 3; i++ {
		sch, err := NewScheduler(SchedulerConfig{
			Manager: m, Store: store, LeaderTTL: time.Second,
			Owner: replicaName(i), Logger: quiet(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sch.RegisterJSON("@every 200ms", "tick", nil, tasks.EnqueuePolicy{MaxRetry: 0}); err != nil {
			t.Fatal(err)
		}
		if err := sch.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sch.Close() })
		schedulers = append(schedulers, sch)
	}

	// Give the election a moment, then count leaders.
	time.Sleep(300 * time.Millisecond)
	leaders := 0
	for _, sch := range schedulers {
		if sch.Leading() {
			leaders++
		}
	}
	if leaders != 1 {
		t.Fatalf("%d of 3 replicas believe they lead, want exactly 1", leaders)
	}

	// Three ticks' worth of time with three replicas running: the count has to
	// look like ONE scheduler, not three.
	time.Sleep(700 * time.Millisecond)
	mu.Lock()
	got := runs
	mu.Unlock()
	t.Logf("three replicas, one @every 200ms entry, ~1s: %d runs", got)
	if got == 0 {
		t.Fatal("the leader never fired")
	}
	if got > 8 {
		t.Fatalf("%d runs: the entry is firing on more than one replica", got)
	}
}

func replicaName(n int) string { return "replica-" + string(rune('a'+n)) }
