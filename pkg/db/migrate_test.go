package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jcsvwinston/GoFrame/pkg/observe"
)

type migrateTestModel struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"not null"`
}

func TestAutoMigrate_OnBunEngine_ReturnsErrGORMRequired(t *testing.T) {
	d := newTestBunDB(t)
	err := d.AutoMigrate(&migrateTestModel{})
	if !errors.Is(err, ErrGORMRequired) {
		t.Fatalf("expected ErrGORMRequired, got %v", err)
	}
}

func TestAutoMigrate_OnGORMEngine_Succeeds(t *testing.T) {
	d := newTestDB(t)
	if err := d.AutoMigrate(&migrateTestModel{}); err != nil {
		t.Fatalf("expected AutoMigrate success, got %v", err)
	}
}

func TestMigratorCreate_WritesUpAndDownFiles(t *testing.T) {
	d := newTestBunDB(t)
	dir := t.TempDir()

	m := NewMigrator(d, dir, observe.NewLogger("error", "text"))
	if err := m.Create("init_schema"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 migration files, got %d", len(entries))
	}

	var hasUp, hasDown bool
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".up.sql") {
			hasUp = true
		}
		if strings.HasSuffix(name, ".down.sql") {
			hasDown = true
		}
	}
	if !hasUp || !hasDown {
		t.Fatalf("expected both .up.sql and .down.sql files, got: %#v", entries)
	}
}

func TestMigrator_UpStatusDown(t *testing.T) {
	d := newTestBunDB(t)
	dir := t.TempDir()
	writeMigrationPair(t, dir, "000001_create_items",
		"CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL);",
		"DROP TABLE IF EXISTS items;",
	)
	writeMigrationPair(t, dir, "000002_create_audit_logs",
		"CREATE TABLE audit_logs (id INTEGER PRIMARY KEY, message TEXT NOT NULL);",
		"DROP TABLE IF EXISTS audit_logs;",
	)

	m := NewMigrator(d, dir, observe.NewLogger("error", "text"))

	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status (initial) failed: %v", err)
	}
	if len(st) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(st))
	}
	for _, s := range st {
		if s.Applied {
			t.Fatalf("migration %s should not be applied initially", s.ID)
		}
	}

	if err := m.Up(); err != nil {
		t.Fatalf("Up failed: %v", err)
	}
	if !tableExists(t, d, "items") {
		t.Fatal("items table should exist after Up")
	}
	if !tableExists(t, d, "audit_logs") {
		t.Fatal("audit_logs table should exist after Up")
	}

	st, err = m.Status()
	if err != nil {
		t.Fatalf("Status (after up) failed: %v", err)
	}
	for _, s := range st {
		if !s.Applied {
			t.Fatalf("migration %s should be applied after Up", s.ID)
		}
		if s.AppliedAt == nil {
			t.Fatalf("migration %s should have applied_at", s.ID)
		}
	}

	if err := m.Down(); err != nil {
		t.Fatalf("Down failed: %v", err)
	}
	if !tableExists(t, d, "items") {
		t.Fatal("items table should still exist after rolling back one migration")
	}
	if tableExists(t, d, "audit_logs") {
		t.Fatal("audit_logs table should not exist after Down")
	}

	st, err = m.Status()
	if err != nil {
		t.Fatalf("Status (after down) failed: %v", err)
	}
	if !st[0].Applied {
		t.Fatalf("expected first migration to remain applied after Down: %+v", st[0])
	}
	if st[1].Applied {
		t.Fatalf("expected second migration to be rolled back after Down: %+v", st[1])
	}
}

func TestMigrator_Steps(t *testing.T) {
	d := newTestBunDB(t)
	dir := t.TempDir()
	writeMigrationPair(t, dir, "000001_create_items",
		"CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL);",
		"DROP TABLE IF EXISTS items;",
	)
	writeMigrationPair(t, dir, "000002_create_audit_logs",
		"CREATE TABLE audit_logs (id INTEGER PRIMARY KEY, message TEXT NOT NULL);",
		"DROP TABLE IF EXISTS audit_logs;",
	)

	m := NewMigrator(d, dir, observe.NewLogger("error", "text"))

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) failed: %v", err)
	}
	if !tableExists(t, d, "items") {
		t.Fatal("items table should exist after Steps(1)")
	}
	if tableExists(t, d, "audit_logs") {
		t.Fatal("audit_logs table should not exist after Steps(1)")
	}

	if err := m.Steps(10); err != nil {
		t.Fatalf("Steps(10) failed: %v", err)
	}
	if !tableExists(t, d, "audit_logs") {
		t.Fatal("audit_logs table should exist after applying remaining migrations")
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("Steps(-1) failed: %v", err)
	}
	if tableExists(t, d, "audit_logs") {
		t.Fatal("audit_logs table should be rolled back by Steps(-1)")
	}
	if !tableExists(t, d, "items") {
		t.Fatal("items table should remain after Steps(-1)")
	}

	if err := m.Steps(-10); err != nil {
		t.Fatalf("Steps(-10) failed: %v", err)
	}
	if tableExists(t, d, "items") {
		t.Fatal("items table should be rolled back after Steps(-10)")
	}
}

func TestMigrator_DownMissingFile_ReturnsError(t *testing.T) {
	d := newTestBunDB(t)
	dir := t.TempDir()

	writeMigrationUpOnly(t, dir, "000001_create_items", "CREATE TABLE items (id INTEGER PRIMARY KEY);")

	m := NewMigrator(d, dir, observe.NewLogger("error", "text"))
	if err := m.Up(); err != nil {
		t.Fatalf("Up failed: %v", err)
	}
	err := m.Down()
	if err == nil {
		t.Fatal("expected Down to fail when down migration file is missing")
	}
	if !strings.Contains(err.Error(), "missing .down.sql file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeMigrationPair(t *testing.T, dir, id, upSQL, downSQL string) {
	t.Helper()
	writeFile(t, fmt.Sprintf("%s/%s.up.sql", dir, id), upSQL)
	writeFile(t, fmt.Sprintf("%s/%s.down.sql", dir, id), downSQL)
}

func writeMigrationUpOnly(t *testing.T, dir, id, upSQL string) {
	t.Helper()
	writeFile(t, fmt.Sprintf("%s/%s.up.sql", dir, id), upSQL)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write file %s failed: %v", path, err)
	}
}

func tableExists(t *testing.T, d *DB, table string) bool {
	t.Helper()

	sqlDB, err := d.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB failed: %v", err)
	}

	var cnt int
	row := sqlDB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name = ?", table)
	if err := row.Scan(&cnt); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("tableExists scan failed: %v", err)
	}
	return cnt > 0
}
