package model

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// TestCRUDLive_PlaceholderPortability is the F-3 (ADR-013) regression guard. It
// drives the full CRUD surface against a live required-matrix engine
// (PostgreSQL or MySQL) so the SQLite-only blind spot that hid the `?`
// placeholder bug cannot recur. On pre-fix code the first INSERT against
// PostgreSQL fails with `syntax error at or near "?"`; post-fix every statement
// is rebound to the engine's native placeholder.
//
// Gated on NUCLEUS_SQL_MATRIX_URL (the db-matrix-required CI lane); skipped
// locally when unset. Never mocks the database (CLAUDE.md §7).
func TestCRUDLive_PlaceholderPortability(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_MATRIX_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_MATRIX_URL is not set; skipping live CRUD portability test")
	}

	const table = "test_users_f3_rebind"
	dialect, ddl, ok := liveMatrixProfile(rawURL, table)
	if !ok {
		t.Skipf("NUCLEUS_SQL_MATRIX_URL=%q is not a required SQL matrix profile (postgres/mysql)", rawURL)
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
		t.Fatalf("drop pre-existing table: %v", err)
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

	// Create — exercises INSERT INTO ... VALUES (?, ?, ...) → rebound.
	for i, email := range []string{"alice@f3.test", "bob@f3.test"} {
		u := &TestUser{Email: email, Name: fmt.Sprintf("User %d", i), Role: "user", Active: true}
		if err := crud.Create(ctx, u); err != nil {
			t.Fatalf("Create(%s) on %s: %v", email, dialect, err)
		}
	}

	// List — exercises SELECT ... ORDER BY ... LIMIT ? OFFSET ? and the
	// dialect-specific getEstimate count query.
	res, err := crud.FindAll(ctx, QueryOpts{Page: 1, PageSize: 10, OrderBy: "email asc"})
	if err != nil {
		t.Fatalf("FindAll on %s: %v", dialect, err)
	}
	items, ok := res.Items.([]TestUser)
	if !ok {
		t.Fatalf("FindAll Items type = %T, want []TestUser", res.Items)
	}
	if len(items) != 2 {
		t.Fatalf("FindAll returned %d rows, want 2", len(items))
	}
	id := items[0].ID
	if id == 0 {
		t.Fatal("listed row has a zero ID; cannot exercise by-id paths")
	}

	// FindByID — exercises SELECT ... WHERE id = ? → rebound.
	got, err := crud.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID(%d) on %s: %v", id, dialect, err)
	}
	if got == nil {
		t.Fatalf("FindByID(%d) returned nil", id)
	}

	// Update — exercises UPDATE ... SET ... WHERE id = ? → rebound.
	if err := crud.Update(ctx, id, map[string]interface{}{"name": "Renamed"}); err != nil {
		t.Fatalf("Update(%d) on %s: %v", id, dialect, err)
	}
	after, err := crud.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID after update on %s: %v", dialect, err)
	}
	if name := entityStringField(after, "Name"); name != "Renamed" {
		t.Fatalf("Update did not persist: Name=%q, want %q", name, "Renamed")
	}

	// Delete — exercises the soft-delete UPDATE ... WHERE id = ? → rebound.
	if err := crud.Delete(ctx, id); err != nil {
		t.Fatalf("Delete(%d) on %s: %v", id, dialect, err)
	}

	// Post-delete: the soft-deleted row must no longer be listed.
	res2, err := crud.FindAll(ctx, QueryOpts{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("FindAll after delete on %s: %v", dialect, err)
	}
	items2, ok := res2.Items.([]TestUser)
	if !ok {
		t.Fatalf("FindAll Items type = %T, want []TestUser", res2.Items)
	}
	for _, it := range items2 {
		if it.ID == id {
			t.Fatalf("soft-deleted row id=%d still listed after Delete", id)
		}
	}
}

// liveMatrixProfile returns the CRUD dialect and a CREATE TABLE statement for
// the required-matrix engine behind rawURL. The dialect string mirrors
// app.detectDatabaseDialect ("postgres"/"mysql"), which is what the runtime
// feeds CRUD.SetDialect — deliberately NOT db.System()'s "postgresql"/"mssql".
func liveMatrixProfile(rawURL, table string) (dialect, ddl string, ok bool) {
	lower := strings.ToLower(rawURL)
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		return "postgres", fmt.Sprintf(`CREATE TABLE %s (
			id BIGSERIAL PRIMARY KEY,
			created_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ,
			deleted_at TIMESTAMPTZ,
			email TEXT NOT NULL,
			name TEXT NOT NULL,
			role TEXT,
			active BOOLEAN NOT NULL DEFAULT false
		)`, table), true
	case strings.HasPrefix(lower, "mysql://"):
		return "mysql", fmt.Sprintf(`CREATE TABLE %s (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			created_at DATETIME NULL,
			updated_at DATETIME NULL,
			deleted_at DATETIME NULL,
			email VARCHAR(255) NOT NULL,
			name VARCHAR(255) NOT NULL,
			role VARCHAR(255),
			active TINYINT(1) NOT NULL DEFAULT 0
		)`, table), true
	default:
		return "", "", false
	}
}

// entityStringField reads a string field from a model entity returned as either
// a pointer or a value (FindByID returns *T; FindAll yields T).
func entityStringField(entity interface{}, field string) string {
	v := reflect.ValueOf(entity)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	f := v.FieldByName(field)
	if !f.IsValid() || f.Kind() != reflect.String {
		return ""
	}
	return f.String()
}
