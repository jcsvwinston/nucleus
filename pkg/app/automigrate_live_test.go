package app

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// liveMigrateUser is the canonical model the live-DB AutoMigrate tests
// run against. It exercises the four interesting code paths in the
// dialect scaffolders: auto-increment integer PK, NOT NULL string, a
// nullable string, and a time.Time column. The table name is fixed —
// CI containers are ephemeral so cross-run collisions can't happen;
// the cleanup phase drops the table defensively at the end of each
// run so a flaky teardown still leaves the next invocation clean.
type liveMigrateUser struct {
	ID uint `db:"pk"`
	// Email carries a secondary index so the scaffold emits a CREATE INDEX in
	// addition to the CREATE TABLE — a multi-statement migration. On Oracle
	// that is two `/`-separated PL/SQL blocks, which exercises db.ExecScript's
	// statement splitting (go-ora runs only one block per Exec).
	Email     string    `db:"required;index"`
	Nickname  string    // nullable
	CreatedAt time.Time `db:"required"`
}

// TableName overrides the convention-derived table name so all four
// matrix engines see the same identifier (which simplifies the
// information_schema introspection in introspectColumns) and the name
// is unmistakably owned by this test.
func (liveMigrateUser) TableName() string { return "ci_automig_live_users" }

// TestSQLMatrix_AutoMigrate exercises App.AutoMigrate end-to-end against
// the live Postgres or MySQL container brought up by the
// db-matrix-required CI lane. It asserts the resulting table is
// observable through the dialect-appropriate introspection query
// (information_schema), so the test catches a scaffold that "looks
// right" but produces SQL the engine actually rejects or interprets
// differently. Closes the gap flagged by audit
// 2026-05-14-post-sprint-readiness §5 risk 4 / §7 task 7.
//
// Skipped when NUCLEUS_SQL_MATRIX_URL is unset (local fast lane) or
// points at a non-required profile.
func TestSQLMatrix_AutoMigrate(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_MATRIX_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_MATRIX_URL is not set; skipping SQL matrix AutoMigrate test")
	}

	lower := strings.ToLower(rawURL)
	isPG := strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://")
	isMySQL := strings.HasPrefix(lower, "mysql://")
	if !isPG && !isMySQL {
		t.Skipf("NUCLEUS_SQL_MATRIX_URL=%q is not a required SQL matrix profile", rawURL)
	}

	runAutoMigrateLive(t, rawURL)
}

// TestSQLMatrix_AutoMigrate_Exploratory is the MSSQL/Oracle counterpart
// to TestSQLMatrix_AutoMigrate. The exploratory lanes carry the mssql
// and oracle build tags respectively; the test is compiled into the
// binary at all times but skips when the relevant URL is missing.
func TestSQLMatrix_AutoMigrate_Exploratory(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_EXPLORATORY_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_EXPLORATORY_URL is not set; skipping exploratory AutoMigrate test")
	}

	lower := strings.ToLower(rawURL)
	isMSSQL := strings.HasPrefix(lower, "sqlserver://") || strings.HasPrefix(lower, "mssql://")
	isOracle := strings.HasPrefix(lower, "oracle://")
	if !isMSSQL && !isOracle {
		t.Skipf("NUCLEUS_SQL_EXPLORATORY_URL=%q is not an MSSQL/Oracle exploratory profile", rawURL)
	}

	runAutoMigrateLive(t, rawURL)
}

// runAutoMigrateLive is the shared body for the required and
// exploratory live AutoMigrate tests. It brings up an app pointed at
// the supplied database URL, runs AutoMigrate, and then introspects
// the resulting schema with the dialect-appropriate query.
func runAutoMigrateLive(t *testing.T, rawURL string) {
	t.Helper()

	cfg := testAppConfig()
	cfg.Databases["default"] = DatabaseConfig{
		URL:         rawURL,
		MaxOpen:     2,
		MaxIdle:     2,
		MaxLifetime: time.Minute,
	}

	a, err := New(cfg, WithoutDefaults())
	if err != nil {
		t.Fatalf("app.New failed for live URL %q: %v", rawURL, err)
	}
	defer a.Shutdown(context.Background())

	sqlDB, err := a.DB.SqlDB()
	if err != nil {
		t.Fatalf("DB.SqlDB failed: %v", err)
	}
	system := a.DB.System()
	tableName := liveMigrateUser{}.TableName()

	// Pre-clean any leftovers from a previous flaky run and register a
	// post-test cleanup so this run leaves a tidy state.
	dropTable(sqlDB, system, tableName)
	t.Cleanup(func() { dropTable(sqlDB, system, tableName) })

	if err := a.AutoMigrate(&liveMigrateUser{}); err != nil {
		t.Fatalf("AutoMigrate failed against live %s URL %q: %v", system, rawURL, err)
	}

	cols, err := introspectColumns(context.Background(), sqlDB, system, tableName)
	if err != nil {
		t.Fatalf("introspect columns for %q: %v", tableName, err)
	}
	if len(cols) == 0 {
		t.Fatalf("AutoMigrate ran without error but no columns visible in %s information_schema for table %q", system, tableName)
	}

	colNames := make([]string, 0, len(cols))
	notNull := make(map[string]bool, len(cols))
	for _, c := range cols {
		colNames = append(colNames, c.name)
		notNull[c.name] = c.notNull
	}
	sort.Strings(colNames)
	wantCols := []string{"created_at", "email", "id", "nickname"}
	if !equalStringSlices(colNames, wantCols) {
		t.Fatalf("unexpected columns for AutoMigrate-created table %q: got %v, want %v", tableName, colNames, wantCols)
	}

	// `id` is the PK (always NOT NULL by definition), `email` and
	// `created_at` are `db:"required"`, so all three must be NOT NULL.
	// `nickname` is the only nullable column.
	for _, required := range []string{"id", "email", "created_at"} {
		if !notNull[required] {
			t.Fatalf("column %q should be NOT NULL after AutoMigrate, got nullable", required)
		}
	}
	if notNull["nickname"] {
		t.Fatalf("column %q should be nullable after AutoMigrate, got NOT NULL", "nickname")
	}

	// AutoMigrate is idempotent on the supported engines (CREATE TABLE
	// IF NOT EXISTS for PG/MySQL/SQLite, dialect-specific guards for
	// MSSQL/Oracle). A second call must not error.
	if err := a.AutoMigrate(&liveMigrateUser{}); err != nil {
		t.Fatalf("AutoMigrate is supposed to be idempotent; second call failed: %v", err)
	}
}

