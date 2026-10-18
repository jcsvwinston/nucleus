// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package asynqprovider

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// Name is what `jobs_provider` selects this provider by.
const Name = "asynq"

// provider is the tasks.Provider this module registers at init: the
// framework resolves `jobs_provider: asynq` through it, and `nucleus doctor
// --check tasks` inspects the deployment through it. Importing this package
// for its side effect is the whole wiring, the same shape a database driver
// module has (ADR-030/031):
//
//	import _ "github.com/jcsvwinston/nucleus/providers/tasks-asynq"
type provider struct{}

func init() {
	tasks.MustRegisterProvider(Name, provider{})
}

// Open builds the manager, the scheduler and the inspector for one
// application. The scheduler runs under leader election unless the
// application opted out with jobs_scheduler_lock: false.
func (provider) Open(opts tasks.ProviderOptions) (tasks.ProviderRuntime, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	redisURL := strings.TrimSpace(opts.Config.RedisURL)
	if redisURL == "" {
		// The configuration layer refuses this up front (jobs_provider
		// "asynq" requires jobs_redis_url); the guard keeps the invariant
		// local for programmatic callers, with the key named.
		return tasks.ProviderRuntime{}, fmt.Errorf("asynq: jobs_provider %q requires jobs_redis_url: %w", Name, tasks.ErrRedisURLRequired)
	}
	mgr, err := NewManager(opts.Config, logger)
	if err != nil {
		return tasks.ProviderRuntime{}, fmt.Errorf("asynq: building manager: %w", err)
	}

	// NF-1: each replica used to start its own asynq scheduler, so with N
	// replicas every cron entry was enqueued N times per tick — asynq
	// dedupes workers, never schedulers. Under jobs_scheduler_lock (default)
	// the scheduler runs behind leader election on a Redis lock; opting out
	// is legitimate for a single replica, but it gets a WARN because
	// nothing else stops the duplication.
	var scheduler tasks.Scheduler
	if opts.SchedulerLock {
		sch, err := NewLeaderScheduler(LeaderSchedulerConfig{
			Scheduler: SchedulerConfig{RedisURL: redisURL},
			Logger:    logger,
		})
		if err != nil {
			_ = mgr.Close()
			return tasks.ProviderRuntime{}, fmt.Errorf("asynq: building leader scheduler: %w", err)
		}
		scheduler = sch
		logger.Info("nucleus: asynq jobs scheduler runs under leader election — with multiple replicas exactly one ticks the cron entries (opt out with jobs_scheduler_lock: false)")
	} else {
		sch, err := NewScheduler(SchedulerConfig{RedisURL: redisURL})
		if err != nil {
			_ = mgr.Close()
			return tasks.ProviderRuntime{}, fmt.Errorf("asynq: building scheduler: %w", err)
		}
		scheduler = sch
		logger.Warn("nucleus: jobs_scheduler_lock is disabled — EVERY replica of this process runs its own asynq scheduler, so each cron job fires once per replica; leave the lock on unless this is a single-replica deployment")
	}

	return tasks.ProviderRuntime{
		Manager:   mgr,
		Scheduler: scheduler,
		Inspector: NewInspector(redisURL),
	}, nil
}

// Inspect reports the queues, servers and workers the Redis at
// jobs_redis_url holds, without starting a worker.
func (provider) Inspect(opts tasks.ProviderOptions) tasks.RuntimeSnapshot {
	return InspectRuntime(opts.Config.RedisURL)
}
