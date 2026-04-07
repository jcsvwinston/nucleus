package cli

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestBuildCreateCacheTableStatementsSQLite(t *testing.T) {
	stmts, err := buildCreateCacheTableStatements(dbFlavorSQLite, "goframe_cache_entries")
	if err != nil {
		t.Fatalf("buildCreateCacheTableStatements failed: %v", err)
	}
	sqlText := strings.Join(stmts, "\n")
	if !strings.Contains(sqlText, `CREATE TABLE IF NOT EXISTS "goframe_cache_entries"`) {
		t.Fatalf("unexpected create table SQL: %s", sqlText)
	}
	if !strings.Contains(sqlText, `CREATE INDEX IF NOT EXISTS "goframe_cache_entries_expires_idx"`) {
		t.Fatalf("expected expires index statement, got: %s", sqlText)
	}
}

func TestBuildCreateCacheTableStatementsMSSQL(t *testing.T) {
	stmts, err := buildCreateCacheTableStatements(dbFlavorMSSQL, "goframe_cache_entries")
	if err != nil {
		t.Fatalf("buildCreateCacheTableStatements failed: %v", err)
	}
	sqlText := strings.Join(stmts, "\n")
	if !strings.Contains(sqlText, "IF OBJECT_ID(N'goframe_cache_entries', N'U') IS NULL CREATE TABLE [goframe_cache_entries]") {
		t.Fatalf("unexpected mssql create table SQL: %s", sqlText)
	}
	if !strings.Contains(sqlText, "CREATE INDEX [goframe_cache_entries_expires_idx] ON [goframe_cache_entries] ([expires_at])") {
		t.Fatalf("expected mssql expires index statement, got: %s", sqlText)
	}
}

func TestBuildCreateCacheTableStatementsOracle(t *testing.T) {
	stmts, err := buildCreateCacheTableStatements(dbFlavorOracle, "goframe_cache_entries")
	if err != nil {
		t.Fatalf("buildCreateCacheTableStatements failed: %v", err)
	}
	sqlText := strings.Join(stmts, "\n")
	if !strings.Contains(sqlText, `EXECUTE IMMEDIATE 'CREATE TABLE "GOFRAME_CACHE_ENTRIES"`) {
		t.Fatalf("unexpected oracle create table SQL: %s", sqlText)
	}
	if !strings.Contains(sqlText, `EXECUTE IMMEDIATE 'CREATE INDEX "GOFRAME_CACHE_ENTRIES_EXPIRES_IDX" ON "GOFRAME_CACHE_ENTRIES" ("EXPIRES_AT")'`) {
		t.Fatalf("expected oracle expires index statement, got: %s", sqlText)
	}
	if !strings.Contains(sqlText, "SQLCODE != -955") {
		t.Fatalf("expected oracle idempotent guard, got: %s", sqlText)
	}
}

func TestBuildCreateCacheTableStatementsUnsupported(t *testing.T) {
	_, err := buildCreateCacheTableStatements(dbFlavorUnknown, "cache_entries")
	if err == nil {
		t.Fatal("expected error for unsupported database engine")
	}
}

func TestSelectSessionExpiryColumn(t *testing.T) {
	col := selectSessionExpiryColumn([]string{"id", "data", "expires_at"})
	if col != "expires_at" {
		t.Fatalf("unexpected expiry column: %q", col)
	}

	col = selectSessionExpiryColumn([]string{"id", "data", "expiry"})
	if col != "expiry" {
		t.Fatalf("unexpected expiry fallback column: %q", col)
	}
}

func TestBuildClearSessionsStatementAll(t *testing.T) {
	stmt, mode, err := buildClearSessionsStatement(nil, dbFlavorSQLite, "goframe_sessions", true)
	if err != nil {
		t.Fatalf("buildClearSessionsStatement(all) failed: %v", err)
	}
	if mode != "all" {
		t.Fatalf("unexpected mode: %s", mode)
	}
	if stmt != `DELETE FROM "goframe_sessions"` {
		t.Fatalf("unexpected delete statement: %s", stmt)
	}
}

func TestBuildClearSessionsStatementExpiredSQLite(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	if _, err := dbConn.Exec(`CREATE TABLE goframe_sessions (id TEXT PRIMARY KEY, expires_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create sessions table failed: %v", err)
	}

	stmt, mode, err := buildClearSessionsStatement(dbConn, dbFlavorSQLite, "goframe_sessions", false)
	if err != nil {
		t.Fatalf("buildClearSessionsStatement(expired) failed: %v", err)
	}
	if mode != "expired" {
		t.Fatalf("unexpected mode: %s", mode)
	}
	if !strings.Contains(stmt, `"expires_at" <= CURRENT_TIMESTAMP`) {
		t.Fatalf("unexpected expiration predicate: %s", stmt)
	}
}

func TestBuildClearSessionsStatementMissingExpiryColumn(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	if _, err := dbConn.Exec(`CREATE TABLE goframe_sessions (id TEXT PRIMARY KEY, payload TEXT NOT NULL)`); err != nil {
		t.Fatalf("create sessions table failed: %v", err)
	}

	if _, _, err := buildClearSessionsStatement(dbConn, dbFlavorSQLite, "goframe_sessions", false); err == nil {
		t.Fatal("expected error when expiry column is missing")
	}
}
