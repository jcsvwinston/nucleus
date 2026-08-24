// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// runtime.go delivers the growth the package contract anticipated
// ("per-test databases, fixture loading"): access to the running
// application's managed resources, a per-test SQLite database, and applying
// the project's SQL migrations — closing the gap where a test could POST
// through the kit but had no way to assert against the database behind it.
package nucleustest

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// probeModuleName is reserved for the kit's runtime-capture module. Start
// fails the test if the application under test already mounts a module with
// this name.
const probeModuleName = "nucleustest_probe"

// runtimeProbe captures the nucleus.Runtime handle the framework passes to
// module OnStart. The kit mounts it as one more module — the same public
// surface every module uses, no core leak — so by the time /healthz answers
// (OnStart runs before routes serve) the handle is already here.
type runtimeProbe struct {
	rt atomic.Value // nucleus.Runtime
}

func (p *runtimeProbe) spec() nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name: probeModuleName,
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			p.rt.Store(rt)
			return nil
		},
	}.Build()
}

// Runtime returns the nucleus.Runtime of the running application — the same
// handle modules receive in OnStart: managed *sql.DB, dialect-aware
// database handles, logger, authorizer, mailer, storage. Fails the test if
// the application has not captured it (it is available as soon as Start
// returns).
func (s *Server) Runtime() nucleus.Runtime {
	s.tb.Helper()
	rt, _ := s.probe.rt.Load().(nucleus.Runtime)
	if rt == nil {
		s.tb.Fatalf("nucleustest: Runtime not captured — the application did not complete module OnStart")
	}
	return rt
}

// DB returns the application's managed *sql.DB (the default alias) so a
// test can assert against the database behind the HTTP surface. Fails the
// test when no database is configured.
func (s *Server) DB() *sql.DB {
	s.tb.Helper()
	dbh := s.Runtime().DB()
	if dbh == nil {
		s.tb.Fatalf("nucleustest: the application has no managed database configured (set Databases in the config, e.g. nucleustest.TempSQLite)")
	}
	return dbh
}

// TempSQLite returns a Databases map whose default alias is a fresh SQLite
// file in a per-test temporary directory — the per-test database: every
// test gets its own isolated file, removed with the test's temp dir.
//
// A file, deliberately not ":memory:": the framework pools connections and
// every pooled connection to ":memory:" opens its own empty database.
func TempSQLite(tb testing.TB) map[string]app.DatabaseConfig {
	tb.Helper()
	return map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(tb.TempDir(), "nucleustest.db")},
	}
}

// MigrateDir applies the project's SQL migrations (the .up.sql/.down.sql
// pairs `nucleus migrate up` runs) from dir against the application's
// default database, through the same Migrator — ledger and checksums
// included. Call it right after Start to give the test schema the exact
// shape production gets:
//
//	srv := nucleustest.Start(t, builder)
//	srv.MigrateDir("../../migrations")
//
// Fails the test on any migration error.
func (s *Server) MigrateDir(dir string) {
	s.tb.Helper()
	handle := s.Runtime().DatabaseHandle()
	if handle == nil {
		s.tb.Fatalf("nucleustest: MigrateDir needs a managed database (set Databases, e.g. nucleustest.TempSQLite)")
	}
	migrator := db.NewMigrator(handle, dir, observe.NewLogger("error", "text"))
	if err := migrator.Up(); err != nil {
		s.tb.Fatalf("nucleustest: apply migrations from %s: %v", dir, err)
	}
}
