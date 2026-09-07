// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Tests for the fs.FS-backed module Migrator (ADR-022, executing the
// ADR-013 §R1 follow-up): embedded migrations go through the same pipeline
// as disk ones — module-scoped ledger, checksums, idempotent re-runs — with
// the FS as the read-only source.
package db

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jcsvwinston/nucleus/pkg/observe"
)

func fsMigrations() fstest.MapFS {
	return fstest.MapFS{
		"000001_create_items.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")},
		"000001_create_items.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE IF EXISTS items;")},
		"000002_create_logs.up.sql":    &fstest.MapFile{Data: []byte("CREATE TABLE logs (id INTEGER PRIMARY KEY, msg TEXT NOT NULL);")},
		"000002_create_logs.down.sql":  &fstest.MapFile{Data: []byte("DROP TABLE IF EXISTS logs;")},
	}
}

func TestFSMigrator_UpStatusDown(t *testing.T) {
	d := newTestDB(t)
	m := NewModuleFSMigrator(d, fsMigrations(), "shop", observe.NewLogger("error", "text"))

	if err := m.Up(); err != nil {
		t.Fatalf("Up over fs.FS: %v", err)
	}
	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(st))
	}
	for _, s := range st {
		if !s.Applied || !s.HasUp || !s.HasDown {
			t.Fatalf("migration %s: want applied with both files, got %+v", s.ID, s)
		}
	}

	// The ledger rows are namespaced `shop/<id>` (same contract as the
	// disk-backed NewModuleMigrator).
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

	// Idempotence: a second Up applies nothing and errors nothing.
	if err := m.Up(); err != nil {
		t.Fatalf("second Up must be a no-op, got %v", err)
	}

	// Down through the FS-read .down.sql.
	if err := m.Down(); err != nil {
		t.Fatalf("Down over fs.FS: %v", err)
	}
	st, err = m.Status()
	if err != nil {
		t.Fatal(err)
	}
	applied := 0
	for _, s := range st {
		if s.Applied {
			applied++
		}
	}
	if applied != 1 {
		t.Fatalf("after Down: want 1 applied, got %d", applied)
	}
}

func TestFSMigrator_DriftSeesEmbeddedChecksums(t *testing.T) {
	d := newTestDB(t)
	fsys := fsMigrations()
	m := NewModuleFSMigrator(d, fsys, "shop", observe.NewLogger("error", "text"))
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	drift, err := m.Drift()
	if err != nil {
		t.Fatalf("Drift over fs.FS: %v", err)
	}
	if len(drift) != 0 {
		t.Fatalf("freshly applied embedded migrations must not drift, got %+v", drift)
	}

	// Mutate the embedded script — the checksum comparison must notice,
	// exactly as it does for an edited disk file.
	fsys["000001_create_items.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE items (id INTEGER PRIMARY KEY);")}
	drift, err = m.Drift()
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 || drift[0].Kind != DriftKindChecksumMismatch {
		t.Fatalf("want one checksum_mismatch entry, got %+v", drift)
	}
}

func TestFSMigrator_CreateRefused(t *testing.T) {
	d := newTestDB(t)
	m := NewModuleFSMigrator(d, fsMigrations(), "shop", observe.NewLogger("error", "text"))
	err := m.Create("nope")
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("Create on an FS-backed Migrator must refuse with the read-only explanation, got %v", err)
	}
}

func TestFSMigrator_MissingUpFileFails(t *testing.T) {
	d := newTestDB(t)
	fsys := fstest.MapFS{
		"000001_x.down.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	m := NewModuleFSMigrator(d, fsys, "shop", observe.NewLogger("error", "text"))
	if err := m.Up(); err == nil || !strings.Contains(err.Error(), "missing .up.sql") {
		t.Fatalf("an id with only .down.sql must fail loudly, got %v", err)
	}
}

func TestNewModuleFSMigrator_ConstructorMisusePanics(t *testing.T) {
	mustPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s: want panic", name)
			}
		}()
		fn()
	}
	d := newTestDB(t)
	mustPanic("nil fsys", func() { NewModuleFSMigrator(d, nil, "shop", nil) })
	mustPanic("empty name", func() { NewModuleFSMigrator(d, fstest.MapFS{}, "", nil) })
	mustPanic("slash in name", func() { NewModuleFSMigrator(d, fstest.MapFS{}, "a/b", nil) })
}

// Applied lists the whole ledger, the host's unscoped rows and every
// module's namespaced ones, each attributed to its namespace — the view
// `nucleus migrate status` merges with the on-disk plan.
func TestMigrator_AppliedListsEveryNamespace(t *testing.T) {
	d := newTestDB(t)
	logger := observe.NewLogger("error", "text")
	if err := NewModuleFSMigrator(d, fsMigrations(), "shop", logger).Up(); err != nil {
		t.Fatalf("shop Up: %v", err)
	}
	host := fstest.MapFS{
		"000001_host.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE host (id INTEGER PRIMARY KEY);")},
		"000001_host.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE IF EXISTS host;")},
	}
	// An unscoped ledger row, written the way a host application's own
	// migrations are recorded (no namespace).
	unscoped := &Migrator{db: d, fsys: host, logger: logger}
	if err := unscoped.Up(); err != nil {
		t.Fatalf("host Up: %v", err)
	}

	rows, err := NewMigrator(d, t.TempDir(), logger).Applied()
	if err != nil {
		t.Fatalf("Applied: %v", err)
	}
	want := []struct{ id, ns string }{
		{"000001_host", ""},
		{"shop/000001_create_items", "shop"},
		{"shop/000002_create_logs", "shop"},
	}
	if len(rows) != len(want) {
		t.Fatalf("want %d ledger rows, got %d: %+v", len(want), len(rows), rows)
	}
	for i, w := range want {
		if rows[i].ID != w.id || rows[i].Namespace != w.ns {
			t.Errorf("row %d: got {%s %s}, want {%s %s}", i, rows[i].ID, rows[i].Namespace, w.id, w.ns)
		}
		if rows[i].AppliedAt.IsZero() {
			t.Errorf("row %d: applied_at must be set", i)
		}
	}
}
