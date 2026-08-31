package cache

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// Compile-time checks: both backends satisfy the Cache contract.
var (
	_ Cache = (*Memory)(nil)
	_ Cache = (*SQL)(nil)
)

func TestMemory_SetGetDelete(t *testing.T) {
	m := NewMemory()
	ctx := t.Context()

	if _, ok, err := m.Get(ctx, "missing"); err != nil || ok {
		t.Fatalf("Get(missing) = ok=%v err=%v, want absent", ok, err)
	}
	if err := m.Set(ctx, "k", []byte("v1"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok, err := m.Get(ctx, "k")
	if err != nil || !ok || !bytes.Equal(got, []byte("v1")) {
		t.Fatalf("Get(k) = %q ok=%v err=%v", got, ok, err)
	}
	// Replace value and TTL.
	if err := m.Set(ctx, "k", []byte("v2"), time.Minute); err != nil {
		t.Fatalf("Set replace: %v", err)
	}
	if got, _, _ := m.Get(ctx, "k"); !bytes.Equal(got, []byte("v2")) {
		t.Fatalf("Get after replace = %q, want v2", got)
	}
	if err := m.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := m.Get(ctx, "k"); ok {
		t.Fatal("Get after Delete still present")
	}
	// Deleting an absent key is not an error.
	if err := m.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete absent: %v", err)
	}
}

func TestMemory_Expiry(t *testing.T) {
	m := NewMemory()
	ctx := t.Context()

	current := time.Now()
	m.now = func() time.Time { return current }

	if err := m.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, ok, _ := m.Get(ctx, "k"); !ok {
		t.Fatal("entry should be live before expiry")
	}
	current = current.Add(time.Minute + time.Second)
	if _, ok, _ := m.Get(ctx, "k"); ok {
		t.Fatal("entry should be expired")
	}
}

func TestMemory_PruneExpired(t *testing.T) {
	m := NewMemory()
	ctx := t.Context()

	current := time.Now()
	m.now = func() time.Time { return current }

	_ = m.Set(ctx, "live", []byte("v"), time.Hour)
	_ = m.Set(ctx, "dead", []byte("v"), time.Second)
	current = current.Add(time.Minute)

	removed, err := m.PruneExpired(ctx)
	if err != nil || removed != 1 {
		t.Fatalf("PruneExpired = %d, %v; want 1, nil", removed, err)
	}
	if _, ok, _ := m.Get(ctx, "live"); !ok {
		t.Fatal("live entry pruned by mistake")
	}
}

func TestMemory_InvalidInputs(t *testing.T) {
	m := NewMemory()
	ctx := t.Context()
	if err := m.Set(ctx, "", []byte("v"), time.Minute); err != ErrEmptyKey {
		t.Fatalf("Set empty key: %v, want ErrEmptyKey", err)
	}
	if err := m.Set(ctx, "k", []byte("v"), 0); err != ErrNonPositiveTTL {
		t.Fatalf("Set zero ttl: %v, want ErrNonPositiveTTL", err)
	}
	if _, _, err := m.Get(ctx, ""); err != ErrEmptyKey {
		t.Fatalf("Get empty key: %v, want ErrEmptyKey", err)
	}
	if err := m.Delete(ctx, ""); err != ErrEmptyKey {
		t.Fatalf("Delete empty key: %v, want ErrEmptyKey", err)
	}
}

func TestMemory_ReturnedSliceIsACopy(t *testing.T) {
	m := NewMemory()
	ctx := t.Context()
	_ = m.Set(ctx, "k", []byte("abc"), time.Minute)
	got, _, _ := m.Get(ctx, "k")
	got[0] = 'X'
	again, _, _ := m.Get(ctx, "k")
	if !bytes.Equal(again, []byte("abc")) {
		t.Fatalf("cache corrupted through returned slice: %q", again)
	}
}

// openSQLiteCacheTable opens an in-memory sqlite database and creates the
// cache table with the exact DDL `nucleus createcachetable` runs for the
// sqlite flavor (internal/cli/cachecommands.go).
func openSQLiteCacheTable(t *testing.T) *sql.DB {
	t.Helper()
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS "nucleus_cache_entries" ("cache_key" TEXT PRIMARY KEY, "value" BLOB NOT NULL, "expires_at" TEXT NOT NULL, "created_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS "nucleus_cache_entries_expires_idx" ON "nucleus_cache_entries" ("expires_at")`,
	}
	for _, stmt := range ddl {
		if _, err := dbConn.Exec(stmt); err != nil {
			t.Fatalf("create cache table: %v", err)
		}
	}
	return dbConn
}

func newSQLiteCache(t *testing.T) *SQL {
	t.Helper()
	c, err := NewSQL(openSQLiteCacheTable(t), SQLOptions{System: "sqlite"})
	if err != nil {
		t.Fatalf("NewSQL: %v", err)
	}
	return c
}

func TestSQL_SetGetDelete(t *testing.T) {
	c := newSQLiteCache(t)
	ctx := t.Context()

	if _, ok, err := c.Get(ctx, "missing"); err != nil || ok {
		t.Fatalf("Get(missing) = ok=%v err=%v, want absent", ok, err)
	}
	if err := c.Set(ctx, "k", []byte("v1"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok, err := c.Get(ctx, "k")
	if err != nil || !ok || !bytes.Equal(got, []byte("v1")) {
		t.Fatalf("Get(k) = %q ok=%v err=%v", got, ok, err)
	}
	// Upsert path: replacing must not violate the primary key.
	if err := c.Set(ctx, "k", []byte("v2"), time.Minute); err != nil {
		t.Fatalf("Set replace: %v", err)
	}
	if got, _, _ := c.Get(ctx, "k"); !bytes.Equal(got, []byte("v2")) {
		t.Fatalf("Get after replace = %q, want v2", got)
	}
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := c.Get(ctx, "k"); ok {
		t.Fatal("Get after Delete still present")
	}
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete absent: %v", err)
	}
}

func TestSQL_ExpiryIsEnforcedServerSide(t *testing.T) {
	c := newSQLiteCache(t)
	ctx := t.Context()

	// Freeze the client clock two hours in the past: an entry Set with a
	// one-hour TTL is already expired from the database clock's point of
	// view, so Get (which compares against datetime('now')) must miss.
	c.now = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	if err := c.Set(ctx, "stale", []byte("v"), time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, ok, err := c.Get(ctx, "stale"); err != nil || ok {
		t.Fatalf("expired entry visible: ok=%v err=%v", ok, err)
	}

	removed, err := c.PruneExpired(ctx)
	if err != nil || removed != 1 {
		t.Fatalf("PruneExpired = %d, %v; want 1, nil", removed, err)
	}
}

func TestSQL_InvalidInputs(t *testing.T) {
	c := newSQLiteCache(t)
	ctx := t.Context()
	if err := c.Set(ctx, "", []byte("v"), time.Minute); err != ErrEmptyKey {
		t.Fatalf("Set empty key: %v", err)
	}
	if err := c.Set(ctx, "k", []byte("v"), -time.Second); err != ErrNonPositiveTTL {
		t.Fatalf("Set negative ttl: %v", err)
	}
}

func TestNewSQL_Validation(t *testing.T) {
	dbConn := openSQLiteCacheTable(t)
	if _, err := NewSQL(nil, SQLOptions{System: "sqlite"}); err == nil {
		t.Fatal("nil db accepted")
	}
	if _, err := NewSQL(dbConn, SQLOptions{System: "dbase"}); err == nil {
		t.Fatal("unknown system accepted")
	}
	if _, err := NewSQL(dbConn, SQLOptions{System: "sqlite", Table: "bad name; drop"}); err == nil {
		t.Fatal("invalid table name accepted")
	}
	for _, system := range []string{"sqlite", "postgresql", "mysql", "mssql", "oracle"} {
		if _, err := NewSQL(dbConn, SQLOptions{System: system}); err != nil {
			t.Fatalf("NewSQL(%s): %v", system, err)
		}
	}
}

func TestNewSQL_StatementShapes(t *testing.T) {
	dbConn := openSQLiteCacheTable(t)
	pg, err := NewSQL(dbConn, SQLOptions{System: "postgresql"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pg.setStmt, "ON CONFLICT") || !strings.Contains(pg.getStmt, "$1") {
		t.Fatalf("postgresql statements malformed: %q / %q", pg.setStmt, pg.getStmt)
	}
	my, err := NewSQL(dbConn, SQLOptions{System: "mysql"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(my.setStmt, "ON DUPLICATE KEY UPDATE") || !strings.Contains(my.getStmt, "UTC_TIMESTAMP()") {
		t.Fatalf("mysql statements malformed: %q / %q", my.setStmt, my.getStmt)
	}
	ms, err := NewSQL(dbConn, SQLOptions{System: "mssql"})
	if err != nil {
		t.Fatal(err)
	}
	if ms.setStmt != "" || ms.txInsert == "" {
		t.Fatalf("mssql should use the tx path: setStmt=%q txInsert=%q", ms.setStmt, ms.txInsert)
	}
	ora, err := NewSQL(dbConn, SQLOptions{System: "oracle"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ora.txInsert, `"NUCLEUS_CACHE_ENTRIES"`) {
		t.Fatalf("oracle identifiers must be upper-cased like the CLI's: %q", ora.txInsert)
	}
}

func TestSQL_TxUpsertPathWorksOnSQLite(t *testing.T) {
	// The mssql/oracle delete+insert path cannot run against those engines
	// here, but the transactional flow itself can be exercised on sqlite by
	// wiring the tx statements with sqlite placeholders.
	dbConn := openSQLiteCacheTable(t)
	c, err := NewSQL(dbConn, SQLOptions{System: "sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	c.setStmt = "" // force the tx path
	c.txDelete = `DELETE FROM "nucleus_cache_entries" WHERE "cache_key" = ?`
	c.txInsert = `INSERT INTO "nucleus_cache_entries" ("cache_key", "value", "expires_at", "created_at", "updated_at") VALUES (?, ?, ?, ?, ?)`

	ctx := t.Context()
	if err := c.Set(ctx, "k", []byte("v1"), time.Minute); err != nil {
		t.Fatalf("tx Set: %v", err)
	}
	if err := c.Set(ctx, "k", []byte("v2"), time.Minute); err != nil {
		t.Fatalf("tx Set replace: %v", err)
	}
	got, ok, err := c.Get(ctx, "k")
	if err != nil || !ok || !bytes.Equal(got, []byte("v2")) {
		t.Fatalf("Get after tx replace = %q ok=%v err=%v", got, ok, err)
	}
}

func TestContextCancellationSurfaces(t *testing.T) {
	c := newSQLiteCache(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Set(ctx, "k", []byte("v"), time.Minute); err == nil {
		t.Fatal("Set with cancelled context should fail")
	}
}
