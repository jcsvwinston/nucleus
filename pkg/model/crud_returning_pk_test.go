package model

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// NU5-2: the RETURNING/OUTPUT read-back is only well-formed for a declared,
// integer primary key. These pin the generated clause per shape; the live
// half is TestCRUDLive_NonIntegerAndAbsentPK below.
func TestInsertReturningClause_PKShapes(t *testing.T) {
	type IntPK struct {
		ID   int64  `db:"pk"`
		Body string `db:"column:body"`
	}
	type UUIDPK struct {
		ID   string `db:"pk"`
		Body string `db:"column:body"`
	}
	type NoPK struct {
		Line string `db:"column:line"`
	}

	cases := []struct {
		name    string
		entity  interface{}
		dialect string
		want    string
	}{
		{"integer pk on postgres", &IntPK{}, "postgres", "RETURNING id"},
		{"integer pk on mssql", &IntPK{}, "sqlserver", "OUTPUT INSERTED.id"},
		{"string pk on postgres takes exec path", &UUIDPK{}, "postgres", ""},
		{"string pk on mssql takes exec path", &UUIDPK{}, "sqlserver", ""},
		// Pre-fix, primaryColumn() guessed "id" here and emitted RETURNING id
		// against a table with no such column (42703 where the plain INSERT
		// would have worked).
		{"no pk on postgres takes exec path", &NoPK{}, "postgres", ""},
		{"no pk on mssql takes exec path", &NoPK{}, "sqlserver", ""},
		{"integer pk on sqlite has no read-back clause", &IntPK{}, "sqlite", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta, err := ExtractMeta(tc.entity)
			if err != nil {
				t.Fatalf("ExtractMeta: %v", err)
			}
			crud := NewCRUD(nil, meta, nil)
			crud.SetDialect(tc.dialect)
			if got := crud.insertReturningClause().String(); strings.TrimSpace(got) != tc.want {
				t.Fatalf("clause = %q, want %q", got, tc.want)
			}
		})
	}
}

// A composite PK cannot reach CRUD: ExtractMeta rejects multiple explicit
// primary keys at registration time. Pinned here so the RETURNING logic can
// rely on "at most one declared PK".
func TestExtractMeta_RejectsCompositePK(t *testing.T) {
	type TwoPKs struct {
		A int64 `db:"pk"`
		B int64 `db:"pk"`
	}
	if _, err := ExtractMeta(&TwoPKs{}); err == nil {
		t.Fatal("expected ExtractMeta to reject a composite primary key")
	}
}

