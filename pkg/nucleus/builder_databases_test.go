// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-FW-14: the remediation the test kit suggests was not expressible
// from the entry point the test kit documents.
//
// nucleustest's godoc presents Start(t, nucleus.New().FromConfigFile(...))
// as the main way in, and its DB()/MigrateDir() errors tell you to "set
// Databases in the config, e.g. nucleustest.TempSQLite". But *AppBuilder
// had no setter for databases at all, so following both instructions at
// once was impossible — the demo had to drop to Build() → mutate
// App.Databases → StartApp.
package nucleus

import (
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func TestAppBuilder_WithDatabases(t *testing.T) {
	dbs := map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + t.TempDir() + "/probe.db"},
	}

	built, err := New().WithDatabases(dbs).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := built.Databases["default"].URL; got != dbs["default"].URL {
		t.Fatalf("the builder must carry the databases it was given, got %q", got)
	}
}

// The env layer exists to override FILES. An explicit programmatic setter
// is not a file: a test that pins its own database must not have it
// swapped by whatever NUCLEUS_DATABASES__* the developer's shell happens
// to export — which is how SQLite DDL ended up running against the demo's
// PostgreSQL.
func TestAppBuilder_WithDatabases_WinsOverEnvironment(t *testing.T) {
	t.Setenv("NUCLEUS_DATABASES__DEFAULT__URL", "postgres://should-not-win/db")

	want := "sqlite://" + t.TempDir() + "/pinned.db"
	built, err := New().
		WithDatabases(map[string]app.DatabaseConfig{"default": {URL: want}}).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := built.Databases["default"].URL; got != want {
		t.Fatalf("an explicitly pinned database must survive the environment layer, got %q", got)
	}
}
