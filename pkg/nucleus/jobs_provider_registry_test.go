// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
	memoryprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/memory"
)

// fakeJobsProvider is a tasks.Provider registered the way a provider package
// registers itself (pkg/tasks/providers/asynq does it from init). It records
// what the framework handed it and answers with the in-process provider, so
// these tests pin the registry WIRING of pkg/nucleus/jobs.go — the asynq
// provider's own behaviour is tested in its package, and its selection
// through this same wiring by jobs_leader_test.go and jobs_test.go.
type fakeJobsProvider struct {
	mu     sync.Mutex
	opened []tasks.ProviderOptions
}

func (f *fakeJobsProvider) Open(opts tasks.ProviderOptions) (tasks.ProviderRuntime, error) {
	f.mu.Lock()
	f.opened = append(f.opened, opts)
	f.mu.Unlock()
	mgr, err := memoryprovider.NewManager(opts.Config, opts.Logger)
	if err != nil {
		return tasks.ProviderRuntime{}, err
	}
	sch, err := memoryprovider.NewScheduler(memoryprovider.SchedulerConfig{Manager: mgr})
	if err != nil {
		return tasks.ProviderRuntime{}, err
	}
	return tasks.ProviderRuntime{Manager: mgr, Scheduler: sch, Inspector: memoryprovider.NewInspector(mgr)}, nil
}

func (f *fakeJobsProvider) Inspect(tasks.ProviderOptions) tasks.RuntimeSnapshot {
	return tasks.RuntimeSnapshot{Enabled: true}
}

// fakeProviderSeq numbers the fakes the tests register.
var fakeProviderSeq atomic.Int64

// uniqueProviderName returns a name no other registration in this process
// used: the pkg/tasks registry is process-wide and has no unregister (a
// provider registers once, at init), so a fixed name would refuse the
// second registration under -count=2.
func uniqueProviderName(base string) string {
	return fmt.Sprintf("%s-%d", base, fakeProviderSeq.Add(1))
}

// registerFakeJobsProvider registers a fake under a name of its own and
// returns both.
func registerFakeJobsProvider(t *testing.T, base string) (*fakeJobsProvider, string) {
	t.Helper()
	name := uniqueProviderName(base)
	f := &fakeJobsProvider{}
	if err := tasks.RegisterProvider(name, f); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
	return f, name
}

// A jobs_provider that is neither memory nor sql is resolved through the
// pkg/tasks registry: the provider is opened once with the jobs_* keys and
// the application's logger, every collected entry is registered on the
// manager and scheduler it returned, and the jobs run.
func TestJobsStart_RegisteredProviderIsOpenedWithTheJobsKeys(t *testing.T) {
	fake, name := registerFakeJobsProvider(t, "fakebroker-wiring")

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
	cfg.JobsProvider = name
	cfg.JobsRedisURL = "redis://broker.example:6379/3"
	cfg.JobsConcurrency = 3
	// Not the zero value: the pin is that the framework forwards what the
	// configuration says, which jobs_leader_test.go pins through the asynq
	// provider's leader election.
	cfg.JobsSchedulerLock = true

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	if err := j.start(ctx, &wg, &cfg, nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		cancel()
		wg.Wait()
		j.close()
	}()

	fake.mu.Lock()
	opened := append([]tasks.ProviderOptions(nil), fake.opened...)
	fake.mu.Unlock()
	if len(opened) != 1 {
		t.Fatalf("the provider was opened %d times, want once", len(opened))
	}
	got := opened[0]
	if got.Config.RedisURL != cfg.JobsRedisURL || got.Config.Concurrency != 3 || !got.SchedulerLock {
		t.Fatalf("the provider did not receive the jobs_* keys: %+v", got)
	}
	if got.Logger != j.logger {
		t.Fatal("the provider did not receive the application's logger")
	}
	if j.inspector == nil {
		t.Fatal("the inspector the provider returned was not kept")
	}
	waitForRuns(t, &runs, 2, 10*time.Second)
}

// `jobs_provider: asynq` resolves through the registry, not through a case
// of its own: the framework links pkg/tasks/providers/asynq and that package
// registers itself at init. This is the line DEP-2026-020 moves to the
// application at v2.0.0; until then the framework's own test binary must see
// the provider registered without importing it here.
func TestJobsStart_AsynqIsRegisteredByTheFramework(t *testing.T) {
	if _, ok := tasks.LookupProvider("asynq"); !ok {
		t.Fatal("jobs_provider asynq is not registered: pkg/nucleus no longer links pkg/tasks/providers/asynq")
	}
	for _, name := range tasks.RegisteredProviders() {
		if name == "asynq" {
			return
		}
	}
	t.Fatalf("RegisteredProviders() = %v does not list asynq", tasks.RegisteredProviders())
}

// The unknown-provider refusal lists what IS available: the two built-in
// providers and whatever a package registered.
func TestJobsStart_UnknownProviderListsTheRegisteredOnes(t *testing.T) {
	_, listed := registerFakeJobsProvider(t, "fakebroker-listed")
	j, _ := newTestModuleJobs()
	if err := j.register("m", "j", JobSpec{Handler: noopJob, Every: time.Second}); err != nil {
		t.Fatal(err)
	}
	cfg := app.DefaultConfig()
	cfg.JobsProvider = "rabbitmq"
	var wg sync.WaitGroup
	defer j.close()
	err := j.start(context.Background(), &wg, &cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown jobs_provider") {
		t.Fatalf("want unknown-provider error, got %v", err)
	}
	for _, want := range []string{"memory", "sql", "asynq", listed, "tasks.RegisterProvider"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q:\n%v", want, err)
		}
	}
}

// A provider that opens without a manager or a scheduler is a bug in the
// provider, reported as such rather than dereferenced later.
func TestJobsStart_ProviderWithoutRuntimeIsRefused(t *testing.T) {
	empty := uniqueProviderName("fakebroker-empty")
	if err := tasks.RegisterProvider(empty, emptyJobsProvider{}); err != nil {
		t.Fatal(err)
	}
	j := newModuleJobs(slog.Default())
	if err := j.register("m", "j", JobSpec{Handler: noopJob, Every: time.Second}); err != nil {
		t.Fatal(err)
	}
	cfg := app.DefaultConfig()
	cfg.JobsProvider = empty
	var wg sync.WaitGroup
	defer j.close()
	err := j.start(context.Background(), &wg, &cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "without a manager or a scheduler") {
		t.Fatalf("want the empty-runtime refusal, got %v", err)
	}
}

type emptyJobsProvider struct{}

func (emptyJobsProvider) Open(tasks.ProviderOptions) (tasks.ProviderRuntime, error) {
	return tasks.ProviderRuntime{}, nil
}

func (emptyJobsProvider) Inspect(tasks.ProviderOptions) tasks.RuntimeSnapshot {
	return tasks.RuntimeSnapshot{}
}

// A jobs provider this project publishes as a module is refused with the
// line that installs it, the way pkg/cache and pkg/storage answer. Until
// v2.0.0 the framework links asynq itself, so the runtime never reaches this
// branch for it; the helper is called directly so the wording is pinned
// before the major flips it on (DEP-2026-020).
func TestUnknownJobsProvider_NamesTheModuleForAsynq(t *testing.T) {
	err := unknownJobsProvider("asynq")
	for _, want := range []string{"ships with this framework", "providers/tasks-asynq", "nucleus add tasks-asynq"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
	if err := unknownJobsProvider("rabbitmq"); strings.Contains(err.Error(), "nucleus add") {
		t.Errorf("a third-party name must not get an install hint:\n%v", err)
	}
}
