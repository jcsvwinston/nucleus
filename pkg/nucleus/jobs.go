// Package nucleus — jobs.go wires ModuleSpec.Jobs to the existing pkg/tasks
// runtime (ADR-010 Phase 2). Modules register named jobs through the
// JobRegistry their Jobs closure receives; the framework translates each
// registration into a pkg/tasks handler plus a scheduler entry on the
// provider selected by `jobs_provider` (memory by default, sql, or a
// provider a module registered with pkg/tasks — asynq, from
// providers/tasks-asynq), starts the worker alongside the application's
// services, and stops scheduler and worker on shutdown. No new scheduler is
// built here — scheduling and execution are entirely pkg/tasks.
package nucleus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
	memoryprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/memory"
	sqlprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/sql"
)

// ErrInvalidJobSpec is returned (wrapped, naming module and job) for any
// invalid JobRegistry.Register call. It reaches the user as a Run error:
// a module that declares a broken job fails boot instead of silently not
// running it.
var ErrInvalidJobSpec = errors.New("nucleus: invalid job registration")

// jobCronParser accepts the portable subset JobSpec.Cron promises: standard
// 5-field expressions plus descriptors (@hourly, @daily, @every 90s, …).
// Validation happens here, once, so authors get the same error on every
// provider — the memory provider alone would demand 6 fields (it is built
// cron.WithSeconds) and asynq would reject what memory accepts.
var jobCronParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// jobEntry is one accepted registration, held until start() wires it into
// the provider. running backs the Singleton skip and must be per-entry.
type jobEntry struct {
	module  string
	name    string
	spec    JobSpec
	running atomic.Bool
}

// taskType returns the pkg/tasks task-type key for this entry. Namespaced so
// module jobs can never collide with task types an application registers on
// its own pkg/tasks manager.
func (e *jobEntry) taskType() string {
	return "nucleus:module:" + e.module + ":" + e.name
}

// providerSpec renders the schedule in the dialect the selected provider
// parses: descriptors ("@every …", "@hourly") pass through everywhere;
// a 5-field expression gains a leading seconds field ("0 ") for the memory
// provider, whose cron is second-granular.
func (e *jobEntry) providerSpec(provider string) string {
	spec := e.spec.Cron
	if e.spec.Every > 0 {
		spec = "@every " + e.spec.Every.String()
	}
	if strings.HasPrefix(spec, "@") {
		return spec
	}
	if provider == jobsProviderMemory {
		return "0 " + spec
	}
	return spec
}

const (
	jobsProviderMemory = "memory"
	jobsProviderSQL    = "sql"
)

// moduleJobs collects every module's job registrations and, if there are
// any, owns the pkg/tasks manager + scheduler pair for their lifetime.
type moduleJobs struct {
	logger  *slog.Logger
	entries []*jobEntry
	names   map[string]struct{} // "module\x00name" duplicate guard

	manager   tasks.Manager
	scheduler tasks.Scheduler
	inspector tasks.Inspector

	closeOnce sync.Once
}

func newModuleJobs(logger *slog.Logger) *moduleJobs {
	return &moduleJobs{logger: logger, names: map[string]struct{}{}}
}

// scopedJobRegistry is the JobRegistry a single module's Jobs closure sees:
// it stamps the module name onto every registration.
type scopedJobRegistry struct {
	jobs   *moduleJobs
	module string
	errs   []error
}

func (r *scopedJobRegistry) Register(name string, spec JobSpec) error {
	err := r.jobs.register(r.module, name, spec)
	if err != nil {
		// Recorded as well as returned: Jobs closures have no error return,
		// so most authors cannot propagate this themselves — collect() turns
		// the record into a boot failure either way.
		r.errs = append(r.errs, err)
	}
	return err
}

// collect invokes one module's Jobs closure against a scoped registry and
// returns the first registration error, failing boot loudly.
func (j *moduleJobs) collect(spec ModuleSpec) error {
	reg := &scopedJobRegistry{jobs: j, module: spec.Name()}
	spec.Jobs(reg)
	if len(reg.errs) > 0 {
		return reg.errs[0]
	}
	return nil
}

func (j *moduleJobs) register(module, name string, spec JobSpec) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: module %q job %q: %s", ErrInvalidJobSpec, module, name, fmt.Sprintf(format, args...))
	}

	if strings.TrimSpace(name) == "" {
		return fail("name must not be empty")
	}
	if spec.Handler == nil {
		return fail("Handler is required")
	}
	if spec.Every < 0 {
		return fail("Every %v must not be negative", spec.Every)
	}
	if spec.Timeout < 0 {
		return fail("Timeout %v must not be negative", spec.Timeout)
	}
	hasEvery := spec.Every > 0
	hasCron := strings.TrimSpace(spec.Cron) != ""
	if hasEvery == hasCron {
		return fail("exactly one of Every or Cron must be set")
	}
	if hasCron {
		if _, err := jobCronParser.Parse(strings.TrimSpace(spec.Cron)); err != nil {
			return fail("invalid Cron %q: %v", spec.Cron, err)
		}
		spec.Cron = strings.TrimSpace(spec.Cron)
	}

	key := module + "\x00" + name
	if _, dup := j.names[key]; dup {
		return fail("duplicate job name within the module")
	}
	j.names[key] = struct{}{}

	j.entries = append(j.entries, &jobEntry{module: module, name: name, spec: spec})
	return nil
}

