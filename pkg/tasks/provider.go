// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"
)

// Provider is the contract a jobs provider module implements to be selected
// with `jobs_provider`. The memory and sql providers are part of the
// framework and need no registration; a provider that brings a client
// library with it — the asynq one, in providers/tasks-asynq — ships as its
// own module and registers itself from its init(), the way a database
// driver module registers with pkg/db/driver (ADR-030/031). The framework
// then resolves `jobs_provider: <name>` through LookupProvider, and an
// application that names a provider nothing registered fails at boot with
// the module that installs it.
//
// A Provider is consulted by two callers: the module-jobs runtime, which
// opens it once per application, and `nucleus doctor --check tasks`, which
// inspects a deployment without opening a worker.
type Provider interface {
	// Open builds the manager, scheduler and inspector the framework drives
	// for one application. The provider owns the choices its configuration
	// expresses — SchedulerLock, for one — and logs them through
	// opts.Logger, so the boot log says what the provider is doing.
	Open(opts ProviderOptions) (ProviderRuntime, error)

	// Inspect reports the state of the deployment the options name, without
	// starting a worker. It never panics: a backend that cannot be reached
	// is a snapshot with Enabled false and the Reason filled in.
	Inspect(opts ProviderOptions) RuntimeSnapshot
}

// ProviderOptions is what the framework hands a Provider: the task
// configuration derived from the `jobs_*` keys, and the logger the
// application runs on.
type ProviderOptions struct {
	// Config carries the broker URL (`jobs_redis_url`), the worker
	// concurrency (`jobs_concurrency`) and the queue weights.
	Config Config
	// SchedulerLock is `jobs_scheduler_lock`: whether the scheduler runs
	// under leader election, so that with several replicas exactly one
	// ticks the cron entries. A provider that cannot elect a leader
	// ignores it and says so in its documentation.
	SchedulerLock bool
	// Logger is the application's logger; nil means slog.Default().
	Logger *slog.Logger
}

// ProviderRuntime is what a Provider builds: the three handles the
// module-jobs runtime drives. Manager and Scheduler are required; Inspector
// may be nil for a provider with nothing to inspect.
type ProviderRuntime struct {
	Manager   Manager
	Scheduler Scheduler
	Inspector Inspector
}

var (
	providersMu sync.RWMutex
	providers   = map[string]Provider{}
)

// RegisterProvider records the Provider selected by `jobs_provider: name`.
//
// It returns an error rather than panicking on a duplicate so that a module
// linked twice through different paths fails loudly at init in the module's
// own code, where the import that caused it is visible.
func RegisterProvider(name string, p Provider) error {
	if name == "" {
		return fmt.Errorf("tasks: provider name is required")
	}
	if p == nil {
		return fmt.Errorf("tasks: %s: provider is nil", name)
	}
	providersMu.Lock()
	defer providersMu.Unlock()
	if _, dup := providers[name]; dup {
		return fmt.Errorf("tasks: provider %q is already registered", name)
	}
	providers[name] = p
	return nil
}

// MustRegisterProvider is RegisterProvider for use in an init(), where there
// is no caller to hand an error back to.
func MustRegisterProvider(name string, p Provider) {
	if err := RegisterProvider(name, p); err != nil {
		panic(err)
	}
}

// LookupProvider returns the Provider registered under name.
func LookupProvider(name string) (Provider, bool) {
	providersMu.RLock()
	defer providersMu.RUnlock()
	p, ok := providers[name]
	return p, ok
}

// RegisteredProviders returns the names registered through
// RegisterProvider, sorted. It is what an error message uses to tell an
// operator what IS linked in; the built-in memory and sql providers are not
// listed because they are always there.
func RegisteredProviders() []string {
	providersMu.RLock()
	defer providersMu.RUnlock()
	names := make([]string, 0, len(providers))
	for n := range providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
