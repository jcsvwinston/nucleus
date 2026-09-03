package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observe"
)

type migrateTestModel struct {
	ID   uint   `db:"pk"`
	Name string `db:"required"`
}

func TestAutoMigrate_ReturnsUnsupportedError(t *testing.T) {
	d := newTestDB(t)
	if err := d.AutoMigrate(&migrateTestModel{}); !errors.Is(err, ErrAutoMigrate) {
		t.Fatalf("expected ErrAutoMigrate, got %v", err)
	}
}

func TestMigratorCreate_WritesUpAndDownFiles(t *testing.T) {
	d := newTestDB(t)
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
	d := newTestDB(t)
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
	d := newTestDB(t)
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
	d := newTestDB(t)
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

// --- Phase 2d: module migration namespacing ---

// queryString runs a single-row, single-column scan against the test
// DB. Used to peek at the framework's bookkeeping tables and assert
// the namespaced storage IDs are present.
func queryString(t *testing.T, d *DB, q string, args ...any) string {
	t.Helper()
	sqlDB, err := d.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}
	var v string
	row := sqlDB.QueryRow(q, args...)
	if err := row.Scan(&v); err != nil {
		t.Fatalf("scan: %v (query=%s)", err, q)
	}
	return v
}

// queryInt mirrors queryString for integer columns.
func queryInt(t *testing.T, d *DB, q string, args ...any) int {
	t.Helper()
	sqlDB, err := d.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}
	var v int
	row := sqlDB.QueryRow(q, args...)
	if err := row.Scan(&v); err != nil {
		t.Fatalf("scan: %v (query=%s)", err, q)
	}
	return v
}

func TestModuleMigrator_PrefixesStorageID(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	writeMigrationPair(t, dir, "000001_init",
		"CREATE TABLE articles_items (id INTEGER PRIMARY KEY, title TEXT);",
		"DROP TABLE IF EXISTS articles_items;",
	)

	m := NewModuleMigrator(d, dir, "articles", observe.NewLogger("error", "text"))
	if err := m.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Bookkeeping rows must carry the `articles/` prefix in both tables.
	gotApplied := queryString(t, d, "SELECT id FROM nucleus_schema_migrations WHERE id = ?", "articles/000001_init")
	if gotApplied != "articles/000001_init" {
		t.Errorf("applied table: got %q want articles/000001_init", gotApplied)
	}
	gotChecksum := queryString(t, d, "SELECT id FROM nucleus_schema_migration_checksums WHERE id = ?", "articles/000001_init")
	if gotChecksum != "articles/000001_init" {
		t.Errorf("checksum table: got %q want articles/000001_init", gotChecksum)
	}

	// The bare (unprefixed) ID must NOT exist anywhere in either table.
	count := queryInt(t, d, "SELECT COUNT(*) FROM nucleus_schema_migrations WHERE id = ?", "000001_init")
	if count != 0 {
		t.Errorf("unprefixed legacy ID present in applied table: %d row(s)", count)
	}
	count = queryInt(t, d, "SELECT COUNT(*) FROM nucleus_schema_migration_checksums WHERE id = ?", "000001_init")
	if count != 0 {
		t.Errorf("unprefixed legacy ID present in checksum table: %d row(s)", count)
	}

	// Status() reports the human-readable file ID (no namespace prefix).
	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st) != 1 || st[0].ID != "000001_init" {
		t.Fatalf("Status: got %#v want one entry with file-ID 000001_init", st)
	}
	if !st[0].Applied {
		t.Fatal("Status: migration should be marked applied")
	}
}