// handlerFor adapts one entry's JobSpec.Handler to a pkg/tasks HandlerFunc,
// applying the per-run Timeout and the Singleton overlap skip. Handler errors
// are logged and returned to the provider (which counts/retries per its own
// policy) — they never crash the process.
func (j *moduleJobs) handlerFor(e *jobEntry) tasks.HandlerFunc {
	return func(ctx context.Context, _ tasks.Task) error {
		if e.spec.Singleton {
			if !e.running.CompareAndSwap(false, true) {
				j.logger.Warn("nucleus: singleton job still running; skipping this tick",
					"module", e.module, "job", e.name)
				return nil
			}
			defer e.running.Store(false)
		}
		if e.spec.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, e.spec.Timeout)
			defer cancel()
		}
		start := time.Now()
		if err := e.spec.Handler(ctx); err != nil {
			j.logger.Error("nucleus: job run failed",
				"module", e.module, "job", e.name, "duration", time.Since(start), "error", err)
			return err
		}
		j.logger.Debug("nucleus: job run completed",
			"module", e.module, "job", e.name, "duration", time.Since(start))
		return nil
	}
}

// start builds the configured provider, registers every collected entry
// (handler + schedule), launches the worker on ctx via wg, and starts the
// scheduler. A no-op when no module registered jobs and the provider is the
// default in-process one — nothing is built, so the memory runtime costs
// nothing to apps without jobs. A NON-DEFAULT jobs_provider (sql, or a
// registered one such as asynq) builds the runtime even with zero cron
// entries: configuring a broker is the explicit opt-in for modules that only
// enqueue one-off tasks through Runtime.Tasks (NF-13). The memory default
// cannot make that distinction — `jobs_provider: memory` IS the framework
// default, so a zero-entry memory runtime would be built for every
// application that has modules.
//
// A provider that is not memory or sql is resolved through the pkg/tasks
// registry, where a provider module registered it at init. One this project
// publishes and the application has not linked — `jobs_provider: asynq`
// without providers/tasks-asynq — is refused naming `nucleus add
// tasks-asynq`, the way an unlinked storage backend is (DEP-2026-020).
func (j *moduleJobs) start(ctx context.Context, wg *sync.WaitGroup, cfg *app.Config, sqlDB *sql.DB) error {
	provider := strings.ToLower(strings.TrimSpace(cfg.JobsProvider))
	if len(j.entries) == 0 && (provider == "" || provider == jobsProviderMemory) {
		return nil
	}
	if provider == "" {
		provider = jobsProviderMemory
	}

	tasksCfg := tasks.Config{
		RedisURL:    cfg.JobsRedisURL,
		Concurrency: cfg.JobsConcurrency,
	}

	switch provider {
	case jobsProviderMemory:
		mgr, err := memoryprovider.NewManager(tasksCfg, j.logger)
		if err != nil {
			return fmt.Errorf("nucleus: jobs: building memory manager: %w", err)
		}
		sch, err := memoryprovider.NewScheduler(memoryprovider.SchedulerConfig{Manager: mgr})
		if err != nil {
			return fmt.Errorf("nucleus: jobs: building memory scheduler: %w", err)
		}
		j.manager, j.scheduler = mgr, sch
		j.inspector = memoryprovider.NewInspector(mgr)
	case jobsProviderSQL:
		if sqlDB == nil {
			return fmt.Errorf("nucleus: jobs: jobs_provider %q needs a SQL database; configure `databases` so the queue has a table to live in", provider)
		}
		store, err := sqlprovider.NewStore(sqlDB, sqlprovider.Config{
			TableName:   cfg.JobsTable,
			DatabaseURL: defaultDatabaseURL(cfg),
		})
		if err != nil {
			return fmt.Errorf("nucleus: jobs: preparing the sql queue: %w", err)
		}
		mgr, err := sqlprovider.NewManager(sqlprovider.ManagerConfig{
			Store:       store,
			Concurrency: cfg.JobsConcurrency,
			Queues:      cfg.JobsQueues,
			// The handlers that are still running when the process stops get
			// the same grace the rest of the lifecycle gets, rather than the
			// whole lease: waiting out a 30-second lease would make every
			// deploy that much slower, and the jobs are released afterwards
			// anyway.
			ShutdownGrace: shutdownGrace(cfg),
			Retention:     cfg.JobsRetention,
		}, j.logger)
		if err != nil {
			return fmt.Errorf("nucleus: jobs: building sql manager: %w", err)
		}
		// A cron entry needs exactly ONE replica to tick it. Here that
		// election is a lease row in the application's own database, so it
		// needs no Redis: every replica runs the schedule and only the leader
		// fires. Without it each replica would fire every entry on every
		// tick, which is the defect NF-1 fixed for asynq with a Redis lock.
		sch, err := sqlprovider.NewScheduler(sqlprovider.SchedulerConfig{
			Manager: mgr, Store: store, Logger: j.logger,
		})
		if err != nil {
			return fmt.Errorf("nucleus: jobs: building sql scheduler: %w", err)
		}
		j.manager, j.scheduler = mgr, sch
		j.inspector = sqlprovider.NewInspector(store)
	default:
		p, ok := tasks.LookupProvider(provider)
		if !ok {
			return unknownJobsProvider(cfg.JobsProvider)
		}
		rt, err := p.Open(tasks.ProviderOptions{
			Config:        tasksCfg,
			SchedulerLock: cfg.JobsSchedulerLock,
			Logger:        j.logger,
		})
		if err != nil {
			return fmt.Errorf("nucleus: jobs: opening jobs_provider %q: %w", provider, err)
		}
		if rt.Manager == nil || rt.Scheduler == nil {
			return fmt.Errorf("nucleus: jobs: jobs_provider %q opened without a manager or a scheduler", provider)
		}
		j.manager, j.scheduler, j.inspector = rt.Manager, rt.Scheduler, rt.Inspector
	}

	for _, e := range j.entries {
		if err := j.manager.HandleFunc(e.taskType(), j.handlerFor(e)); err != nil {
			return fmt.Errorf("nucleus: jobs: registering handler for %s: %w", e.taskType(), err)
		}
		policy := tasks.DefaultEnqueuePolicy()
		policy.Timeout = e.spec.Timeout
		if _, err := j.scheduler.RegisterJSON(e.providerSpec(provider), e.taskType(), nil, policy); err != nil {
			return fmt.Errorf("nucleus: jobs: scheduling %s: %w", e.taskType(), err)
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := j.manager.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			j.logger.Error("nucleus: jobs worker terminated with error", "error", err)
		}
	}()

	// Every provider builds a scheduler now — the sql one elects its leader
	// with a lease row in the application's own database — but the guard
	// stays: while the sql branch left this nil, selecting that provider took
	// the application down on boot with a nil-interface dereference, and
	// nothing caught it, because the jobs bench measures the provider package
	// directly and never walks this wiring.
	if j.scheduler != nil {
		if err := j.scheduler.Start(); err != nil {
			return fmt.Errorf("nucleus: jobs: starting scheduler: %w", err)
		}
	}

	j.logger.Info("nucleus: module jobs scheduled",
		"provider", provider, "jobs", len(j.entries))
	return nil
}

