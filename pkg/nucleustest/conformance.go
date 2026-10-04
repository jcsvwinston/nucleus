// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// The conformance half of the kit: a module checked against what the
// framework expects of it, without an application test around it. Before
// this, a module's mistakes surfaced as a boot failure in somebody's
// application test, one boot at a time — a bad policy row hid the
// duplicate route behind it, which hid the OnShutdown that failed.

// CheckModule checks one module against what the framework expects of it
// and fails the test once for every check the module does not pass,
// naming the check and saying what the framework says about it. It
// returns every verdict, passed ones included:
//
//	func TestNotesModule(t *testing.T) {
//		nucleustest.CheckModule(t, notes.Module())
//	}
//
// The checks are nucleus.CheckModule's — the boot sequence's own, one at a
// time: name, prefix, typed configuration, required databases, policy rows
// and CSRF exemptions (well formed, and each one about a route the module
// serves), templates, models, OnStart, embedded migrations, jobs, webhooks,
// routes, and OnShutdown. They run against a default application with a
// temporary SQLite database, the default-deny enforcer on, and mail and
// storage kept in the process. A module whose migrations are written for
// another engine, that requires a database by name, or that reads a
// configuration file is checked with CheckModuleIn.
func CheckModule(tb testing.TB, spec nucleus.ModuleSpec) []nucleus.ModuleCheck {
	tb.Helper()
	cfg := app.DefaultConfig()
	cfg.LogLevel = "error"
	cfg.Databases = TempSQLite(tb)
	return checkModule(tb, nucleus.App{Config: cfg}, spec)
}

// CheckModuleIn is CheckModule against the application b describes — its
// configuration file and databases, the module's modules.<name> settings
// in that file, and the application's other modules, whose names the
// module must not take (mount the others on b, not the module under
// check):
//
//	nucleustest.CheckModuleIn(t, nucleus.New().
//		FromConfigFile("testdata/nucleus.yml").
//		WithDatabases(map[string]app.DatabaseConfig{"default": {URL: os.Getenv("TEST_DATABASE_URL")}}),
//		billing.Module())
//
// The checks write to the application's databases, so give it a throwaway
// one; an application that leaves the framework's default database in
// place gets a temporary SQLite file instead of a nucleus.db in the
// package directory.
func CheckModuleIn(tb testing.TB, b *nucleus.AppBuilder, spec nucleus.ModuleSpec) []nucleus.ModuleCheck {
	tb.Helper()
	a, err := b.Build()
	if err != nil {
		tb.Fatalf("nucleustest: CheckModuleIn: build the application: %v", err)
	}
	if len(a.Config.Databases) == 0 || reflect.DeepEqual(a.Config.Databases, app.DefaultConfig().Databases) {
		a.Config.Databases = TempSQLite(tb)
	}
	return checkModule(tb, a, spec)
}

func checkModule(tb testing.TB, a nucleus.App, spec nucleus.ModuleSpec) []nucleus.ModuleCheck {
	tb.Helper()
	// The kit mounts its runtime probe beside every module it starts, so its
	// name is taken the way another module's would be.
	modules := make(map[string]nucleus.ModuleSpec, len(a.Modules)+1)
	for name, other := range a.Modules {
		modules[name] = other
	}
	modules[probeModuleName] = (&runtimeProbe{}).spec()
	a.Modules = modules
	// What the check starts keeps its files in the test's temp dir and its
	// mail and storage in the process.
	a.Config.MailDriver = captureMail(a.Config.MailDriver)
	a.Config.Storage.Provider = "memory"
	a.Config.StateDir = filepath.Join(tb.TempDir(), "state")

	name := "<nil>"
	if spec != nil {
		name = spec.Name()
	}
	checks := nucleus.CheckModule(context.Background(), a, spec)
	for _, c := range checks {
		if c.Err != nil {
			tb.Errorf("nucleustest: module %q fails the %s check: %v", name, c.Name, c.Err)
		}
	}
	return checks
}
