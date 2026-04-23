package tasks

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestPeriodicTaskValidate(t *testing.T) {
	tests := []PeriodicTask{
		{},
		{Spec: "@every 1s"},
		{Spec: "@every 1s", TaskType: "cron.cleanup", Policy: EnqueuePolicy{MaxRetry: -2}},
	}

	for _, tc := range tests {
		if err := tc.Validate(); err == nil {
			t.Fatalf("expected validation error for %#v", tc)
		}
	}
}

func TestNewSchedulerRequiresRedisURL(t *testing.T) {
	if _, err := NewScheduler(SchedulerConfig{}); err == nil {
		t.Fatal("expected redis url validation error")
	}
}

func TestSchedulerRegisterAndInspectRuntime(t *testing.T) {
	redisServer := miniredis.RunT(t)

	scheduler, err := NewScheduler(SchedulerConfig{
		RedisURL:          "redis://" + redisServer.Addr(),
		HeartbeatInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}
	defer scheduler.Close()

	policy := DefaultEnqueuePolicy()
	policy.Queue = "cron"
	policy.MaxRetry = 1

	entryID, err := scheduler.RegisterJSON("@every 1s", "cleanup.sessions", map[string]any{
		"scope": "expired",
	}, policy)
	if err != nil {
		t.Fatalf("RegisterJSON failed: %v", err)
	}
	if entryID == "" {
		t.Fatal("expected non-empty entry id")
	}

	if err := scheduler.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	inspector, err := newInspectorForTest("redis://" + redisServer.Addr())
	if err != nil {
		t.Fatalf("newInspectorForTest failed: %v", err)
	}
	defer inspector.Close()

	var entriesFound bool
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := inspector.SchedulerEntries()
		if err == nil && len(entries) > 0 {
			for _, entry := range entries {
				if entry != nil && entry.ID == entryID {
					if entry.Spec != "@every 1s" {
						t.Fatalf("spec = %q, want @every 1s", entry.Spec)
					}
					if entry.Task == nil || entry.Task.Type() != "cleanup.sessions" {
						t.Fatalf("unexpected task in scheduler entry: %#v", entry.Task)
					}
					entriesFound = true
					break
				}
			}
		}
		if entriesFound {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !entriesFound {
		t.Fatal("expected scheduler entry to be discoverable from runtime inspector")
	}

	time.Sleep(1100 * time.Millisecond)
	events, err := inspector.ListSchedulerEnqueueEvents(entryID)
	if err != nil {
		t.Fatalf("ListSchedulerEnqueueEvents failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one enqueue event from scheduler")
	}

	snapshot := InspectRuntime("redis://" + redisServer.Addr())
	if snapshot.TotalSchedules == 0 {
		t.Fatalf("expected runtime snapshot to include schedules, got %#v", snapshot)
	}

	if err := scheduler.Unregister(entryID); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}
}

func TestSchedulerNilReceiver(t *testing.T) {
	var scheduler *Scheduler
	if err := scheduler.Start(); err != ErrNilScheduler {
		t.Fatalf("expected ErrNilScheduler from Start, got %v", err)
	}
	if _, err := scheduler.RegisterJSON("@every 1s", "cleanup.sessions", map[string]any{}, DefaultEnqueuePolicy()); err != ErrNilScheduler {
		t.Fatalf("expected ErrNilScheduler from RegisterJSON, got %v", err)
	}
}