// unknownJobsProvider is the refusal for a jobs_provider nothing provides. A
// provider THIS project publishes as its own module gets the recipe instead
// of a list: the configuration is not wrong — the operator wrote "asynq"
// because the documentation says so — the module simply is not in the build
// yet, and that is a one-line fix.
func unknownJobsProvider(name string) error {
	registered := append([]string{jobsProviderMemory, jobsProviderSQL}, tasks.RegisteredProviders()...)
	if p, ours := knownproviders.JobsProvider(name); ours {
		return fmt.Errorf("nucleus: jobs: jobs_provider %q ships as its own module and is not imported yet.\n\n"+
			"\tAdd it to your build:\n\n%s\n\n"+
			"\tOr let the CLI do it:\n\n\t\tnucleus add %s\n\n"+
			"\t(registered right now: %s)",
			name, p.InstallHint(), p.Name, strings.Join(registered, ", "))
	}
	return fmt.Errorf("nucleus: jobs: unknown jobs_provider %q (registered: %s) — a third-party provider registers with tasks.RegisterProvider",
		name, strings.Join(registered, ", "))
}

// close stops the scheduler first — no new ticks are enqueued — and then the
// manager, which waits for the handlers still running and gives back what it
// holds.
//
// It runs once, whoever calls it first: Run registers it as a shutdown hook so
// it happens BEFORE the database closes, and calls it again on the way out,
// after the ctx-driven worker exit. Not every provider's Close survives a
// second call (the asynq leader scheduler closes a channel), so the guard is
// here rather than trusted to each of them.
func (j *moduleJobs) close() {
	j.closeOnce.Do(func() {
		if j.scheduler != nil {
			if err := j.scheduler.Close(); err != nil {
				j.logger.Warn("nucleus: jobs scheduler close", "error", err)
			}
		}
		if j.manager != nil {
			if err := j.manager.Close(); err != nil {
				j.logger.Warn("nucleus: jobs manager close", "error", err)
			}
		}
	})
}

// defaultDatabaseURL is how the jobs store learns which dialect it is talking
// to: the URL of the application's default database. The store only reads it
// to pick a dialect — the connection itself is the pool the framework already
// manages.
func defaultDatabaseURL(cfg *app.Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.DefaultDatabase().URL
}

// shutdownGrace is how long the jobs runtime waits for handlers that are still
// running when the process stops. It mirrors the lifecycle's own timeout, so
// an application that tunes one tunes both.
func shutdownGrace(cfg *app.Config) time.Duration {
	const fallback = 10 * time.Second
	if cfg == nil || cfg.WriteTimeout <= 0 {
		return fallback
	}
	return cfg.WriteTimeout
}
