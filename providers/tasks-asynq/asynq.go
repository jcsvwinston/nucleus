// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package asynqprovider is the import path the Redis-backed jobs provider
// has from v2.0.0 on (DEP-2026-020): at the major the code of
// pkg/tasks/providers/asynq moves here, with its package name, its symbols
// and their behaviour unchanged, and an application that selects
// `jobs_provider: asynq` links this module itself.
//
// In 1.x this module FORWARDS. It imports pkg/tasks/providers/asynq, which
// is still in the framework's root module, and re-exports its whole
// surface: the types are aliases, so a *Manager built here is the same type
// the in-tree package returns; the constants and variables are the same
// values; the constructors call the in-tree ones. Importing this package
// runs the in-tree package's init, which registers the provider with
// pkg/tasks, and nothing registers it twice: the framework's own blank
// import (pkg/nucleus) and this one name the same package.
//
// So, until v2.0.0, the blank import is optional: the framework links the
// provider on its own. Writing it today is what makes the 2.0 upgrade a
// version bump and nothing else. `nucleus add tasks-asynq` writes it:
//
//	import _ "github.com/jcsvwinston/nucleus/providers/tasks-asynq"
//
// A test in contracts/ (tasks_asynq_module_test.go) holds this file to the
// exported surface of the in-tree package, so the two cannot drift apart in
// silence before the move.
package asynqprovider

import (
	"log/slog"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
	asynq "github.com/jcsvwinston/nucleus/pkg/tasks/providers/asynq"
)

// Name is what `jobs_provider` selects this provider by.
const Name = asynq.Name

// DefaultSchedulerLockKey is the Redis key the leader-elected scheduler
// locks.
const DefaultSchedulerLockKey = asynq.DefaultSchedulerLockKey

// DefaultSchedulerLockTTL is how long a leader holds the scheduler lock
// without renewing it.
const DefaultSchedulerLockTTL = asynq.DefaultSchedulerLockTTL

// The errors the provider returns, the same values the in-tree package
// returns, so errors.Is works across the two import paths.
var (
	ErrNilScheduler     = asynq.ErrNilScheduler
	ErrCronSpecRequired = asynq.ErrCronSpecRequired
	ErrRedisURLRequired = asynq.ErrRedisURLRequired
	ErrTaskTypeRequired = asynq.ErrTaskTypeRequired
	ErrNilHandler       = asynq.ErrNilHandler
	ErrNilManager       = asynq.ErrNilManager
	ErrNilTask          = asynq.ErrNilTask
)

// The types, as aliases: a value of one of them IS a value of the in-tree
// type, methods included.
type (
	// Inspector reads the queues, servers and workers of one Redis.
	Inspector = asynq.Inspector
	// LeaderScheduler is a Scheduler that ticks only on the replica
	// holding the Redis lock.
	LeaderScheduler = asynq.LeaderScheduler
	// LeaderSchedulerConfig configures a LeaderScheduler.
	LeaderSchedulerConfig = asynq.LeaderSchedulerConfig
	// Manager is the tasks.Manager over asynq: enqueue, handle, run, close.
	Manager = asynq.Manager
	// PeriodicTask is one cron entry a Scheduler registers.
	PeriodicTask = asynq.PeriodicTask
	// Scheduler enqueues periodic tasks from cron specs.
	Scheduler = asynq.Scheduler
	// SchedulerConfig configures a Scheduler.
	SchedulerConfig = asynq.SchedulerConfig
)

// NewManager builds a Manager from the configuration: the Redis under
// cfg.RedisURL (required), cfg.Concurrency workers (asynq's default of 10
// when zero), cfg.Queues and cfg.StrictPriority as the queue weights. A nil
// logger means slog.Default().
func NewManager(cfg tasks.Config, logger *slog.Logger) (*Manager, error) {
	return asynq.NewManager(cfg, logger)
}

// NewScheduler builds a Scheduler on the Redis under cfg.RedisURL.
func NewScheduler(cfg SchedulerConfig) (*Scheduler, error) {
	return asynq.NewScheduler(cfg)
}

// NewLeaderScheduler wraps a Scheduler in leader election over a Redis
// lock, so that exactly one replica ticks the cron entries.
func NewLeaderScheduler(cfg LeaderSchedulerConfig) (*LeaderScheduler, error) {
	return asynq.NewLeaderScheduler(cfg)
}

// NewInspector returns an Inspector over the Redis at redisURL. Nothing is
// dialled until a method is called.
func NewInspector(redisURL string) *Inspector {
	return asynq.NewInspector(redisURL)
}

// InspectRuntime reads the queues, servers and workers of the Redis at
// redisURL into a snapshot. It never panics: an unreachable server comes
// back as Enabled=false with the reason.
func InspectRuntime(redisURL string) tasks.RuntimeSnapshot {
	return asynq.InspectRuntime(redisURL)
}

// OperateQueue applies one operational action to the named queue of the
// Redis at redisURL: pause, unpause, retry, archive-retry, retry-archived or
// purge-archived.
func OperateQueue(redisURL, queue, action string) (tasks.QueueActionResult, error) {
	return asynq.OperateQueue(redisURL, queue, action)
}