// TestCRUDLive_NonIntegerAndAbsentPK is the live half of NU5-2 against the
// engines with a read-back INSERT variant (postgres RETURNING, mssql OUTPUT
// INSERTED): Create on a model with a DB-generated UUID key, and on a model
// with no primary key at all, must succeed. Pre-fix both failed on postgres —
// the UUID case on the int64 scan of RETURNING, the no-PK case with 42703
// (RETURNING id against a table without an id column) — and the mssql branch
// had never executed anywhere (NU5-4).
//
// Gated on NUCLEUS_SQL_MATRIX_URL like the rest of the live matrix; skips on
// engines without a read-back path (mysql).
func TestCRUDLive_NonIntegerAndAbsentPK(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_MATRIX_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_MATRIX_URL is not set; skipping live RETURNING-path test")
	}
	lower := strings.ToLower(rawURL)
	var dialect, uuidDDLTail, noPKColType string
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		dialect, uuidDDLTail, noPKColType = "postgres", "id UUID PRIMARY KEY DEFAULT gen_random_uuid(), body TEXT", "TEXT"
	case strings.HasPrefix(lower, "sqlserver://"):
		dialect, uuidDDLTail, noPKColType = "mssql", "id UNIQUEIDENTIFIER PRIMARY KEY DEFAULT NEWID(), body NVARCHAR(400)", "NVARCHAR(400)"
	default:
		t.Skipf("read-back INSERT is a postgres/mssql scenario; skipping on %q", rawURL)
	}

	logger := observe.NewLogger("error", "text")
	database, err := db.New(db.Config{
		Engine:          db.EngineSQL,
		DatabaseURL:     rawURL,
		DatabaseMaxOpen: 2,
		DatabaseMaxIdle: 1,
	}, logger)
	if err != nil {
		t.Fatalf("db.New(%q): %v", rawURL, err)
	}
	t.Cleanup(func() { _ = database.Close() })
	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}
	ctx := context.Background()

	t.Run("uuid pk", func(t *testing.T) {
		type UUIDNote struct {
			ID   string `db:"pk"`
			Body string `db:"column:body"`
		}
		const table = "test_nu52_uuid_pk"
		if _, err := sqlDB.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			t.Fatalf("drop: %v", err)
		}
		ddl := fmt.Sprintf(`CREATE TABLE %s (%s)`, table, uuidDDLTail)
		if _, err := sqlDB.ExecContext(ctx, ddl); err != nil {
			t.Fatalf("create table: %v", err)
		}
		t.Cleanup(func() { _, _ = sqlDB.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table) })

		meta, err := ExtractMeta(&UUIDNote{})
		if err != nil {
			t.Fatalf("ExtractMeta: %v", err)
		}
		meta.Table = table
		crud := NewCRUD(sqlDB, meta, nil)
		crud.SetDialect(dialect)

		if err := crud.Create(ctx, &UUIDNote{Body: "hello"}); err != nil {
			t.Fatalf("Create with uuid pk on %s: %v", dialect, err)
		}
		readBack := "SELECT id::text FROM " + table
		if dialect == "mssql" {
			readBack = "SELECT CAST(id AS NVARCHAR(36)) FROM " + table
		}
		var generated string
		if err := sqlDB.QueryRowContext(ctx, readBack).Scan(&generated); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if generated == "" {
			t.Fatal("expected the DB default to generate a uuid")
		}
	})

	t.Run("no pk", func(t *testing.T) {
		type LogLine struct {
			Line string `db:"column:line"`
		}
		const table = "test_nu52_no_pk"
		if _, err := sqlDB.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			t.Fatalf("drop: %v", err)
		}
		if _, err := sqlDB.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (line %s)`, table, noPKColType)); err != nil {
			t.Fatalf("create table: %v", err)
		}
		t.Cleanup(func() { _, _ = sqlDB.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table) })

		meta, err := ExtractMeta(&LogLine{})
		if err != nil {
			t.Fatalf("ExtractMeta: %v", err)
		}
		meta.Table = table
		crud := NewCRUD(sqlDB, meta, nil)
		crud.SetDialect(dialect)

		if err := crud.Create(ctx, &LogLine{Line: "first"}); err != nil {
			t.Fatalf("Create without pk on %s: %v", dialect, err)
		}
		var n int
		if err := sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected 1 row, got %d", n)
		}

		// NU6-2: listing a no-pk model must work too — the default ORDER BY
		// falls back to a real column, not the phantom `id` (on mssql the
		// ORDER BY is structurally required by OFFSET…FETCH, so pre-fix this
		// failed with "Invalid column name 'id'").
		meta.Config = ModelConfig{PageSize: 25}
		res, err := crud.FindAll(ctx, QueryOpts{Page: 1, PageSize: 10})
		if err != nil {
			t.Fatalf("FindAll without pk on %s: %v", dialect, err)
		}
		if items, ok := res.Items.([]LogLine); !ok || len(items) != 1 {
			t.Fatalf("FindAll = %T len %d, want []LogLine len 1", res.Items, len(res.Items.([]LogLine)))
		}
	})

	// NU6-1: a CLIENT-generated uuid must travel in the INSERT. The uuid
	// table here has NO server-side default, which is exactly the setup the
	// NU5-2 test avoided — pre-fix, the pk stayed out of the column list and
	// the engine rejected the row (NOT NULL violation) despite the entity
	// carrying a perfectly good key.
	t.Run("client assigned uuid pk", func(t *testing.T) {
		type ClientNote struct {
			ID   string `db:"pk"`
			Body string `db:"column:body"`
		}
		const table = "test_nu61_client_uuid"
		colType := "UUID"
		if dialect == "mssql" {
			colType = "UNIQUEIDENTIFIER"
		}
		if _, err := sqlDB.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			t.Fatalf("drop: %v", err)
		}
		ddl := fmt.Sprintf(`CREATE TABLE %s (id %s PRIMARY KEY, body %s)`, table, colType, noPKColType)
		if _, err := sqlDB.ExecContext(ctx, ddl); err != nil {
			t.Fatalf("create table: %v", err)
		}
		t.Cleanup(func() { _, _ = sqlDB.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table) })

		meta, err := ExtractMeta(&ClientNote{})
		if err != nil {
			t.Fatalf("ExtractMeta: %v", err)
		}
		meta.Table = table
		crud := NewCRUD(sqlDB, meta, nil)
		crud.SetDialect(dialect)

		const clientKey = "3f2c8a04-9d3e-4f6b-8a34-6f0d2f6c9b11"
		entity := &ClientNote{ID: clientKey, Body: "hello"}
		if err := crud.Create(ctx, entity); err != nil {
			t.Fatalf("Create with client uuid on %s: %v", dialect, err)
		}
		if entity.ID != clientKey {
			t.Fatalf("entity pk overwritten: %q", entity.ID)
		}
		readBack := "SELECT id::text FROM " + table
		if dialect == "mssql" {
			readBack = "SELECT LOWER(CAST(id AS NVARCHAR(36))) FROM " + table
		}
		var stored string
		if err := sqlDB.QueryRowContext(ctx, readBack).Scan(&stored); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if !strings.EqualFold(stored, clientKey) {
			t.Fatalf("stored pk = %q, want %q", stored, clientKey)
		}
	})
}

// TestCRUDLive_InsertEventRowsAffected is the live extension of the
// rows-affected contract pin (which runs on SQLite's exec path only): the
// same logical insert must report rows_affected=1 on every engine, including
// the RETURNING/OUTPUT read-back path where the generic query observer used
// to report 0 (NU5-5: rows=1 on SQLite/MySQL vs rows=0 on PG/MSSQL).
func TestCRUDLive_InsertEventRowsAffected(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_MATRIX_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_MATRIX_URL is not set; skipping live insert-event test")
	}
	const table = "test_nu55_insert_event"
	dialect, ddl, ok := liveMatrixProfile(rawURL, table)
	if !ok {
		t.Skipf("NUCLEUS_SQL_MATRIX_URL=%q is not a live SQL matrix profile", rawURL)
	}

	logger := observe.NewLogger("error", "text")
	database, err := db.New(db.Config{
		Engine:          db.EngineSQL,
		DatabaseURL:     rawURL,
		DatabaseMaxOpen: 2,
		DatabaseMaxIdle: 1,
	}, logger)
	if err != nil {
		t.Fatalf("db.New(%q): %v", rawURL, err)
	}
	t.Cleanup(func() { _ = database.Close() })
	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}
	ctx := context.Background()
	if _, err := sqlDB.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, ddl); err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { _, _ = sqlDB.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table) })

	meta, err := ExtractMeta(&TestUser{})
	if err != nil {
		t.Fatalf("ExtractMeta: %v", err)
	}
	meta.Table = table
	meta.Config = ModelConfig{PageSize: 25}
	crud := NewCRUD(sqlDB, meta, nil)
	crud.SetDialect(dialect)

	var inserts []SQLQueryEvent
	crud.SetSQLQueryObserver(func(_ context.Context, ev SQLQueryEvent) {
		if ev.Operation == "insert" {
			inserts = append(inserts, ev)
		}
	})

	if err := crud.Create(ctx, &TestUser{Email: "rows@nu55.test", Name: "Rows", Role: "user", Active: true}); err != nil {
		t.Fatalf("Create on %s: %v", dialect, err)
	}
	if len(inserts) != 1 {
		t.Fatalf("expected 1 insert event, got %d", len(inserts))
	}
	if got := inserts[0].RowsAffected; got != 1 {
		t.Fatalf("insert RowsAffected on %s = %d, want 1", dialect, got)
	}
	if inserts[0].Error != nil {
		t.Fatalf("insert event carries error: %v", inserts[0].Error)
	}
}