func TestModuleMigrator_TwoModulesNoCollision(t *testing.T) {
	// Two modules ship the same migration filename (`000001_init`) and
	// share a database alias. The old shared-keyspace behaviour would
	// fail the second Up() with a PRIMARY KEY collision. With Phase
	// 2d namespacing, both Migrators write distinct storage IDs and
	// both Up() calls succeed.
	d := newTestDB(t)

	articlesDir := t.TempDir()
	usersDir := t.TempDir()
	writeMigrationPair(t, articlesDir, "000001_init",
		"CREATE TABLE articles_items (id INTEGER PRIMARY KEY);",
		"DROP TABLE IF EXISTS articles_items;",
	)
	writeMigrationPair(t, usersDir, "000001_init",
		"CREATE TABLE users_items (id INTEGER PRIMARY KEY);",
		"DROP TABLE IF EXISTS users_items;",
	)

	articles := NewModuleMigrator(d, articlesDir, "articles", observe.NewLogger("error", "text"))
	users := NewModuleMigrator(d, usersDir, "users", observe.NewLogger("error", "text"))

	if err := articles.Up(); err != nil {
		t.Fatalf("articles.Up: %v", err)
	}
	if err := users.Up(); err != nil {
		t.Fatalf("users.Up (expected to coexist with articles): %v", err)
	}

	// Both tables must have been created.
	if !tableExists(t, d, "articles_items") {
		t.Error("articles_items should exist after articles.Up")
	}
	if !tableExists(t, d, "users_items") {
		t.Error("users_items should exist after users.Up")
	}

	// Both rows must be present in the bookkeeping tables.
	applied := queryInt(t, d, "SELECT COUNT(*) FROM nucleus_schema_migrations WHERE id IN (?, ?)", "articles/000001_init", "users/000001_init")
	if applied != 2 {
		t.Errorf("expected 2 namespaced rows in applied table; got %d", applied)
	}
}

func TestUnscopedMigrator_BackwardCompatible(t *testing.T) {
	// The legacy `NewMigrator` constructor produces an unscoped
	// Migrator that stores raw file IDs (no `/` prefix) — preserves
	// the pre-Phase-2d on-disk schema and history for host
	// applications that have not adopted the module pattern.
	d := newTestDB(t)
	dir := t.TempDir()
	writeMigrationPair(t, dir, "000001_legacy",
		"CREATE TABLE legacy_items (id INTEGER PRIMARY KEY);",
		"DROP TABLE IF EXISTS legacy_items;",
	)
	m := NewMigrator(d, dir, observe.NewLogger("error", "text"))
	if err := m.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// The applied row should be stored as the bare file ID — no
	// `<module>/` prefix.
	got := queryString(t, d, "SELECT id FROM nucleus_schema_migrations WHERE id = ?", "000001_legacy")
	if got != "000001_legacy" {
		t.Errorf("unscoped Migrator should store raw file ID; got %q", got)
	}
}

func TestUnscopedMigrator_IgnoresModuleRowsInDrift(t *testing.T) {
	// Drift() should only report the migrations the current Migrator
	// owns. An unscoped Migrator running against a DB that ALSO has
	// namespaced rows from a module-scoped Migrator must not flag the
	// foreign rows as missing-up-file drift.
	d := newTestDB(t)

	// Module-scoped Migrator applies an articles migration.
	articlesDir := t.TempDir()
	writeMigrationPair(t, articlesDir, "000001_init",
		"CREATE TABLE articles_items (id INTEGER PRIMARY KEY);",
		"DROP TABLE IF EXISTS articles_items;",
	)
	articles := NewModuleMigrator(d, articlesDir, "articles", observe.NewLogger("error", "text"))
	if err := articles.Up(); err != nil {
		t.Fatalf("articles.Up: %v", err)
	}

	// Unscoped Migrator with NO files of its own — Drift should be empty.
	bareDir := t.TempDir()
	bare := NewMigrator(d, bareDir, observe.NewLogger("error", "text"))
	drift, err := bare.Drift()
	if err != nil {
		t.Fatalf("bare.Drift: %v", err)
	}
	if len(drift) != 0 {
		t.Errorf("unscoped Migrator should ignore foreign-module rows; got drift: %#v", drift)
	}
}

