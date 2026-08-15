// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Regression tests for QCD-CLI-4 (quantum-coverage-demo): `generate resource`
// always rendered the SQLite migration scaffold — `"id" INTEGER PRIMARY KEY
// AUTOINCREMENT`, DATETIME columns — regardless of the database the project
// is configured against, and offered no dialect flag. `nucleus migrate` then
// failed on PostgreSQL with `syntax error at or near "AUTOINCREMENT"`
// (SQLSTATE 42601) for a migration the CLI itself produced.
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scaffoldResourceProject writes the minimal project layout `generate
// resource` needs (a go.mod for module detection) plus an optional
// nucleus.yml, runs the generator, and returns the up migration SQL.
func scaffoldResourceProject(t *testing.T, configYAML string, extraArgs ...string) string {
	t.Helper()
	dir := t.TempDir()

	goMod := "module example.com/qcd4demo\n\ngo 1.26\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if configYAML != "" {
		if err := os.WriteFile(filepath.Join(dir, "nucleus.yml"), []byte(configYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	args := append([]string{"resource", "announcement", "--out", dir}, extraArgs...)
	var stdout, stderr bytes.Buffer
	if err := runGenerate(args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate(%v): %v\nstderr: %s", args, err, stderr.String())
	}

	matches, err := filepath.Glob(filepath.Join(dir, "migrations", "*_create_announcements_table.up.sql"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected exactly one up migration, got %v (err=%v)", matches, err)
	}
	up, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(up)
}

func assertPostgresScaffold(t *testing.T, up string) {
	t.Helper()
	if strings.Contains(up, "AUTOINCREMENT") {
		t.Errorf("up migration carries SQLite-only AUTOINCREMENT for a PostgreSQL database:\n%s", up)
	}
	if !strings.Contains(up, "BIGSERIAL PRIMARY KEY") {
		t.Errorf("up migration lacks the PostgreSQL auto-increment PK idiom:\n%s", up)
	}
	if strings.Contains(up, "DATETIME") {
		t.Errorf("up migration carries SQLite DATETIME columns for a PostgreSQL database:\n%s", up)
	}
}

// The scaffolded migration must match the database the project is configured
// against — here PostgreSQL via nucleus.yml, the same config `nucleus
// migrate` will read before applying that very migration.
func TestGenerateResourceMigrationHonorsConfiguredDatabase(t *testing.T) {
	configYAML := "databases:\n  default:\n    url: postgres://demo:demo@localhost:5432/demo\n"
	up := scaffoldResourceProject(t, configYAML)
	assertPostgresScaffold(t, up)
}

// An explicit --dialect must win with no config file at all.
func TestGenerateResourceDialectFlagOverride(t *testing.T) {
	up := scaffoldResourceProject(t, "", "--dialect", "postgresql")
	assertPostgresScaffold(t, up)
}

// Without config or flag the historic SQLite default stays (a fresh `nucleus
// new` project runs on sqlite://nucleus.db).
func TestGenerateResourceDefaultsToSQLite(t *testing.T) {
	up := scaffoldResourceProject(t, "")
	if !strings.Contains(up, "AUTOINCREMENT") {
		t.Errorf("with no configured database the scaffold should keep the sqlite default:\n%s", up)
	}
}
