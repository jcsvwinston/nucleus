// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// `nucleus migrate` with no action used to run `up` silently. It is now a
// usage error that names the actions, before any configuration or
// database is touched.
func TestMigrateWithoutActionIsAUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runMigrate([]string{"--config", filepath.Join(t.TempDir(), "absent.yml")}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatalf("migrate with no action must fail, got output:\n%s", stdout.String())
	}
	if !strings.Contains(err.Error(), "requires an action") || !strings.Contains(err.Error(), "status") {
		t.Errorf("the error must name the missing action and list the actions, got: %v", err)
	}
	if strings.Contains(stdout.String(), "Migrations applied") {
		t.Errorf("no migration may run without an explicit action:\n%s", stdout.String())
	}
}

// migrate status merges the on-disk plan with the ledger rows modules wrote
// under their `<module>/` namespace when they applied embedded migrations
// at start: an empty migrations directory is no longer "No migration files
// found" when the database holds a module's applied history.
func TestMigrateStatusListsModuleLedgerRows(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := filepath.Join(dir, "nucleus.yml")
	cfg := fmt.Sprintf("database_default: default\ndatabases:\n  default:\n    url: sqlite://%s\nlog_level: error\nlog_format: text\n", dbPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A module applying its embedded migration through the module-scoped
	// ledger, exactly as Runtime.ApplyModuleMigrations does at boot.
	logger := observe.NewLogger("error", "text")
	database, err := db.New(db.Config{Engine: db.EngineSQL, DatabaseURL: "sqlite://" + dbPath}, logger)
	if err != nil {
		t.Fatal(err)
	}
	embedded := fstest.MapFS{
		"000001_create_notes.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE notes (id INTEGER PRIMARY KEY);")},
		"000001_create_notes.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE IF EXISTS notes;")},
	}
	if err := db.NewModuleFSMigrator(database, embedded, "notes", logger).Up(); err != nil {
		t.Fatalf("module Up: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := runMigrate([]string{"--config", cfgPath, "--migrations", migrationsDir, "status"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("migrate status: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, "No migration files found") {
		t.Fatalf("status must not claim there is nothing when the ledger holds module rows:\n%s", out)
	}
	if !strings.Contains(out, "notes/000001_create_notes\tapplied\t") {
		t.Errorf("status must list the module's applied migration under its namespace, got:\n%s", out)
	}
}
