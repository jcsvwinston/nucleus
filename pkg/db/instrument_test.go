package db

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// collector is a concurrency-safe StatementObserver sink for the tests.
type collector struct {
	mu   sync.Mutex
	seen []StatementInfo
}

func (c *collector) observe(_ context.Context, info StatementInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, info)
}

func (c *collector) snapshot() []StatementInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]StatementInfo, len(c.seen))
	copy(out, c.seen)
	return out
}

// openInstrumentedSQLite opens an in-memory sqlite *sql.DB wrapped with the
// collector, via the same path db.New uses.
func openInstrumentedSQLite(t *testing.T, c *collector) *sql.DB {
	t.Helper()
	sqlDB, err := openConfiguredDB(Config{
		DatabaseURL:       "sqlite://:memory:",
		StatementObserver: c.observe,
	})
	if err != nil {
		t.Fatalf("openConfiguredDB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}

func TestInstrument_CapturesDirectExecAndQuery(t *testing.T) {
	c := &collector{}
	sqlDB := openInstrumentedSQLite(t, c)
	ctx := context.Background()

	if _, err := sqlDB.ExecContext(ctx, "CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO widgets (name) VALUES (?)", "alpha"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows, err := sqlDB.QueryContext(ctx, "SELECT id, name FROM widgets WHERE name = ?", "alpha")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	_ = rows.Close()

	seen := c.snapshot()
	if len(seen) < 3 {
		t.Fatalf("observed %d statements, want >= 3: %+v", len(seen), seen)
	}

	// Find the INSERT and assert operation + row count + arg sanitization
	// responsibility (args are delivered raw to the observer).
	var insert, selectStmt *StatementInfo
	for i := range seen {
		switch seen[i].Operation {
		case "insert":
			insert = &seen[i]
		case "select":
			selectStmt = &seen[i]
		}
	}
	if insert == nil {
		t.Fatal("no INSERT statement observed")
	}
	if insert.RowsAffected != 1 {
		t.Errorf("INSERT RowsAffected = %d, want 1", insert.RowsAffected)
	}
	if len(insert.Args) != 1 || insert.Args[0] != "alpha" {
		t.Errorf("INSERT Args = %+v, want [alpha] (raw, unredacted)", insert.Args)
	}
	if insert.Err != nil {
		t.Errorf("INSERT Err = %v, want nil", insert.Err)
	}
	if selectStmt == nil {
		t.Fatal("no SELECT statement observed")
	}
	if selectStmt.RowsAffected != 0 {
		t.Errorf("SELECT RowsAffected = %d, want 0 (queries do not report rows)", selectStmt.RowsAffected)
	}
}

func TestInstrument_SkipsModelObservedContext(t *testing.T) {
	c := &collector{}
	sqlDB := openInstrumentedSQLite(t, c)

	if _, err := sqlDB.ExecContext(context.Background(), "CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	before := len(c.snapshot())

	// A statement whose context is marked as already observed at the model
	// layer must NOT be re-recorded by the driver wrapper.
	crudCtx := observe.CtxWithModelObserved(context.Background())
	if _, err := sqlDB.ExecContext(crudCtx, "INSERT INTO t (id) VALUES (1)"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	after := c.snapshot()
	if len(after) != before {
		t.Fatalf("model-observed statement was recorded by the driver wrapper: %d new entries (%+v)", len(after)-before, after[before:])
	}
}

func TestInstrument_CapturesErrors(t *testing.T) {
	c := &collector{}
	sqlDB := openInstrumentedSQLite(t, c)

	// Query a table that does not exist: the driver returns an error, which
	// the observer must surface.
	_, err := sqlDB.ExecContext(context.Background(), "INSERT INTO missing_table (x) VALUES (1)")
	if err == nil {
		t.Fatal("expected an error inserting into a missing table")
	}
	var found bool
	for _, s := range c.snapshot() {
		if s.Err != nil {
			found = true
		}
	}
	if !found {
		t.Error("no observed statement carried the driver error")
	}
}

func TestInstrument_NotWrappedWhenObserverNil(t *testing.T) {
	// Without a StatementObserver the stock path is used and queries still
	// work — this guards the zero-cost default.
	sqlDB, err := openConfiguredDB(Config{DatabaseURL: "sqlite://:memory:"})
	if err != nil {
		t.Fatalf("openConfiguredDB: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	if _, err := sqlDB.ExecContext(context.Background(), "SELECT 1"); err != nil {
		t.Fatalf("select 1: %v", err)
	}
}

func TestInstrument_PreparedStatementPath(t *testing.T) {
	c := &collector{}
	sqlDB := openInstrumentedSQLite(t, c)
	ctx := context.Background()

	if _, err := sqlDB.ExecContext(ctx, "CREATE TABLE p (id INTEGER, label TEXT)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	stmt, err := sqlDB.PrepareContext(ctx, "INSERT INTO p (id, label) VALUES (?, ?)")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer func() { _ = stmt.Close() }()
	if _, err := stmt.ExecContext(ctx, 1, "one"); err != nil {
		t.Fatalf("stmt exec: %v", err)
	}

	// The prepared exec must be observed exactly once (via the stmt path,
	// not double-counted with the conn path).
	inserts := 0
	for _, s := range c.snapshot() {
		if s.Operation == "insert" {
			inserts++
		}
	}
	if inserts != 1 {
		t.Errorf("prepared INSERT observed %d times, want exactly 1", inserts)
	}
}
