package cli

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// The admin user store (`nucleus_admin_users`) is owned by the orbit
// module (`github.com/jcsvwinston/orbit`) as of ADR-019. The CLI
// commands `createuser` and `changepassword` operate on that table but
// MUST NOT create it: orbit's migrations (or its first start) are the
// single source of truth for the schema. These helpers let the
// commands fail fast with an actionable message when orbit has not yet
// initialised the target database, instead of silently creating an
// orphan table (the historical `ensureAdminUsersTable` behaviour) or
// surfacing a raw "no such table" SQL error.

// requireOrbitAdminSchema verifies that the orbit admin schema (the
// `nucleus_admin_users` table) already exists in the target database.
// If the table is absent it returns an actionable error explaining that
// the orbit module must initialise the schema first. The dialect is the
// value returned by (*db.DB).System() — one of "sqlite", "postgresql",
// "mysql", "mssql", "oracle".
func requireOrbitAdminSchema(sqlDB *sql.DB, dialect, command string) error {
	exists, err := adminUsersTableExists(sqlDB, dialect)
	if err != nil {
		return fmt.Errorf("%s: check orbit admin schema: %w", command, err)
	}
	if !exists {
		return orbitNotInstalledError(command)
	}
	return nil
}

// orbitNotInstalledError builds the actionable "orbit not installed"
// error for the given command. The message names the missing table and
// the concrete steps an operator takes to provision it via orbit.
func orbitNotInstalledError(command string) error {
	return fmt.Errorf(
		"%s: the orbit admin module does not appear to be installed for this database "+
			"(table %s not found). The admin user store is provided by the orbit module — "+
			"add github.com/jcsvwinston/orbit, mount orbit.Module(...), and start the app "+
			"(or run its migrations) once to create the schema, then retry.",
		command, adminUsersTable,
	)
}

// adminUsersTableExists reports whether the `nucleus_admin_users` table
// exists in the connected database. It is dialect-aware and reuses the
// same per-engine existence probes that pkg/db's schema-drift
// introspection relies on (sqlite_master for SQLite; information_schema
// scoped to the current schema/database for PostgreSQL, MySQL and
// MSSQL; the USER_TABLES catalog for Oracle). It deliberately avoids
// fragile error-string matching for "no such table".
func adminUsersTableExists(sqlDB *sql.DB, dialect string) (bool, error) {
	if sqlDB == nil {
		return false, errors.New("adminUsersTableExists: nil database handle")
	}

	var (
		query string
		args  []any
	)
	switch dialect {
	case "sqlite":
		query = "SELECT 1 FROM sqlite_master WHERE type='table' AND name=?"
		args = []any{adminUsersTable}
	case "postgresql":
		query = `SELECT 1 FROM information_schema.tables
		         WHERE table_schema = current_schema() AND table_name = $1`
		args = []any{adminUsersTable}
	case "mysql":
		query = `SELECT 1 FROM information_schema.tables
		         WHERE table_schema = DATABASE() AND table_name = ?`
		args = []any{adminUsersTable}
	case "mssql":
		query = `SELECT 1 FROM INFORMATION_SCHEMA.TABLES
		         WHERE TABLE_SCHEMA = SCHEMA_NAME() AND TABLE_NAME = @p1`
		args = []any{adminUsersTable}
	case "oracle":
		// Oracle folds unquoted identifiers to upper case; the framework
		// emits unquoted identifiers (ADR-011), so match the upper-cased
		// name while keeping the literal-case form as a hedge for a
		// hand-rolled double-quoted DDL.
		query = `SELECT 1 FROM USER_TABLES WHERE TABLE_NAME = :1 OR TABLE_NAME = :2`
		args = []any{strings.ToUpper(adminUsersTable), adminUsersTable}
	default:
		return false, fmt.Errorf("adminUsersTableExists: unsupported database system %q", dialect)
	}

	var exists int
	err := sqlDB.QueryRow(query, args...).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		// A real query error (connectivity, permissions, driver) — keep the
		// dialect + wrap so callers don't mistake it for "table absent".
		return false, fmt.Errorf("adminUsersTableExists: probe %s schema for %q: %w", dialect, adminUsersTable, err)
	}
	return true, nil
}
