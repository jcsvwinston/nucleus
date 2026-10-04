// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// NU-41: the error form of the module migrator constructors reports the input
// NewModuleMigrator and NewModuleFSMigrator panic on.
func TestNewMigratorFromConfig_BadInputIsAnError(t *testing.T) {
	d := newTestDB(t)
	cases := []struct {
		name string
		cfg  MigratorConfig
		want string
	}{
		{"no source", MigratorConfig{DB: d, Module: "shop"}, "set Dir"},
		{"two sources", MigratorConfig{DB: d, Dir: t.TempDir(), FS: fstest.MapFS{}}, "both set"},
		{"slash in module", MigratorConfig{DB: d, FS: fstest.MapFS{}, Module: "a/b"}, "must not contain"},
		{"NUL in module", MigratorConfig{DB: d, Dir: t.TempDir(), Module: "a\x00b"}, "must not contain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := NewMigratorFromConfig(tc.cfg, nil)
			if err == nil || m != nil {
				t.Fatalf("NewMigratorFromConfig = %v, %v; want nil and an error", m, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not say %q", err, tc.want)
			}
		})
	}
}

func TestNewMigratorFromConfig_FSWithModuleScopesTheLedger(t *testing.T) {
	d := newTestDB(t)
	m, err := NewMigratorFromConfig(MigratorConfig{DB: d, FS: fsMigrations(), Module: "shop"}, nil)
	if err != nil {
		t.Fatalf("NewMigratorFromConfig: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}
	sqlDB, err := d.SqlDB()
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM nucleus_schema_migrations WHERE id LIKE 'shop/%'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("want 2 namespaced ledger rows, got %d", count)
	}
}

func TestNewMigratorFromConfig_DirWithoutModuleIsTheBareLedger(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "000001_t.up.sql"), []byte("CREATE TABLE t (id INTEGER PRIMARY KEY);"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "000001_t.down.sql"), []byte("DROP TABLE t;"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := NewMigratorFromConfig(MigratorConfig{DB: d, Dir: dir}, nil)
	if err != nil {
		t.Fatalf("NewMigratorFromConfig: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}
	sqlDB, err := d.SqlDB()
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err := sqlDB.QueryRow("SELECT id FROM nucleus_schema_migrations").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id != "000001_t" {
		t.Fatalf("ledger id = %q, want the bare id", id)
	}
}