func TestModuleMigrator_DriftReportsFileIDNotStorageID(t *testing.T) {
	// When Drift fires (e.g. file deleted after apply), the reported
	// ID should be the human-readable file ID, not the namespaced
	// storage ID — operators look at filenames, not storage keys.
	d := newTestDB(t)
	dir := t.TempDir()
	writeMigrationPair(t, dir, "000001_init",
		"CREATE TABLE articles_items (id INTEGER PRIMARY KEY);",
		"DROP TABLE IF EXISTS articles_items;",
	)
	m := NewModuleMigrator(d, dir, "articles", observe.NewLogger("error", "text"))
	if err := m.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Delete the migration files; the row in the bookkeeping table
	// becomes "missing up file" drift.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("rm migrations dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	drift, err := m.Drift()
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if len(drift) != 1 {
		t.Fatalf("expected 1 drift entry; got %d (%#v)", len(drift), drift)
	}
	if drift[0].ID != "000001_init" {
		t.Errorf("Drift should report the file ID (not the storage ID with namespace prefix); got %q", drift[0].ID)
	}
	if drift[0].Kind != DriftKindMissingUpFile {
		t.Errorf("Drift kind: got %q want %q", drift[0].Kind, DriftKindMissingUpFile)
	}
}

func TestModuleMigrator_DownRemovesNamespacedRow(t *testing.T) {
	// Phase 2d guard: `rollbackMigration` must delete the namespaced
	// storage IDs from both bookkeeping tables, not the raw file ID.
	// Without this test a future refactor that drops `namespacedID`
	// from the DELETE statements would leave orphan rows in the DB
	// (and the test for `Up` would still pass since the rows ARE
	// written under the namespaced key).
	d := newTestDB(t)
	dir := t.TempDir()
	writeMigrationPair(t, dir, "000001_init",
		"CREATE TABLE articles_items (id INTEGER PRIMARY KEY);",
		"DROP TABLE IF EXISTS articles_items;",
	)
	m := NewModuleMigrator(d, dir, "articles", observe.NewLogger("error", "text"))
	if err := m.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}
	// Confirm the namespaced row is present before Down.
	cnt := queryInt(t, d, "SELECT COUNT(*) FROM nucleus_schema_migrations WHERE id = ?", "articles/000001_init")
	if cnt != 1 {
		t.Fatalf("pre-Down: expected 1 namespaced row, got %d", cnt)
	}

	if err := m.Down(); err != nil {
		t.Fatalf("Down: %v", err)
	}
	// Both tracking tables must no longer carry the row under the
	// namespaced key.
	cnt = queryInt(t, d, "SELECT COUNT(*) FROM nucleus_schema_migrations WHERE id = ?", "articles/000001_init")
	if cnt != 0 {
		t.Errorf("post-Down: namespaced applied row should be gone; got %d row(s)", cnt)
	}
	cnt = queryInt(t, d, "SELECT COUNT(*) FROM nucleus_schema_migration_checksums WHERE id = ?", "articles/000001_init")
	if cnt != 0 {
		t.Errorf("post-Down: namespaced checksum row should be gone; got %d row(s)", cnt)
	}
}

func TestNewModuleMigrator_RejectsEmptyName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty module name")
		}
	}()
	NewModuleMigrator(newTestDB(t), t.TempDir(), "", observe.NewLogger("error", "text"))
}

func TestNewModuleMigrator_RejectsSlashInName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on module name containing '/'")
		}
	}()
	NewModuleMigrator(newTestDB(t), t.TempDir(), "a/b", observe.NewLogger("error", "text"))
}

// Migration files are named by timestamp and sorted by name. `migrate
// create` used local time while the generators used UTC, so two files
// written minutes apart in a zone east of Greenwich sorted in the wrong
// order — a migration created later ran first. Every namer uses UTC now,
// and this pins the one in pkg/db by running it in a zone twelve hours off.
func TestMigratorCreate_NamesFilesInUTC(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()

	saved := time.Local
	time.Local = time.FixedZone("far-east", 12*3600)
	t.Cleanup(func() { time.Local = saved })

	before := time.Now().UTC().Add(-time.Minute)
	m := NewMigrator(d, dir, observe.NewLogger("error", "text"))
	if err := m.Create("utc_probe"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	after := time.Now().UTC().Add(time.Minute)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		stamp := strings.SplitN(e.Name(), "_", 2)[0]
		got, err := time.ParseInLocation("20060102150405", stamp, time.UTC)
		if err != nil {
			t.Fatalf("%s: prefix is not a timestamp: %v", e.Name(), err)
		}
		if got.Before(before) || got.After(after) {
			t.Fatalf("%s: prefix %s is not UTC now (window %s..%s) — local time leaked into the name", e.Name(), stamp, before.Format("150405"), after.Format("150405"))
		}
	}
}
