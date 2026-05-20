package admin

import (
	"context"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
)

// orLabel returns s, or fallback when s is empty — used to give the
// empty-system subtest a readable name.
func orLabel(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// TestBootstrapAdminUsersTableDDL_Dialects asserts the per-dialect
// CREATE-TABLE form for the admin users table. The MSSQL and Oracle
// branches close the bug that blocked TestSQLMatrix_AutoMigrate_Exploratory:
// MSSQL rejected `CREATE TABLE IF NOT EXISTS` ("Incorrect syntax near
// 'nucleus_admin_users'") and Oracle rejected `NOT NULL DEFAULT 0`
// (ORA-03076). These are offline string assertions — no live DB needed.
func TestBootstrapAdminUsersTableDDL_Dialects(t *testing.T) {
	t.Parallel()

	// The default branch must serve every non-mssql/oracle system,
	// including the empty string and the literal "unknown" that
	// db.dbSystemFromURL emits for an unrecognised URL scheme.
	for _, sys := range []string{"", "sqlite", "postgresql", "mysql", "unknown"} {
		sys := sys
		t.Run("default/"+orLabel(sys, "empty"), func(t *testing.T) {
			t.Parallel()
			ddl := bootstrapAdminUsersTableDDL(sys)
			if !strings.Contains(ddl, "CREATE TABLE IF NOT EXISTS "+defaultAdminUsersTable) {
				t.Errorf("system=%q: want portable CREATE TABLE IF NOT EXISTS, got:\n%s", sys, ddl)
			}
			if !strings.Contains(ddl, "is_superuser INTEGER NOT NULL DEFAULT 0") {
				t.Errorf("system=%q: want INTEGER NOT NULL DEFAULT 0, got:\n%s", sys, ddl)
			}
		})
	}

	t.Run("mssql", func(t *testing.T) {
		t.Parallel()
		ddl := bootstrapAdminUsersTableDDL("mssql")
		// SQL Server has no IF NOT EXISTS — must guard with OBJECT_ID.
		if !strings.Contains(ddl, "IF OBJECT_ID('"+defaultAdminUsersTable+"', 'U') IS NULL") {
			t.Errorf("mssql: want IF OBJECT_ID guard, got:\n%s", ddl)
		}
		if strings.Contains(ddl, "IF NOT EXISTS") {
			t.Errorf("mssql: must NOT use CREATE TABLE IF NOT EXISTS, got:\n%s", ddl)
		}
		// Dialect-correct column types.
		for _, want := range []string{
			"id NVARCHAR(64) NOT NULL PRIMARY KEY",
			"password_hash NVARCHAR(MAX) NOT NULL",
			"is_superuser BIT NOT NULL DEFAULT 0",
		} {
			if !strings.Contains(ddl, want) {
				t.Errorf("mssql: missing %q in:\n%s", want, ddl)
			}
		}
		if strings.Contains(ddl, "TEXT") || strings.Contains(ddl, "INTEGER") {
			t.Errorf("mssql: must not use TEXT/INTEGER (deprecated/non-idiomatic), got:\n%s", ddl)
		}
	})

	t.Run("oracle", func(t *testing.T) {
		t.Parallel()
		ddl := bootstrapAdminUsersTableDDL("oracle")
		// Oracle has no IF NOT EXISTS — must wrap in a PL/SQL block that
		// swallows ORA-00955.
		if !strings.Contains(ddl, "EXECUTE IMMEDIATE") || !strings.Contains(ddl, "SQLCODE != -955") {
			t.Errorf("oracle: want PL/SQL block swallowing ORA-00955, got:\n%s", ddl)
		}
		// The ORA-03076 fix: DEFAULT must precede NOT NULL.
		if !strings.Contains(ddl, "is_superuser NUMBER(1) DEFAULT 0 NOT NULL") {
			t.Errorf("oracle: want `NUMBER(1) DEFAULT 0 NOT NULL` (DEFAULT before NOT NULL — the ORA-03076 fix), got:\n%s", ddl)
		}
		// Catch a regression where someone reorders to the rejected form.
		if strings.Contains(ddl, "NOT NULL DEFAULT") {
			t.Errorf("oracle: `NOT NULL DEFAULT` ordering is rejected with ORA-03076; got:\n%s", ddl)
		}
		for _, want := range []string{
			"id VARCHAR2(64) NOT NULL PRIMARY KEY",
			"password_hash VARCHAR2(4000) NOT NULL",
		} {
			if !strings.Contains(ddl, want) {
				t.Errorf("oracle: missing %q in:\n%s", want, ddl)
			}
		}
		if strings.Contains(ddl, "IF NOT EXISTS") {
			t.Errorf("oracle: must NOT use CREATE TABLE IF NOT EXISTS, got:\n%s", ddl)
		}
	})
}

// TestEnsureBootstrapAdminUser_SQLite is an end-to-end check of the
// default (SQLite) bootstrap path: the table is created, the first
// superuser is inserted, and a second call is idempotent (no second
// account, no error). Exercises the dialect-agnostic INSERT/COUNT
// statements alongside the default DDL.
func TestEnsureBootstrapAdminUser_SQLite(t *testing.T) {
	t.Parallel()

	database, err := db.New(db.Config{DatabaseURL: "sqlite://:memory:"}, nil)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer database.Close()
	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}

	cfg := BootstrapAdminConfig{
		Username: "root",
		Email:    "root@example.com",
		Password: "supersecret",
		System:   database.System(),
	}

	res, err := EnsureBootstrapAdminUser(context.Background(), sqlDB, cfg)
	if err != nil {
		t.Fatalf("EnsureBootstrapAdminUser (first): %v", err)
	}
	if !res.Created {
		t.Fatal("first call should create the bootstrap admin")
	}
	if res.Username != "root" {
		t.Errorf("username: got %q want root", res.Username)
	}

	// Idempotent: a second call must not create a second account.
	res2, err := EnsureBootstrapAdminUser(context.Background(), sqlDB, cfg)
	if err != nil {
		t.Fatalf("EnsureBootstrapAdminUser (second): %v", err)
	}
	if res2.Created {
		t.Error("second call should be a no-op (table already has a user)")
	}

	var count int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM " + defaultAdminUsersTable).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Errorf("user count: got %d want 1", count)
	}
}

// TestEnsureBootstrapAdminUser_EmptySystemUsesDefaultDDL guards the
// backward-compatibility contract: a config with no System set falls
// back to the portable DDL and still works on SQLite.
func TestEnsureBootstrapAdminUser_EmptySystemUsesDefaultDDL(t *testing.T) {
	t.Parallel()

	database, err := db.New(db.Config{DatabaseURL: "sqlite://:memory:"}, nil)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer database.Close()
	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}

	// System deliberately left empty.
	res, err := EnsureBootstrapAdminUser(context.Background(), sqlDB, BootstrapAdminConfig{
		Username: "admin",
		Email:    "admin@example.com",
		Password: "supersecret",
	})
	if err != nil {
		t.Fatalf("EnsureBootstrapAdminUser with empty System: %v", err)
	}
	if !res.Created {
		t.Fatal("empty-System bootstrap should still create the admin via the portable DDL")
	}
}
