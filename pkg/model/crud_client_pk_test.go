package model

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// NU6-1: a caller-assigned primary key must travel in the INSERT. The old
// loop skipped f.IsPK unconditionally, so the database never received the
// key — SQLite inserted a NULL-pk row without error (silent corruption) and
// client-generated UUIDs were impossible through CRUD.
func TestInsertColumnsAndArgs_PKShapes(t *testing.T) {
	type IntPK struct {
		ID   int64  `db:"pk"`
		Body string `db:"column:body"`
	}
	type UUIDPK struct {
		ID   string `db:"pk"`
		Body string `db:"column:body"`
	}

	cases := []struct {
		name         string
		entity       interface{}
		wantPKInCols bool
		wantAssigned bool
	}{
		{"integer pk zero stays out (DB generates)", &IntPK{Body: "b"}, false, false},
		{"integer pk pre-assigned travels", &IntPK{ID: 42, Body: "b"}, true, true},
		{"string pk empty stays out (DB default)", &UUIDPK{Body: "b"}, false, false},
		{"string pk pre-assigned travels", &UUIDPK{ID: "u-1", Body: "b"}, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta, err := ExtractMeta(tc.entity)
			if err != nil {
				t.Fatalf("ExtractMeta: %v", err)
			}
			crud := NewCRUD(nil, meta, nil)
			cols, args, assigned := crud.insertColumnsAndArgs(tc.entity)
			hasPK := strings.Contains(","+strings.Join(cols, ",")+",", ",id,")
			if hasPK != tc.wantPKInCols {
				t.Fatalf("pk in columns = %v, want %v (cols=%v)", hasPK, tc.wantPKInCols, cols)
			}
			if assigned != tc.wantAssigned {
				t.Fatalf("pkAssigned = %v, want %v", assigned, tc.wantAssigned)
			}
			if len(cols) != len(args) {
				t.Fatalf("cols/args mismatch: %d vs %d", len(cols), len(args))
			}
		})
	}
}

// The pin of the silent-NULL corruption, on the engine that exhibited it:
// pre-fix, Create on SQLite with a client-assigned string PK inserted a row
// whose pk was NULL (no error, no key). Post-fix the row carries the key and
// the entity keeps it (no backfill overwrite).
func TestCRUD_ClientAssignedPK_SQLite(t *testing.T) {
	type ClientKeyNote struct {
		ID   string `db:"pk"`
		Body string `db:"column:body"`
	}

	sqlDB := setupTestDB(t)
	ctx := context.Background()
	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE client_key_notes (id TEXT PRIMARY KEY, body TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	meta, err := ExtractMeta(&ClientKeyNote{})
	if err != nil {
		t.Fatalf("ExtractMeta: %v", err)
	}
	meta.Table = "client_key_notes"
	crud := NewCRUD(sqlDB, meta, nil)
	crud.SetDialect("sqlite")

	entity := &ClientKeyNote{ID: "uuid-client-1", Body: "hello"}
	if err := crud.Create(ctx, entity); err != nil {
		t.Fatalf("Create with client-assigned pk: %v", err)
	}
	if entity.ID != "uuid-client-1" {
		t.Fatalf("entity pk overwritten: %q", entity.ID)
	}

	var gotID *string
	var body string
	if err := sqlDB.QueryRowContext(ctx, `SELECT id, body FROM client_key_notes`).Scan(&gotID, &body); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if gotID == nil {
		t.Fatal("row inserted with NULL primary key — the silent corruption NU6-1 pins")
	}
	if *gotID != "uuid-client-1" || body != "hello" {
		t.Fatalf("row = (%v, %q), want (uuid-client-1, hello)", *gotID, body)
	}
}

// An explicitly pre-assigned INTEGER key is respected end to end: it travels
// in the INSERT, the engine stores it, and the backfill does not overwrite it
// with the driver-reported value.
func TestCRUD_PreAssignedIntegerPK_SQLite(t *testing.T) {
	type NumberedDoc struct {
		ID    int64  `db:"pk"`
		Title string `db:"column:title"`
	}

	sqlDB := setupTestDB(t)
	ctx := context.Background()
	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE numbered_docs (id INTEGER PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	meta, err := ExtractMeta(&NumberedDoc{})
	if err != nil {
		t.Fatalf("ExtractMeta: %v", err)
	}
	meta.Table = "numbered_docs"
	crud := NewCRUD(sqlDB, meta, nil)
	crud.SetDialect("sqlite")

	entity := &NumberedDoc{ID: 42, Title: "answer"}
	if err := crud.Create(ctx, entity); err != nil {
		t.Fatalf("Create with pre-assigned int pk: %v", err)
	}
	if entity.ID != 42 {
		t.Fatalf("entity pk changed to %d, want 42", entity.ID)
	}
	var got int64
	if err := sqlDB.QueryRowContext(ctx, `SELECT id FROM numbered_docs WHERE title = 'answer'`).Scan(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != 42 {
		t.Fatalf("stored pk = %d, want 42", got)
	}

	// Zero-value pk still takes the generated path on this engine.
	auto := &NumberedDoc{Title: "auto"}
	if err := crud.Create(ctx, auto); err != nil {
		t.Fatalf("Create with zero pk: %v", err)
	}
	if auto.ID == 0 || auto.ID == 42 {
		t.Fatalf("generated pk = %d, want a fresh non-42 key", auto.ID)
	}
}

// NU6-2: the by-id operations must refuse models without a primary key
// instead of guessing a phantom `id` column, and FindAll must order by a
// real column.
func TestCRUD_NoPKModel(t *testing.T) {
	type LogLine struct {
		Line string `db:"column:line"`
		N    int64  `db:"column:n"`
	}

	sqlDB := setupTestDB(t)
	ctx := context.Background()
	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE log_lines (line TEXT, n INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	meta, err := ExtractMeta(&LogLine{})
	if err != nil {
		t.Fatalf("ExtractMeta: %v", err)
	}
	meta.Table = "log_lines"
	meta.Config = ModelConfig{PageSize: 25}
	crud := NewCRUD(sqlDB, meta, nil)
	crud.SetDialect("sqlite")

	for i, l := range []string{"first", "second"} {
		if err := crud.Create(ctx, &LogLine{Line: l, N: int64(i)}); err != nil {
			t.Fatalf("Create(%s): %v", l, err)
		}
	}

	// FindAll works: default ORDER BY falls back to the first real column,
	// not the phantom `id` (which this table does not have).
	res, err := crud.FindAll(ctx, QueryOpts{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("FindAll on no-pk model: %v", err)
	}
	items, ok := res.Items.([]LogLine)
	if !ok || len(items) != 2 {
		t.Fatalf("FindAll = %T len %d, want []LogLine len 2", res.Items, len(items))
	}

	// By-id operations refuse honestly.
	if _, err := crud.FindByID(ctx, 1); !errors.Is(err, ErrNoPrimaryKey) {
		t.Fatalf("FindByID err = %v, want ErrNoPrimaryKey", err)
	}
	if err := crud.Update(ctx, 1, map[string]interface{}{"line": "x"}); !errors.Is(err, ErrNoPrimaryKey) {
		t.Fatalf("Update err = %v, want ErrNoPrimaryKey", err)
	}
	if err := crud.Delete(ctx, 1); !errors.Is(err, ErrNoPrimaryKey) {
		t.Fatalf("Delete err = %v, want ErrNoPrimaryKey", err)
	}
}
