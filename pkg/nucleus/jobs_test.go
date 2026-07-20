package nucleus

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func newTestModuleJobs() (*moduleJobs, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return newModuleJobs(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))), buf
}

func noopJob(context.Context) error { return nil }

// TestJobRegister_Validation drives every rejection path of
// JobRegistry.Register: each one must wrap ErrInvalidJobSpec (these errors
// fail application boot) and name the offending condition.
func TestJobRegister_Validation(t *testing.T) {
	cases := []struct {
		label string
		name  string
		spec  JobSpec
		want  string
	}{
		{"empty name", "", JobSpec{Handler: noopJob, Every: time.Second}, "name must not be empty"},
		{"nil handler", "j", JobSpec{Every: time.Second}, "Handler is required"},
		{"no schedule", "j", JobSpec{Handler: noopJob}, "exactly one of Every or Cron"},
		{"both schedules", "j", JobSpec{Handler: noopJob, Every: time.Second, Cron: "@hourly"}, "exactly one of Every or Cron"},
		{"negative every", "j", JobSpec{Handler: noopJob, Every: -time.Second}, "must not be negative"},
		{"negative timeout", "j", JobSpec{Handler: noopJob, Every: time.Second, Timeout: -1}, "must not be negative"},
		{"six-field cron", "j", JobSpec{Handler: noopJob, Cron: "0 */5 * * * *"}, "invalid Cron"},
		{"garbage cron", "j", JobSpec{Handler: noopJob, Cron: "not-a-spec"}, "invalid Cron"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			j, _ := newTestModuleJobs()
			err := j.register("billing", tc.name, tc.spec)
			if !errors.Is(err, ErrInvalidJobSpec) {
				t.Fatalf("want ErrInvalidJobSpec, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestJobRegister_DuplicateWithinModule rejects a second job with the same
// name in the same module, while the same name in another module is fine.
func TestJobRegister_DuplicateWithinModule(t *testing.T) {
	j, _ := newTestModuleJobs()
	spec := JobSpec{Handler: noopJob, Every: time.Second}
	if err := j.register("billing", "sweep", spec); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if err := j.register("billing", "sweep", spec); !errors.Is(err, ErrInvalidJobSpec) {
		t.Fatalf("duplicate in same module: want ErrInvalidJobSpec, got %v", err)
	}
	if err := j.register("reports", "sweep", spec); err != nil {
		t.Fatalf("same name in another module must be accepted, got %v", err)
	}
}

// TestJobRegister_AcceptsPortableSpecs locks in the accepted schedule
// dialects: interval, 5-field cron, and descriptors.
func TestJobRegister_AcceptsPortableSpecs(t *testing.T) {
	j, _ := newTestModuleJobs()
	for i, spec := range []JobSpec{
		{Handler: noopJob, Every: 30 * time.Second},
		{Handler: noopJob, Cron: "*/5 * * * *"},
		{Handler: noopJob, Cron: "@hourly"},
		{Handler: noopJob, Cron: "@every 90s"},
	} {
		if err := j.register("m", string(rune('a'+i)), spec); err != nil {
			t.Fatalf("spec %d must be accepted: %v", i, err)
		}
	}
}

// TestJobEntry_ProviderSpec covers the per-provider schedule translation:
// descriptors pass through everywhere; a 5-field expression gains the leading
// seconds field only for the memory provider (its cron is second-granular).
func TestJobEntry_ProviderSpec(t *testing.T) {
	cases := []struct {
		spec       JobSpec
		wantMemory string
		wantAsynq  string
	}{
		{JobSpec{Every: 90 * time.Second}, "@every 1m30s", "@every 1m30s"},
		{JobSpec{Cron: "*/5 * * * *"}, "0 */5 * * * *", "*/5 * * * *"},
		{JobSpec{Cron: "@hourly"}, "@hourly", "@hourly"},
	}
	for _, tc := range cases {
		e := &jobEntry{spec: tc.spec}
		if got := e.providerSpec(jobsProviderMemory); got != tc.wantMemory {
			t.Errorf("memory spec for %+v: got %q, want %q", tc.spec, got, tc.wantMemory)
		}
		if got := e.providerSpec(jobsProviderAsynq); got != tc.wantAsynq {
			t.Errorf("asynq spec for %+v: got %q, want %q", tc.spec, got, tc.wantAsynq)
		}
	}
}

// TestJobsCollect_RegistrationErrorFailsCollect proves the boot-failure
// contract: a module whose Jobs closure registers a broken job surfaces the
// error from collect even though the closure itself cannot propagate it.
func TestJobsCollect_RegistrationErrorFailsCollect(t *testing.T) {
	j, _ := newTestModuleJobs()
	spec := Module[struct{}]{
		Name: "broken",
		Jobs: func(r JobRegistry, _ struct{}) {
			_ = r.Register("bad", JobSpec{Handler: noopJob}) // no schedule
		},
	}.Build()

	if err := j.collect(spec); !errors.Is(err, ErrInvalidJobSpec) {
		t.Fatalf("collect must surface the registration error, got %v", err)
	}
}

// TestJobHandler_SingletonSkipsOverlap drives the Singleton guard directly:
// while one run is in flight, a second tick returns immediately without
// invoking the handler and logs the skip.
func TestJobHandler_SingletonSkipsOverlap(t *testing.T) {
	j, buf := newTestModuleJobs()

	release := make(chan struct{})
	entered := make(chan struct{})
	var enteredOnce sync.Once
	var runs atomic.Int64
	e := &jobEntry{module: "m", name: "slow", spec: JobSpec{
		Singleton: true,
		Handler: func(context.Context) error {
			if runs.Add(1) == 1 {
				enteredOnce.Do(func() { close(entered) })
				<-release
			}
			return nil
		},
	}}
	h := j.handlerFor(e)

	done := make(chan error, 1)
	go func() { done <- h(context.Background(), nil) }()
	<-entered

	// Second tick while the first run is still executing.
	if err := h(context.Background(), nil); err != nil {
		t.Fatalf("skipped tick must return nil, got %v", err)
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("handler must not run concurrently, got %d runs", got)
	}
	if !strings.Contains(buf.String(), "singleton job still running") {
		t.Fatalf("expected the skip WARN, got %q", buf.String())
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first run: %v", err)
	}

	// With the first run finished, the next tick executes again.
	if err := h(context.Background(), nil); err != nil {
		t.Fatalf("post-completion tick: %v", err)
	}
	if got := runs.Load(); got != 2 {
		t.Fatalf("want 2 runs after the singleton released, got %d", got)
	}
}

// TestJobHandler_TimeoutBoundsRun proves JobSpec.Timeout imposes a real
// context deadline on the handler.
func TestJobHandler_TimeoutBoundsRun(t *testing.T) {
	j, buf := newTestModuleJobs()
	e := &jobEntry{module: "m", name: "hang", spec: JobSpec{
		Timeout: 50 * time.Millisecond,
		Handler: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}}

	start := time.Now()
	err := j.handlerFor(e)(context.Background(), nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout did not bound the run: took %v", elapsed)
	}
	if !strings.Contains(buf.String(), "job run failed") {
		t.Fatalf("expected the failure log, got %q", buf.String())
	}
}

// TestJobsStart_NoEntriesIsNoOp: an application without module jobs builds no
// provider at all.
func TestJobsStart_NoEntriesIsNoOp(t *testing.T) {
	j, _ := newTestModuleJobs()
	cfg := app.DefaultConfig()
	var wg sync.WaitGroup
	if err := j.start(context.Background(), &wg, &cfg); err != nil {
		t.Fatalf("start with no entries: %v", err)
	}
	if j.manager != nil || j.scheduler != nil {
		t.Fatal("no provider must be built when no jobs are registered")
	}
}

// TestJobsStart_UnknownProvider rejects a jobs_provider outside the enum —
// the local guard behind validateSemantics, for programmatic callers.
func TestJobsStart_UnknownProvider(t *testing.T) {
	j, _ := newTestModuleJobs()
	if err := j.register("m", "j", JobSpec{Handler: noopJob, Every: time.Second}); err != nil {
		t.Fatal(err)
	}
	cfg := app.DefaultConfig()
	cfg.JobsProvider = "rabbitmq"
	var wg sync.WaitGroup
	defer j.close()
	if err := j.start(context.Background(), &wg, &cfg); err == nil || !strings.Contains(err.Error(), "unknown jobs_provider") {
		t.Fatalf("want unknown-provider error, got %v", err)
	}
}

// TestJobsStart_AsynqRequiresRedisURL mirrors the validateSemantics rule at
// the runtime layer.
func TestJobsStart_AsynqRequiresRedisURL(t *testing.T) {
	j, _ := newTestModuleJobs()
	if err := j.register("m", "j", JobSpec{Handler: noopJob, Every: time.Second}); err != nil {
		t.Fatal(err)
	}
	cfg := app.DefaultConfig()
	cfg.JobsProvider = "asynq"
	cfg.JobsRedisURL = ""
	var wg sync.WaitGroup
	defer j.close()
	if err := j.start(context.Background(), &wg, &cfg); err == nil || !strings.Contains(err.Error(), "jobs_redis_url") {
		t.Fatalf("want missing-redis-url error, got %v", err)
	}
}

// waitForRuns polls counter until it reaches want or the deadline passes.
func waitForRuns(t *testing.T, counter *atomic.Int64, want int64, deadline time.Duration) {
	t.Helper()
	stop := time.Now().Add(deadline)
	for time.Now().Before(stop) {
		if counter.Load() >= want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("job ran %d times, want at least %d within %v", counter.Load(), want, deadline)
}

// TestJobsStart_MemoryProviderExecutes is the real-execution proof for the
// default provider: a registered interval job is scheduled by pkg/tasks'
// memory scheduler and executed by its worker — no stubs anywhere in the
// path. The 1s interval is the memory scheduler's floor (its cron rounds
// sub-second delays up), hence the generous deadline.
func TestJobsStart_MemoryProviderExecutes(t *testing.T) {
	j, _ := newTestModuleJobs()
	var runs atomic.Int64
	err := j.register("billing", "tick", JobSpec{
		Every:   time.Second,
		Handler: func(context.Context) error { runs.Add(1); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := app.DefaultConfig()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	if err := j.start(ctx, &wg, &cfg); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		cancel()
		wg.Wait()
		j.close()
	}()

	waitForRuns(t, &runs, 2, 10*time.Second)
}

// TestJobsStart_AsynqProviderExecutes proves the asynq path end to end
// against miniredis: scheduler entry → Redis queue → worker → handler. The
// same wiring runs against a real Redis in the jobs-redis CI lane (see
// TestJobsStart_AsynqAgainstRealRedis).
func TestJobsStart_AsynqProviderExecutes(t *testing.T) {
	mr := miniredis.RunT(t)
	runAsynqExecutionScenario(t, "redis://"+mr.Addr())
}

// TestJobsStart_AsynqAgainstRealRedis is the Redis-real lane for the asynq
// provider: it runs only when NUCLEUS_JOBS_REDIS_URL is set (the jobs-redis
// CI job provides a redis:7 service container) and executes the identical
// scenario as the miniredis test, against a real server.
func TestJobsStart_AsynqAgainstRealRedis(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("NUCLEUS_JOBS_REDIS_URL"))
	if url == "" {
		t.Skip("NUCLEUS_JOBS_REDIS_URL not set; real-Redis jobs lane only")
	}
	runAsynqExecutionScenario(t, url)
}

func runAsynqExecutionScenario(t *testing.T, redisURL string) {
	t.Helper()
	j, _ := newTestModuleJobs()
	var runs atomic.Int64
	err := j.register("billing", "tick", JobSpec{
		Every:   time.Second,
		Handler: func(context.Context) error { runs.Add(1); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := app.DefaultConfig()
	cfg.JobsProvider = "asynq"
	cfg.JobsRedisURL = redisURL
	cfg.JobsConcurrency = 2

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	if err := j.start(ctx, &wg, &cfg); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		cancel()
		wg.Wait()
		j.close()
	}()

	// Scheduler tick (1s) + asynq's queue poll interval: allow a wide margin.
	waitForRuns(t, &runs, 2, 30*time.Second)
}
