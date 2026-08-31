package nucleus

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
	memoryprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/memory"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// NF-13: modules could neither enqueue one-off tasks nor outbox events —
// Runtime exposed neither the tasks.Manager nor the Outbox. These tests pin
// the two accessors' degrade-to-nil contract and the publish-once cell that
// makes Tasks() answer after the jobs runtime starts.

func TestRuntimeOutboxUnbackedAndDisabledAreNil(t *testing.T) {
	if (runtime{}).Outbox() != nil {
		t.Fatal("Runtime.Outbox() on an unbacked runtime should be nil, not a panic")
	}
	core := newTestApp(t) // outbox disabled by default
	rt := newRuntime(core, "")
	if rt.Outbox() != nil {
		t.Fatal("Runtime.Outbox() with a disabled outbox should be nil")
	}
}

func TestRuntimeTasksNilUntilPublished(t *testing.T) {
	if (runtime{}).Tasks() != nil {
		t.Fatal("Runtime.Tasks() on an unbacked runtime should be nil, not a panic")
	}

	core := newTestApp(t)
	ref := &taskManagerRef{}
	rt := newRuntime(core, "")
	rt.tasksRef = ref

	if rt.Tasks() != nil {
		t.Fatal("Runtime.Tasks() before the jobs runtime starts should be nil")
	}

	mgr, err := memoryprovider.NewManager(tasks.Config{}, discardLogger())
	if err != nil {
		t.Fatalf("memory manager: %v", err)
	}
	ref.set(mgr)
	if rt.Tasks() == nil {
		t.Fatal("Runtime.Tasks() after publish should return the manager")
	}
}

// A broker-backed jobs_provider (asynq) builds the jobs runtime even with
// ZERO cron entries — the opt-in for enqueue-only modules (NF-13). The
// default in-process memory provider keeps the historical zero-cost
// behaviour: no entries, nothing built.
func TestModuleJobsStartEnqueueOnlyOptIn(t *testing.T) {
	cfgDefaults := app.DefaultConfig()
	cfg := &cfgDefaults

	// Historical default (jobs_provider: memory): nothing declared, nothing built.
	j := newModuleJobs(discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	if err := j.start(ctx, &wg, cfg); err != nil {
		t.Fatalf("start (no entries, default provider): %v", err)
	}
	if j.manager != nil {
		t.Fatal("no entries + default memory provider must not build a manager")
	}
	cancel()
	wg.Wait()

	// Broker-backed provider: the runtime exists so Runtime.Tasks has
	// something to hand out, and a one-off enqueue lands in the broker.
	srv := miniredis.RunT(t)
	cfg.JobsProvider = "asynq"
	cfg.JobsRedisURL = "redis://" + srv.Addr()
	j2 := newModuleJobs(discardLogger())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	var wg2 sync.WaitGroup
	if err := j2.start(ctx2, &wg2, cfg); err != nil {
		t.Fatalf("start (no entries, asynq provider): %v", err)
	}
	if j2.manager == nil {
		t.Fatal("asynq jobs_provider with zero entries must build the manager (NF-13 enqueue-only opt-in)")
	}
	if err := j2.manager.HandleFunc("test:oneoff", func(context.Context, tasks.Task) error { return nil }); err != nil {
		t.Fatalf("HandleFunc: %v", err)
	}
	if _, err := j2.manager.EnqueueJSON("test:oneoff", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("EnqueueJSON: %v", err)
	}
	cancel2()
	wg2.Wait()
	j2.close()
}
