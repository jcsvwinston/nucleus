package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// TestCRUDLive_OracleCRUD drives the CRUD list/get/update/soft-delete surface
// against a live Oracle. The `oracle://` scheme is declared stable in the
// contract inventory, yet until NU8-1 both FindAll and FindByID emitted a
// trailing `LIMIT` clause on Oracle — invalid SQL (ORA-00933) on every list
// and every by-id read. The db-matrix-live-oracle lane ran connect/migrate
// smoke tests but no CRUD test, so the branch shipped without ever executing;
// this test is the lane's teeth for the OFFSET … FETCH / FETCH FIRST grammar.
//
// Rows are created with caller-assigned primary keys: Oracle's documented
// Create limitation is that the generated-key back-fill is not implemented
// (go-ora needs `RETURNING … INTO` with an output binding — see
// insertReturningClause), and this test exercises the read paths, not that
// gap. Gated on NUCLEUS_SQL_MATRIX_URL with an oracle:// scheme; requires
// `-tags oracle` for the driver. Never mocks the database (CLAUDE.md §7).
func TestCRUDLive_OracleCRUD(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_MATRIX_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_MATRIX_URL is not set; skipping live Oracle CRUD test")
	}
	if !strings.HasPrefix(strings.ToLower(rawURL), "oracle://") {
		t.Skipf("NUCLEUS_SQL_MATRIX_URL=%q is not an oracle:// profile", rawURL)
	}

	const table = "test_users_nu81_oracle"

	logger := observe.NewLogger("error", "text")
	database, err := db.New(db.Config{
		Engine:          db.EngineSQL,
		DatabaseURL:     rawURL,
		DatabaseMaxOpen: 2,
		DatabaseMaxIdle: 1,
	}, logger)
	if err != nil {
		t.Fatalf("db.New(%q): %v (is the build running with -tags oracle?)", rawURL, err)
	}
	t.Cleanup(func() { _ = database.Close() })

	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}

	ctx := context.Background()
	// Oracle 23 supports DROP TABLE IF EXISTS (the image behind the CI lane
	// is gvenzl/oracle-free:23).
	if _, err := sqlDB.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
		t.Fatalf("drop pre-existing table: %v", err)
	}
	ddl := fmt.Sprintf(`CREATE TABLE %s (
		id NUMBER(19) PRIMARY KEY,
		created_at TIMESTAMP,
		updated_at TIMESTAMP,
		deleted_at TIMESTAMP,
		email VARCHAR2(255) NOT NULL,
		name VARCHAR2(255) NOT NULL,
		role VARCHAR2(255),
		active NUMBER(1) NOT NULL
	)`, table)
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
	crud.SetDialect("oracle")

	// Create — caller-assigned keys travel in the INSERT (pkAssigned path),
	// matching the documented Oracle limitation on generated-key back-fill.
	for i, email := range []string{"alice@nu81.test", "bob@nu81.test", "carol@nu81.test"} {
		u := &TestUser{
			BaseModel: BaseModel{ID: uint(i + 1)},
			Email:     email,
			Name:      fmt.Sprintf("User %d", i+1),
			Role:      "user",
			Active:    true,
		}
		if err := crud.Create(ctx, u); err != nil {
			t.Fatalf("Create(%s) on oracle: %v", email, err)
		}
	}

	// List — pre-NU8-1 this emitted `ORDER BY … LIMIT :1 OFFSET :2`, which
	// Oracle rejects with ORA-00933; the fixed grammar is
	// `OFFSET :1 ROWS FETCH NEXT :2 ROWS ONLY`.
	res, err := crud.FindAll(ctx, QueryOpts{Page: 1, PageSize: 10, OrderBy: "email asc"})
	if err != nil {
		t.Fatalf("FindAll on oracle: %v", err)
	}
	items, ok := res.Items.([]TestUser)
	if !ok {
		t.Fatalf("FindAll Items type = %T, want []TestUser", res.Items)
	}
	if len(items) != 3 {
		t.Fatalf("FindAll returned %d rows, want 3", len(items))
	}
	if items[0].Email != "alice@nu81.test" {
		t.Fatalf("ORDER BY email asc: first row is %q, want alice@nu81.test", items[0].Email)
	}

	// Pagination — the OFFSET/FETCH argument order (offset first) must hold:
	// page 2 of size 2 skips exactly two rows and detects no further page.
	page2, err := crud.FindAll(ctx, QueryOpts{Page: 2, PageSize: 2, OrderBy: "email asc"})
	if err != nil {
		t.Fatalf("FindAll page 2 on oracle: %v", err)
	}
	page2Items, ok := page2.Items.([]TestUser)
	if !ok {
		t.Fatalf("FindAll page 2 Items type = %T, want []TestUser", page2.Items)
	}
	if len(page2Items) != 1 || page2Items[0].Email != "carol@nu81.test" {
		t.Fatalf("page 2 of size 2: got %d rows (first=%v), want exactly carol@nu81.test",
			len(page2Items), page2Items)
	}
	if page2.HasMore {
		t.Fatal("page 2 of 3 rows with size 2 must report HasMore=false")
	}

	// FindByID — pre-NU8-1 this emitted a trailing `LIMIT 1` (ORA-00933);
	// the fixed grammar is `FETCH FIRST 1 ROWS ONLY`.
	id := items[0].ID
	got, err := crud.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID(%d) on oracle: %v", id, err)
	}
	if name := entityStringField(got, "Email"); name != "alice@nu81.test" {
		t.Fatalf("FindByID(%d) returned %q, want alice@nu81.test", id, name)
	}

	// Update — UPDATE … WHERE id = :n, rebound for oracle.
	if err := crud.Update(ctx, id, map[string]interface{}{"name": "Renamed"}); err != nil {
		t.Fatalf("Update(%d) on oracle: %v", id, err)
	}
	after, err := crud.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID after update on oracle: %v", err)
	}
	if name := entityStringField(after, "Name"); name != "Renamed" {
		t.Fatalf("Update did not persist: Name=%q, want %q", name, "Renamed")
	}

	// Soft delete, then both read paths must stop returning the row.
	if err := crud.Delete(ctx, id); err != nil {
		t.Fatalf("Delete(%d) on oracle: %v", id, err)
	}
	if _, err := crud.FindByID(ctx, id); err == nil {
		t.Fatalf("FindByID(%d) after soft delete must fail with not-found", id)
	} else {
		var domainErr *gferrors.DomainError
		if !errors.As(err, &domainErr) || domainErr.StatusCode != 404 {
			t.Fatalf("FindByID(%d) after soft delete: got %v, want a 404 not-found", id, err)
		}
	}
	res2, err := crud.FindAll(ctx, QueryOpts{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("FindAll after delete on oracle: %v", err)
	}
	items2, ok := res2.Items.([]TestUser)
	if !ok {
		t.Fatalf("FindAll Items type = %T, want []TestUser", res2.Items)
	}
	if len(items2) != 2 {
		t.Fatalf("after soft delete: FindAll returned %d rows, want 2", len(items2))
	}
	for _, it := range items2 {
		if it.ID == id {
			t.Fatalf("soft-deleted row id=%d still listed after Delete", id)
		}
	}
}
