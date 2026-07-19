package cli

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// adminUsersDDL is the table orbit owns (ADR-019). The CLI commands no
// longer create it; tests pre-create it to simulate orbit having
// initialised the schema.
const adminUsersDDL = `CREATE TABLE nucleus_admin_users (
	id VARCHAR(64) PRIMARY KEY,
	username VARCHAR(191) NOT NULL UNIQUE,
	email VARCHAR(191) NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	is_superuser INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
)`

// writeAdminCLIConfig writes a minimal nucleus config pointing at a
// file-backed SQLite database and returns (configPath, dbPath). A file
// (not :memory:) is required because the command opens its own
// connection, so the schema must survive across connections.
func writeAdminCLIConfig(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := filepath.Join(dir, "nucleus.yml")
	cfg := fmt.Sprintf(
		"database_default: default\ndatabases:\n  default:\n    url: sqlite://%s\nlog_level: error\nlog_format: text\n",
		dbPath,
	)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config %s failed: %v", cfgPath, err)
	}
	return cfgPath, dbPath
}

// createAdminUsersTable pre-creates the orbit-owned table in the SQLite
// file at dbPath, simulating orbit having initialised the schema.
func createAdminUsersTable(t *testing.T, dbPath string) {
	t.Helper()
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(adminUsersDDL); err != nil {
		t.Fatalf("create admin users table failed: %v", err)
	}
}

