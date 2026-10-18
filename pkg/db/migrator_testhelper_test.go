package db

import (
	"io/fs"
	"log/slog"
	"testing"
)

// mustModuleMigrator builds a directory-backed, module-scoped Migrator for
// a test and fails it on input the constructor rejects.
func mustModuleMigrator(t *testing.T, d *DB, dir, module string, logger *slog.Logger) *Migrator {
	t.Helper()
	m, err := NewMigratorFromConfig(MigratorConfig{DB: d, Dir: dir, Module: module}, logger)
	if err != nil {
		t.Fatalf("NewMigratorFromConfig: %v", err)
	}
	return m
}

// mustModuleFSMigrator is mustModuleMigrator over an fs.FS source.
func mustModuleFSMigrator(t *testing.T, d *DB, fsys fs.FS, module string, logger *slog.Logger) *Migrator {
	t.Helper()
	m, err := NewMigratorFromConfig(MigratorConfig{DB: d, FS: fsys, Module: module}, logger)
	if err != nil {
		t.Fatalf("NewMigratorFromConfig: %v", err)
	}
	return m
}
