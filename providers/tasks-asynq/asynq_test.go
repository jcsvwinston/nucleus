// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package asynqprovider_test

import (
	"errors"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
	asynq "github.com/jcsvwinston/nucleus/pkg/tasks/providers/asynq"
	asynqprovider "github.com/jcsvwinston/nucleus/providers/tasks-asynq"
)

// The types are aliases, not new types: a *Manager from either import path
// is the same type, so code that moves to this path at 2.0 keeps passing
// its values to code that has not moved yet. The assignments compile or the
// package does not.
var (
	_ *asynqprovider.Manager                = (*asynq.Manager)(nil)
	_ *asynqprovider.Scheduler              = (*asynq.Scheduler)(nil)
	_ *asynqprovider.LeaderScheduler        = (*asynq.LeaderScheduler)(nil)
	_ *asynqprovider.Inspector              = (*asynq.Inspector)(nil)
	_ asynqprovider.SchedulerConfig         = asynq.SchedulerConfig{}
	_ asynqprovider.LeaderSchedulerConfig   = asynq.LeaderSchedulerConfig{}
	_ asynqprovider.PeriodicTask            = asynq.PeriodicTask{}
	_ tasks.Inspector                       = (*asynqprovider.Inspector)(nil)
	_ func(string) *asynqprovider.Inspector = asynqprovider.NewInspector
)

// Importing this package registers the provider: the in-tree package's init
// runs, and `jobs_provider: asynq` resolves. Nothing is registered twice —
// the framework's own blank import and this one name the same package.
func TestImportRegistersTheProvider(t *testing.T) {
	if _, ok := tasks.LookupProvider(asynqprovider.Name); !ok {
		t.Fatalf("tasks.LookupProvider(%q) found nothing; registered: %v", asynqprovider.Name, tasks.RegisteredProviders())
	}
	if asynqprovider.Name != "asynq" {
		t.Fatalf("Name = %q, want asynq", asynqprovider.Name)
	}
}

// The constants and the errors are the in-tree values, not copies: errors.Is
// holds across the two paths, which is what lets a caller that checks the
// old sentinel keep working against a manager built through this one.
func TestForwardedValuesAreTheSame(t *testing.T) {
	if asynqprovider.DefaultSchedulerLockKey != asynq.DefaultSchedulerLockKey || asynqprovider.DefaultSchedulerLockTTL != asynq.DefaultSchedulerLockTTL {
		t.Fatal("the scheduler lock defaults differ from the in-tree package")
	}
	pairs := map[string][2]error{
		"ErrNilScheduler":     {asynqprovider.ErrNilScheduler, asynq.ErrNilScheduler},
		"ErrCronSpecRequired": {asynqprovider.ErrCronSpecRequired, asynq.ErrCronSpecRequired},
		"ErrRedisURLRequired": {asynqprovider.ErrRedisURLRequired, asynq.ErrRedisURLRequired},
		"ErrTaskTypeRequired": {asynqprovider.ErrTaskTypeRequired, asynq.ErrTaskTypeRequired},
		"ErrNilHandler":       {asynqprovider.ErrNilHandler, asynq.ErrNilHandler},
		"ErrNilManager":       {asynqprovider.ErrNilManager, asynq.ErrNilManager},
		"ErrNilTask":          {asynqprovider.ErrNilTask, asynq.ErrNilTask},
	}
	for name, p := range pairs {
		if p[0] == nil || !errors.Is(p[0], p[1]) || !errors.Is(p[1], p[0]) {
			t.Errorf("%s: the forwarded error is not the in-tree one", name)
		}
	}
}

// A wrapper behaves as the original, without a Redis: NewManager and
// NewScheduler with no RedisURL refuse with ErrRedisURLRequired, the same
// sentinel either path exposes; NewLeaderScheduler refuses too (it parses
// the lock's Redis URL first, so its error is the parser's, not the
// sentinel); a PeriodicTask without a spec fails its own validation.
func TestWrappersBehaveAsTheInTreeConstructors(t *testing.T) {
	if m, err := asynqprovider.NewManager(tasks.Config{}, nil); m != nil || !errors.Is(err, asynqprovider.ErrRedisURLRequired) {
		t.Fatalf("NewManager(empty) = %v, %v; want nil, ErrRedisURLRequired", m, err)
	}
	if s, err := asynqprovider.NewScheduler(asynqprovider.SchedulerConfig{}); s != nil || !errors.Is(err, asynq.ErrRedisURLRequired) {
		t.Fatalf("NewScheduler(empty) = %v, %v; want nil, ErrRedisURLRequired", s, err)
	}
	if s, err := asynqprovider.NewLeaderScheduler(asynqprovider.LeaderSchedulerConfig{}); s != nil || err == nil {
		t.Fatalf("NewLeaderScheduler(empty) = %v, %v; want nil and an error", s, err)
	}
	if err := (asynqprovider.PeriodicTask{TaskType: "x"}).Validate(); !errors.Is(err, asynqprovider.ErrCronSpecRequired) {
		t.Fatalf("PeriodicTask{}.Validate() = %v; want ErrCronSpecRequired", err)
	}
	if asynqprovider.NewInspector("redis://127.0.0.1:1") == nil {
		t.Fatal("NewInspector returned nil")
	}
}

// The inspection helpers answer without a server, the way the in-tree ones
// do: a snapshot that says why, and an error for a queue action.
func TestInspectionWithoutARedisDoesNotPanic(t *testing.T) {
	snap := asynqprovider.InspectRuntime("")
	if snap.Enabled {
		t.Fatalf("InspectRuntime(\"\") reports Enabled: %+v", snap)
	}
	if _, err := asynqprovider.OperateQueue("", "default", "pause"); err == nil {
		t.Fatal("OperateQueue with no Redis URL returned no error")
	}
}
