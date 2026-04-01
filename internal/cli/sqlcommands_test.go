package cli

import (
	"strings"
	"testing"
)

func TestResolveMigrationRef(t *testing.T) {
	migs := []migrationPair{
		{ID: "20260401120000_create_users"},
		{ID: "20260401123000_add_posts"},
	}

	got, err := resolveMigrationRef("20260401120000_create_users", migs)
	if err != nil {
		t.Fatalf("resolve exact migration failed: %v", err)
	}
	if got.ID != "20260401120000_create_users" {
		t.Fatalf("unexpected exact match id: %s", got.ID)
	}

	got, err = resolveMigrationRef("add_posts", migs)
	if err != nil {
		t.Fatalf("resolve suffix migration failed: %v", err)
	}
	if got.ID != "20260401123000_add_posts" {
		t.Fatalf("unexpected suffix match id: %s", got.ID)
	}
}

func TestResolveMigrationRefAmbiguous(t *testing.T) {
	migs := []migrationPair{
		{ID: "20260401120000_create_users"},
		{ID: "20260401123000_create_users_index"},
	}
	_, err := resolveMigrationRef("create_users", migs)
	if err == nil {
		t.Fatal("expected ambiguous migration reference error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("unexpected ambiguous error: %v", err)
	}
}

func TestBuildFlushStatementsSQLite(t *testing.T) {
	stmts := buildFlushStatements(dbFlavorSQLite, []string{"users", "posts"})
	rendered := renderSQLStatements(stmts)

	if !strings.Contains(rendered, "DELETE FROM \"users\";") {
		t.Fatalf("expected users delete statement, got: %s", rendered)
	}
	if !strings.Contains(rendered, "DELETE FROM sqlite_sequence WHERE name = 'posts';") {
		t.Fatalf("expected posts sequence reset statement, got: %s", rendered)
	}
}

func TestBuildSequenceResetStatementsPostgres(t *testing.T) {
	stmts := buildSequenceResetStatements(dbFlavorPostgres, []string{"users"})
	rendered := renderSQLStatements(stmts)
	if !strings.Contains(rendered, "pg_get_serial_sequence('users', 'id')") {
		t.Fatalf("expected postgres sequence reset statement, got: %s", rendered)
	}
}

func TestNormalizeTableList(t *testing.T) {
	tables := normalizeTableList([]string{" users ", "users", "goframe_schema_migrations", "posts"})
	got := strings.Join(tables, ",")
	if got != "posts,users" {
		t.Fatalf("unexpected normalized tables: %s", got)
	}
}
