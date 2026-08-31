package nucleus

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/jcsvwinston/nucleus/pkg/app"
	asynqprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/asynq"
)

// NF-1 wiring: with jobs_provider asynq the scheduler runs under leader
// election by default, and opting out (jobs_scheduler_lock: false) leaves a
// WARN in the boot log about per-replica duplication.
func TestModuleJobsAsynqSchedulerLockWiring(t *testing.T) {
	srv := miniredis.RunT(t)

	run := func(lock bool) (*moduleJobs, *bytes.Buffer) {
		t.Helper()
		var buf bytes.Buffer
		j := newModuleJobs(slog.New(slog.NewTextHandler(&buf, nil)))
		if err := j.register("m", "tick", JobSpec{Every: 3600e9, Handler: func(context.Context) error { return nil }}); err != nil {
			t.Fatalf("register: %v", err)
		}
		cfgDefaults := app.DefaultConfig()
		cfg := &cfgDefaults
		cfg.JobsProvider = "asynq"
		cfg.JobsRedisURL = "redis://" + srv.Addr()
		cfg.JobsSchedulerLock = lock
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		var wg sync.WaitGroup
		if err := j.start(ctx, &wg, cfg); err != nil {
			t.Fatalf("start(lock=%v): %v", lock, err)
		}
		t.Cleanup(func() { cancel(); wg.Wait(); j.close() })
		return j, &buf
	}

	j, logs := run(true)
	if _, ok := j.scheduler.(*asynqprovider.LeaderScheduler); !ok {
		t.Fatalf("with jobs_scheduler_lock the scheduler must be a LeaderScheduler, got %T", j.scheduler)
	}
	if !strings.Contains(logs.String(), "leader election") {
		t.Fatalf("lock-enabled boot log does not mention leader election:\n%s", logs.String())
	}

	j2, logs2 := run(false)
	if _, ok := j2.scheduler.(*asynqprovider.LeaderScheduler); ok {
		t.Fatal("with jobs_scheduler_lock disabled the plain scheduler must be used")
	}
	if !strings.Contains(logs2.String(), "once per replica") {
		t.Fatalf("opt-out boot log does not WARN about per-replica duplication:\n%s", logs2.String())
	}
}
