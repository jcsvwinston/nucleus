package cli

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestSQLMatrix_ExploratoryAdminUserCommands is the real-engine round-trip
// for `nucleus createuser` / `nucleus changepassword` on the exploratory
// engines whose single-row grammar differs from LIMIT. Their admin-user
// lookups gained a TOP 1 branch for mssql (NU6-3), but the MSSQL lane had no
// fixture for the orbit admin schema, so that branch had never executed
// against a real engine (NU7-3); Oracle then repeated the same history — it
// shared the LIMIT branch, invalid in its grammar (ORA-00933), until NU8-1
// gave it FETCH FIRST 1 ROWS ONLY. This test provisions the minimal schema
// the commands require (see requireOrbitAdminSchema: the nucleus_admin_users
// table, owned by orbit per ADR-019) and exercises:
//
//   - createuser (insert path) — new admin row;
//   - createuser again, same username — the findExistingAdminUserID lookup
//     (the single-row SELECT) followed by the update path;
//   - changepassword — the findAdminUserIDByUsername lookup plus the hash
//     update, asserted by comparing stored hashes;
//   - changepassword for a missing user — the no-rows path errors honestly.
//
// mssql and oracle only: the TOP 1 / FETCH FIRST grammars are what this
// pins; the LIMIT branch is covered by the required postgres/mysql lanes.
func TestSQLMatrix_ExploratoryAdminUserCommands(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_EXPLORATORY_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_EXPLORATORY_URL is not set; skipping exploratory admin user command test")
	}
	flavor := detectDBFlavor(rawURL)
	if flavor != dbFlavorMSSQL && flavor != dbFlavorOracle {
		t.Skipf("NUCLEUS_SQL_EXPLORATORY_URL=%q is not an MSSQL or Oracle profile", rawURL)
	}

	dir := t.TempDir()
	cfgPath := writeSQLMatrixConfig(t, dir, rawURL)

	runSQL := func(label, sqlText string) string {
		t.Helper()
		var out, errOut bytes.Buffer
		if err := runShell([]string{"--config", cfgPath, "-c", sqlText}, strings.NewReader(""), &out, &errOut); err != nil {
			t.Fatalf("%s failed: err=%v stderr=%s", label, err, errOut.String())
		}
		return out.String()
	}

	// selectOne mirrors each engine's single-row grammar for the test's own
	// verification reads (the command under test builds its equivalent via
	// selectOneAdminUserIDSQL).
	selectOne := func(column, where string) string {
		if flavor == dbFlavorMSSQL {
			return fmt.Sprintf("SELECT TOP 1 %s FROM %s WHERE %s", column, adminUsersTable, where)
		}
		return fmt.Sprintf("SELECT %s FROM %s WHERE %s FETCH FIRST 1 ROWS ONLY", column, adminUsersTable, where)
	}

	// Minimal orbit admin schema (the exact columns the two commands read
	// and write). The table name is the fixed contract adminUsersTable; a
	// leftover from a previous run is dropped first. Oracle 23 (the CI
	// image) supports DROP TABLE IF EXISTS like the others.
	columnsDDL := "id NVARCHAR(64) PRIMARY KEY, " +
		"username NVARCHAR(64) NOT NULL, " +
		"email NVARCHAR(255) NOT NULL, " +
		"password_hash NVARCHAR(255) NOT NULL, " +
		"is_superuser BIT NOT NULL, " +
		"created_at NVARCHAR(64) NOT NULL, " +
		"updated_at NVARCHAR(64) NOT NULL"
	if flavor == dbFlavorOracle {
		columnsDDL = "id VARCHAR2(64) PRIMARY KEY, " +
			"username VARCHAR2(64) NOT NULL, " +
			"email VARCHAR2(255) NOT NULL, " +
			"password_hash VARCHAR2(255) NOT NULL, " +
			"is_superuser NUMBER(1) NOT NULL, " +
			"created_at VARCHAR2(64) NOT NULL, " +
			"updated_at VARCHAR2(64) NOT NULL"
	}
	runSQL("drop leftover admin table", "DROP TABLE IF EXISTS "+adminUsersTable)
	runSQL("create admin table", fmt.Sprintf("CREATE TABLE %s (%s)", adminUsersTable, columnsDDL))
	t.Cleanup(func() {
		var out, errOut bytes.Buffer
		_ = runShell([]string{"--config", cfgPath, "-c", "DROP TABLE IF EXISTS " + adminUsersTable}, strings.NewReader(""), &out, &errOut)
	})

	var out, errOut bytes.Buffer

	// Insert path: a brand-new admin user.
	if err := runCreateUser([]string{
		"--config", cfgPath, "--no-input",
		"--username", "matrixadmin",
		"--email", "matrixadmin@example.com",
		"--password", "matrix-secret-1",
	}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("createuser (insert) failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "Admin user created: matrixadmin") {
		t.Fatalf("unexpected createuser output: %s", out.String())
	}

	firstHash := runSQL("read hash after create",
		selectOne("password_hash", "username = 'matrixadmin'"))
	if !strings.Contains(firstHash, "$2") {
		t.Fatalf("expected a bcrypt hash stored for matrixadmin, got: %s", firstHash)
	}

	// Existing-user path: same username, new email. This is the single-row
	// lookup (findExistingAdminUserID) running against the real engine — the
	// statement shape NU6-3 (mssql) / NU8-1 (oracle) fixed and no lane had
	// ever executed.
	out.Reset()
	errOut.Reset()
	if err := runCreateUser([]string{
		"--config", cfgPath, "--no-input",
		"--username", "matrixadmin",
		"--email", "matrixadmin+rotated@example.com",
		"--password", "matrix-secret-2",
	}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("createuser (update) failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "Admin user updated: matrixadmin") {
		t.Fatalf("unexpected createuser update output: %s", out.String())
	}
	rowCount := runSQL("count admin rows",
		"SELECT COUNT(*) AS total FROM "+adminUsersTable+" WHERE username = 'matrixadmin'")
	if !strings.Contains(rowCount, "1") {
		t.Fatalf("expected exactly one matrixadmin row after upsert, got: %s", rowCount)
	}
	emailOut := runSQL("read email after update",
		selectOne("email", "username = 'matrixadmin'"))
	if !strings.Contains(emailOut, "matrixadmin+rotated@example.com") {
		t.Fatalf("expected updated email, got: %s", emailOut)
	}

	// changepassword: single-row lookup by username plus the hash update.
	out.Reset()
	errOut.Reset()
	if err := runChangePassword([]string{
		"--config", cfgPath, "--no-input",
		"--password", "matrix-secret-3",
		"matrixadmin",
	}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("changepassword failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "Password updated: matrixadmin") {
		t.Fatalf("unexpected changepassword output: %s", out.String())
	}
	updatedHash := runSQL("read hash after changepassword",
		selectOne("password_hash", "username = 'matrixadmin'"))
	if !strings.Contains(updatedHash, "$2") {
		t.Fatalf("expected a bcrypt hash after changepassword, got: %s", updatedHash)
	}
	if updatedHash == firstHash {
		t.Fatalf("changepassword did not change the stored hash:\nbefore=%s\nafter=%s", firstHash, updatedHash)
	}

	// Missing user: the lookup finds no rows and the command says so
	// instead of surfacing a SQL error.
	out.Reset()
	errOut.Reset()
	err := runChangePassword([]string{
		"--config", cfgPath, "--no-input",
		"--password", "matrix-secret-4",
		"nobody.here",
	}, strings.NewReader(""), &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("changepassword for a missing user: err=%v, want a 'not found' error", err)
	}
}
