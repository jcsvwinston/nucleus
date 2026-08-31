package asynqprovider

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

func testLeaderScheduler(t *testing.T, addr, owner string) *LeaderScheduler {
	t.Helper()
	s, err := NewLeaderScheduler(LeaderSchedulerConfig{
		Scheduler: SchedulerConfig{RedisURL: "redis://" + addr},
		Owner:     owner,
		LockTTL:   500 * time.Millisecond,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewLeaderScheduler(%s): %v", owner, err)
	}
	return s
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(msg)
}

// NF-1: with two replicas contending on the same Redis, exactly one becomes
// leader and ticks the schedule; the other stays dormant.
func TestLeaderSchedulerExactlyOneLeader(t *testing.T) {
	srv := miniredis.RunT(t)

	a := testLeaderScheduler(t, srv.Addr(), "replica-a")
	b := testLeaderScheduler(t, srv.Addr(), "replica-b")

	for _, s := range []*LeaderScheduler{a, b} {
		if _, err := s.RegisterJSON("@every 1h", "test:tick", nil, tasks.DefaultEnqueuePolicy()); err != nil {
			t.Fatalf("RegisterJSON: %v", err)
		}
		if err := s.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
	}
	defer func() { _ = a.Close(); _ = b.Close() }()

	waitFor(t, 5*time.Second, func() bool { return a.IsLeader() || b.IsLeader() },
		"no replica acquired leadership within 5s")

	// Give the loser a moment to (incorrectly) also become leader.
	time.Sleep(200 * time.Millisecond)
	if a.IsLeader() && b.IsLeader() {
		t.Fatal("both replicas claim leadership — the lock is not exclusive")
	}
}

// When the leader shuts down it releases the lock, and the surviving
// replica takes over without waiting for the TTL.
func TestLeaderSchedulerFailover(t *testing.T) {
	srv := miniredis.RunT(t)

	a := testLeaderScheduler(t, srv.Addr(), "replica-a")
	if err := a.Start(); err != nil {
		t.Fatalf("Start a: %v", err)
	}
	waitFor(t, 5*time.Second, a.IsLeader, "first replica did not acquire leadership")

	b := testLeaderScheduler(t, srv.Addr(), "replica-b")
	if _, err := b.RegisterJSON("@every 1h", "test:tick", nil, tasks.DefaultEnqueuePolicy()); err != nil {
		t.Fatalf("RegisterJSON b: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("Start b: %v", err)
	}
	defer func() { _ = b.Close() }()

	if b.IsLeader() {
		t.Fatal("second replica became leader while the first still holds the lock")
	}

	if err := a.Close(); err != nil {
		t.Fatalf("Close a: %v", err)
	}
	waitFor(t, 5*time.Second, b.IsLeader, "surviving replica did not take over after the leader released the lock")
}

// A registration made before leadership is replayed onto the scheduler the
// election builds, and Unregister on the synthetic id works in both states.
func TestLeaderSchedulerReplaysRegistrations(t *testing.T) {
	srv := miniredis.RunT(t)
	s := testLeaderScheduler(t, srv.Addr(), "replica-a")

	id, err := s.RegisterJSON("@every 1h", "test:tick", map[string]any{"k": "v"}, tasks.DefaultEnqueuePolicy())
	if err != nil {
		t.Fatalf("RegisterJSON before start: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Close() }()
	waitFor(t, 5*time.Second, s.IsLeader, "replica did not acquire leadership")

	// A registration while LEADING lands on the live scheduler too.
	if _, err := s.RegisterJSON("@every 2h", "test:tock", nil, tasks.DefaultEnqueuePolicy()); err != nil {
		t.Fatalf("RegisterJSON while leading: %v", err)
	}
	if err := s.Unregister(id); err != nil {
		t.Fatalf("Unregister replayed entry: %v", err)
	}
	if err := s.Unregister("no-such-entry"); err == nil {
		t.Fatal("Unregister of an unknown id must error")
	}
}

// An invalid spec is rejected at registration, not at leadership time.
func TestLeaderSchedulerValidatesEagerly(t *testing.T) {
	srv := miniredis.RunT(t)
	s := testLeaderScheduler(t, srv.Addr(), "replica-a")
	if _, err := s.RegisterJSON("", "test:tick", nil, tasks.DefaultEnqueuePolicy()); err == nil {
		t.Fatal("empty cron spec must be rejected")
	}
	if _, err := s.RegisterJSON("@every 1h", "", nil, tasks.DefaultEnqueuePolicy()); err == nil {
		t.Fatal("empty task type must be rejected")
	}
	_ = s.Close()
}
