// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package asynqprovider_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/internal/testredis"
	"github.com/jcsvwinston/nucleus/internal/testsqlite"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// The application boots on SQLite; the root module's tests link it through
// internal/testsqlite (NU-106), as every other package's do.
func init() { testsqlite.Register() }

// The framework's selection of the provider, seen from outside the package:
// an application that selects `jobs_provider: asynq` has its module jobs
// scheduled on Redis and run by the worker — registration → schedule →
// enqueue → worker → handler, through pkg/nucleus/jobs.go and the pkg/tasks
// registry. Nothing here imports the provider for its side effect: in 1.x
// pkg/nucleus links it, and this is the test that says so; at v2.0.0 the
// blank import becomes the application's (DEP-2026-020).
func TestFramework_SelectsTheProviderThroughTheRegistry(t *testing.T) {
	srv := testredis.Run(t)

	var runs atomic.Int64
	mod := nucleus.Module[struct{}]{
		Name: "billing",
		Jobs: func(j nucleus.JobRegistry, _ struct{}) {
			_ = j.Register("tick", nucleus.JobSpec{
				Every:   time.Second,
				Handler: func(context.Context) error { runs.Add(1); return nil },
			})
		},
	}

	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.JWTSecret = strings.Repeat("tasks-asynq-framework-test", 2)
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "app.db")},
	}
	cfg.JobsProvider = "asynq"
	cfg.JobsRedisURL = "redis://" + srv.Addr()
	cfg.JobsConcurrency = 2

	nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{"billing": mod.Build()},
	})

	// Scheduler tick (1s) + asynq's queue poll interval: allow a wide margin.
	stop := time.Now().Add(30 * time.Second)
	for time.Now().Before(stop) {
		if runs.Load() >= 2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the module job ran %d times on asynq, want at least 2", runs.Load())
}

// The registry answer the framework relies on, stated once from the outside:
// asynq is the one provider the framework registers beyond memory and sql.
func TestFramework_RegistryNamesTheProvider(t *testing.T) {
	if _, ok := tasks.LookupProvider("asynq"); !ok {
		t.Fatal("linking pkg/tasks/providers/asynq did not register jobs_provider asynq")
	}
	names := tasks.RegisteredProviders()
	if len(names) != 1 || names[0] != "asynq" {
		t.Fatalf("RegisteredProviders() = %v, want [asynq]", names)
	}
}
