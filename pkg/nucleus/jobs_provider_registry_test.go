// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"context"
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

// fakeJobsProvider is a tasks.Provider registered the way a provider module
// registers itself (providers/tasks-asynq does it from init). It records
// what the framework handed it and answers with the in-process provider, so
// these tests pin the registry WIRING of pkg/nucleus/jobs.go — the asynq
// provider's own behaviour is tested in its module, and asynq is the one
// provider this project publishes that must NOT be reachable from here.
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

// registerFakeJobsProvider registers a fake under name. The registry is
// process-wide and has no unregister (a provider module registers once, at
// init), so each test uses a name of its own.
func registerFakeJobsProvider(t *testing.T, name string) *fakeJobsProvider {
	t.Helper()
	f := &fakeJobsProvider{}
	if err := tasks.RegisterProvider(name, f); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
	return f
}

// A jobs_provider that is neither memory nor sql is resolved through the
// pkg/tasks registry: the provider is opened once with the jobs_* keys and
// the application's logger, every collected entry is registered on the
// manager and scheduler it returned, and the jobs run.
func TestJobsStart_RegisteredProviderIsOpenedWithTheJobsKeys(t *testing.T) {
	fake := registerFakeJobsProvider(t, "fakebroker-wiring")

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
	cfg.JobsProvider = "fakebroker-wiring"
	cfg.JobsRedisURL = "redis://broker.example:6379/3"
	cfg.JobsConcurrency = 3
	cfg.JobsSchedulerLock = false

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
	if got.Config.RedisURL != cfg.JobsRedisURL || got.Config.Concurrency != 3 || got.SchedulerLock {
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

// `jobs_provider: asynq` without providers/tasks-asynq linked: the
// configuration is the one the documentation shows, so the refusal says
// which module is missing and the command that installs it — not "unknown
// provider" (DEP-2026-020). The root module's tests are the one place this
// is guaranteed to be observable: nothing here links the module.
func TestJobsStart_PublishedProviderNotLinkedNamesNucleusAdd(t *testing.T) {
	if _, linked := tasks.LookupProvider("asynq"); linked {
		t.Fatal("the asynq provider is registered in the framework's own test binary: providers/tasks-asynq has leaked back into the root module")
	}
	j, _ := newTestModuleJobs()
	if err := j.register("m", "j", JobSpec{Handler: noopJob, Every: time.Second}); err != nil {
		t.Fatal(err)
	}
	cfg := app.DefaultConfig()
	cfg.JobsProvider = "asynq"
	cfg.JobsRedisURL = "redis://127.0.0.1:6379/0"
	var wg sync.WaitGroup
	defer j.close()
	err := j.start(context.Background(), &wg, &cfg, nil)
	if err == nil {
		t.Fatal("jobs_provider asynq without the module must refuse to start")
	}
	for _, want := range []string{
		"nucleus add tasks-asynq",
		`import _ "github.com/jcsvwinston/nucleus/providers/tasks-asynq"`,
		"registered right now: memory, sql",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "unknown jobs_provider") {
		t.Errorf("a provider this project publishes must not be reported as unknown:\n%v", err)
	}
}

// The unknown-provider refusal lists what IS available: the two built-in
// providers and whatever a module registered.
func TestJobsStart_UnknownProviderListsTheRegisteredOnes(t *testing.T) {
	registerFakeJobsProvider(t, "fakebroker-listed")
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
	for _, want := range []string{"memory", "sql", "fakebroker-listed", "tasks.RegisterProvider"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q:\n%v", want, err)
		}
	}
}

// A provider that opens without a manager or a scheduler is a bug in the
// provider, reported as such rather than dereferenced later.
func TestJobsStart_ProviderWithoutRuntimeIsRefused(t *testing.T) {
	if err := tasks.RegisterProvider("fakebroker-empty", emptyJobsProvider{}); err != nil {
		t.Fatal(err)
	}
	j := newModuleJobs(slog.Default())
	if err := j.register("m", "j", JobSpec{Handler: noopJob, Every: time.Second}); err != nil {
		t.Fatal(err)
	}
	cfg := app.DefaultConfig()
	cfg.JobsProvider = "fakebroker-empty"
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