// columnInfo is the minimal projection of information_schema columns
// the live-DB AutoMigrate test needs to verify what was created.
type columnInfo struct {
	name    string
	notNull bool
}

// introspectColumns runs a dialect-specific information_schema query
// and returns the columns of the given table. The matrix-required
// lane covers Postgres and MySQL; the exploratory lane extends to
// MSSQL and Oracle.
func introspectColumns(ctx context.Context, db *sql.DB, system, table string) ([]columnInfo, error) {
	var (
		query string
		args  []any
	)
	switch system {
	case "postgresql":
		query = `SELECT column_name, is_nullable
		         FROM information_schema.columns
		         WHERE table_schema = current_schema() AND table_name = $1
		         ORDER BY ordinal_position`
		args = []any{table}
	case "mysql":
		query = `SELECT COLUMN_NAME, IS_NULLABLE
		         FROM information_schema.COLUMNS
		         WHERE table_schema = DATABASE() AND table_name = ?
		         ORDER BY ORDINAL_POSITION`
		args = []any{table}
	case "mssql":
		query = `SELECT COLUMN_NAME, IS_NULLABLE
		         FROM INFORMATION_SCHEMA.COLUMNS
		         WHERE TABLE_NAME = @p1
		         ORDER BY ORDINAL_POSITION`
		args = []any{table}
	case "oracle":
		// Oracle folds unquoted identifiers to UPPER CASE — match
		// accordingly. NULLABLE in USER_TAB_COLUMNS is 'Y'/'N', not
		// 'YES'/'NO'; the caller normalises both forms.
		query = `SELECT COLUMN_NAME, NULLABLE
		         FROM USER_TAB_COLUMNS
		         WHERE TABLE_NAME = :1
		         ORDER BY COLUMN_ID`
		args = []any{strings.ToUpper(table)}
	case "sqlite":
		// SQLite uses PRAGMA, not information_schema; included for
		// future SQLite live-DB coverage even though the matrix lanes
		// do not exercise it today. The table name is passed as a
		// bound parameter (mirroring the production introspection in
		// pkg/db/schema_drift.go) — keeping the form correct so a
		// future copy-paste cannot accidentally introduce an injection
		// path.
		query = "SELECT name, CASE \"notnull\" WHEN 0 THEN 'YES' ELSE 'NO' END FROM pragma_table_info(?)"
		args = []any{table}
	default:
		return nil, fmt.Errorf("introspectColumns: unsupported system %q", system)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("introspect %s columns for %q: %w", system, table, err)
	}
	defer rows.Close()

	cols := make([]columnInfo, 0, 8)
	for rows.Next() {
		var (
			name     string
			nullable string
		)
		if err := rows.Scan(&name, &nullable); err != nil {
			return nil, fmt.Errorf("scan column row: %w", err)
		}
		cols = append(cols, columnInfo{
			name:    strings.ToLower(name),
			notNull: isNotNullMarker(nullable),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read columns: %w", err)
	}
	return cols, nil
}

// isNotNullMarker accepts the cross-dialect ways a NOT NULL column is
// reported by an introspection query: 'NO' (PG, MySQL, MSSQL, SQLite
// via the CASE we emit), 'N' (Oracle).
func isNotNullMarker(s string) bool {
	t := strings.TrimSpace(s)
	return strings.EqualFold(t, "NO") || strings.EqualFold(t, "N")
}

// dropTable performs best-effort DROP TABLE IF EXISTS in the
// dialect-appropriate form. Used both for pre-test cleanup (in case a
// previous run aborted with the table in place) and post-test
// cleanup. Errors are swallowed — the test asserts via introspection,
// not via the DROP result.
func dropTable(db *sql.DB, system, table string) {
	switch system {
	case "mssql":
		_, _ = db.Exec(fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s", table, "["+table+"]"))
	case "oracle":
		// Wrap in PL/SQL to swallow ORA-00942 (table doesn't exist).
		_, _ = db.Exec(fmt.Sprintf(`BEGIN
			EXECUTE IMMEDIATE 'DROP TABLE "%s"';
		EXCEPTION
			WHEN OTHERS THEN
				IF SQLCODE != -942 THEN RAISE; END IF;
		END;`, strings.ToUpper(table)))
	default:
		_, _ = db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteForDialect(system, table)))
	}
}

// quoteForDialect quotes a table identifier for ad-hoc DDL in the
// teardown path. The scaffold-side quoting (in pkg/model) is the
// authoritative writer; this helper just needs to round-trip the
// table name we created.
func quoteForDialect(system, name string) string {
	switch system {
	case "mysql":
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	case "mssql":
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	case "oracle":
		return `"` + strings.ToUpper(name) + `"`
	default:
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