func TestRunCreateUser_HappyPathWhenOrbitSchemaPresent(t *testing.T) {
	cfgPath, dbPath := writeAdminCLIConfig(t)
	createAdminUsersTable(t, dbPath)

	var out, errOut bytes.Buffer
	err := runCreateUser([]string{
		"--config", cfgPath,
		"--no-input",
		"--username", "admin",
		"--email", "admin@example.com",
		"--password", "supersecret123",
	}, strings.NewReader(""), &out, &errOut)
	if err != nil {
		t.Fatalf("createuser failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "Admin user created") {
		t.Fatalf("unexpected createuser output: %s", out.String())
	}

	// Verify the row actually landed in the pre-existing table.
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer conn.Close()
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM nucleus_admin_users WHERE username = 'admin'`).Scan(&count); err != nil {
		t.Fatalf("count admin users failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one admin row, got %d", count)
	}
}

func TestRunCreateUser_FailsWhenOrbitSchemaMissing(t *testing.T) {
	cfgPath, dbPath := writeAdminCLIConfig(t)
	// Deliberately do NOT create the table: orbit is "not installed".

	var out, errOut bytes.Buffer
	err := runCreateUser([]string{
		"--config", cfgPath,
		"--no-input",
		"--username", "admin",
		"--email", "admin@example.com",
		"--password", "supersecret123",
	}, strings.NewReader(""), &out, &errOut)
	if err == nil {
		t.Fatal("expected createuser to fail when the orbit admin schema is missing")
	}
	assertOrbitNotInstalledError(t, "createuser", err)

	// The command must not have created the table as a side effect.
	assertAdminUsersTableAbsent(t, dbPath)
}

func TestRunChangePassword_HappyPathWhenOrbitSchemaPresent(t *testing.T) {
	cfgPath, dbPath := writeAdminCLIConfig(t)
	createAdminUsersTable(t, dbPath)

	// Seed an existing admin user via createuser (also guarded).
	var seedOut, seedErr bytes.Buffer
	if err := runCreateUser([]string{
		"--config", cfgPath,
		"--no-input",
		"--username", "admin",
		"--email", "admin@example.com",
		"--password", "supersecret123",
	}, strings.NewReader(""), &seedOut, &seedErr); err != nil {
		t.Fatalf("seed createuser failed: err=%v stderr=%s", err, seedErr.String())
	}

	var out, errOut bytes.Buffer
	err := runChangePassword([]string{
		"--config", cfgPath,
		"--no-input",
		"--password", "newsecret456",
		"admin",
	}, strings.NewReader(""), &out, &errOut)
	if err != nil {
		t.Fatalf("changepassword failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "Password updated") {
		t.Fatalf("unexpected changepassword output: %s", out.String())
	}
}

func TestRunChangePassword_FailsWhenOrbitSchemaMissing(t *testing.T) {
	cfgPath, dbPath := writeAdminCLIConfig(t)
	// Deliberately do NOT create the table: orbit is "not installed".

	var out, errOut bytes.Buffer
	err := runChangePassword([]string{
		"--config", cfgPath,
		"--no-input",
		"--password", "newsecret456",
		"admin",
	}, strings.NewReader(""), &out, &errOut)
	if err == nil {
		t.Fatal("expected changepassword to fail when the orbit admin schema is missing")
	}
	assertOrbitNotInstalledError(t, "changepassword", err)
	assertAdminUsersTableAbsent(t, dbPath)
}

func TestAdminUsersTableExists_SQLite(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "probe.db")
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer conn.Close()

	exists, err := adminUsersTableExists(conn, "sqlite")
	if err != nil {
		t.Fatalf("existence check (absent) failed: %v", err)
	}
	if exists {
		t.Fatal("expected table to be reported absent before creation")
	}

	if _, err := conn.Exec(adminUsersDDL); err != nil {
		t.Fatalf("create admin users table failed: %v", err)
	}

	exists, err = adminUsersTableExists(conn, "sqlite")
	if err != nil {
		t.Fatalf("existence check (present) failed: %v", err)
	}
	if !exists {
		t.Fatal("expected table to be reported present after creation")
	}
}

func TestAdminUsersTableExists_UnsupportedDialect(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "probe.db")
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer conn.Close()

	if _, err := adminUsersTableExists(conn, "cassandra"); err == nil {
		t.Fatal("expected an error for an unsupported dialect")
	}
}

// The admin-user lookups must not emit `LIMIT 1` on SQL Server — T-SQL
// has no LIMIT clause; mssql takes `SELECT TOP 1 …` instead (NU6-3, the
// CLI counterpart of the NU5-4 CRUD fix).
func TestAdminUserLookupSQL_DialectBranches(t *testing.T) {
	// database.System() values the CLI can see.
	for _, dialect := range []string{"sqlite", "postgresql", "mysql", "oracle"} {
		for name, query := range map[string]string{
			"createuser":     findExistingAdminUserIDSQL(dialect, "admin", "admin@example.com"),
			"changepassword": findAdminUserIDByUsernameSQL(dialect, "admin"),
		} {
			if !strings.HasSuffix(query, " LIMIT 1") {
				t.Fatalf("%s/%s: expected trailing LIMIT 1, got %q", name, dialect, query)
			}
			if strings.Contains(query, "TOP 1") {
				t.Fatalf("%s/%s: unexpected TOP 1 outside mssql: %q", name, dialect, query)
			}
		}
	}

	for name, query := range map[string]string{
		"createuser":     findExistingAdminUserIDSQL("mssql", "admin", "admin@example.com"),
		"changepassword": findAdminUserIDByUsernameSQL("mssql", "admin"),
	} {
		if !strings.HasPrefix(query, "SELECT TOP 1 id FROM "+adminUsersTable) {
			t.Fatalf("%s/mssql: expected SELECT TOP 1 prefix, got %q", name, query)
		}
		if strings.Contains(query, "LIMIT") {
			t.Fatalf("%s/mssql: LIMIT must not reach T-SQL: %q", name, query)
		}
	}
}

func assertOrbitNotInstalledError(t *testing.T, command string, err error) {
	t.Helper()
	msg := err.Error()
	// Assert on the stable, meaningful tokens (the command, the orbit pointer,
	// the table name) — not the exact "not found" phrasing, which may evolve.
	for _, want := range []string{command, "orbit", adminUsersTable} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error message %q missing expected substring %q", msg, want)
		}
	}
}

func assertAdminUsersTableAbsent(t *testing.T, dbPath string) {
	t.Helper()
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer conn.Close()
	exists, err := adminUsersTableExists(conn, "sqlite")
	if err != nil {
		t.Fatalf("existence check failed: %v", err)
	}
	if exists {
		t.Fatal("nucleus_admin_users table must not be created by the guarded command")
	}
}
