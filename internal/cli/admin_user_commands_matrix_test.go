package cli

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestSQLMatrix_ExploratoryAdminUserCommands is the real-engine round-trip
// for `nucleus createuser` / `nucleus changepassword` on SQL Server. Their
// admin-user lookups gained a TOP 1 branch for mssql (NU6-3), but the MSSQL
// lane had no fixture for the orbit admin schema, so that branch had never
// executed against a real engine (NU7-3). This test provisions the minimal
// schema the commands require (see requireOrbitAdminSchema: the
// nucleus_admin_users table, owned by orbit per ADR-019) and exercises:
//
//   - createuser (insert path) — new admin row;
//   - createuser again, same username — the findExistingAdminUserID lookup
//     (the TOP 1 SELECT) followed by the update path;
//   - changepassword — the findAdminUserIDByUsername lookup (TOP 1 again)
//     plus the hash update, asserted by comparing stored hashes;
//   - changepassword for a missing user — the no-rows path errors honestly.
//
// mssql only: the TOP 1 grammar is what this pins, and mssql is the only
// exploratory engine these two commands claim. (Their non-mssql branch emits
// a trailing LIMIT 1, which Oracle does not accept — the commands make no
// oracle claim, and this test does not manufacture one.)
func TestSQLMatrix_ExploratoryAdminUserCommands(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_EXPLORATORY_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_EXPLORATORY_URL is not set; skipping exploratory admin user command test")
	}
	if detectDBFlavor(rawURL) != dbFlavorMSSQL {
		t.Skipf("NUCLEUS_SQL_EXPLORATORY_URL=%q is not an MSSQL profile", rawURL)
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

	// Minimal orbit admin schema (the exact columns the two commands read
	// and write). The table name is the fixed contract adminUsersTable; a
	// leftover from a previous run is dropped first.
	runSQL("drop leftover admin table", "DROP TABLE IF EXISTS "+adminUsersTable)
	runSQL("create admin table", fmt.Sprintf(
		"CREATE TABLE %s ("+
			"id NVARCHAR(64) PRIMARY KEY, "+
			"username NVARCHAR(64) NOT NULL, "+
			"email NVARCHAR(255) NOT NULL, "+
			"password_hash NVARCHAR(255) NOT NULL, "+
			"is_superuser BIT NOT NULL, "+
			"created_at NVARCHAR(64) NOT NULL, "+
			"updated_at NVARCHAR(64) NOT NULL)",
		adminUsersTable,
	))
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
		"SELECT TOP 1 password_hash FROM "+adminUsersTable+" WHERE username = 'matrixadmin'")
	if !strings.Contains(firstHash, "$2") {
		t.Fatalf("expected a bcrypt hash stored for matrixadmin, got: %s", firstHash)
	}

	// Existing-user path: same username, new email. This is the TOP 1
	// lookup (findExistingAdminUserID) running against real T-SQL — the
	// statement shape NU6-3 fixed and no lane had ever executed.
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
		"SELECT TOP 1 email FROM "+adminUsersTable+" WHERE username = 'matrixadmin'")
	if !strings.Contains(emailOut, "matrixadmin+rotated@example.com") {
		t.Fatalf("expected updated email, got: %s", emailOut)
	}

	// changepassword: TOP 1 lookup by username plus the hash update.
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
		"SELECT TOP 1 password_hash FROM "+adminUsersTable+" WHERE username = 'matrixadmin'")
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
