package cli

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAdminCLIConfigWithChain is writeAdminCLIConfig plus an auth_backends
// chain, which is what makes the local password hash irrelevant.
func writeAdminCLIConfigWithChain(t *testing.T, backends string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := filepath.Join(dir, "nucleus.yml")
	cfg := fmt.Sprintf(
		"database_default: default\ndatabases:\n  default:\n    url: sqlite://%s\nlog_level: error\nlog_format: text\nauth_backends: %s\n",
		dbPath, backends,
	)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath, dbPath
}

func seedAdminRow(t *testing.T, dbPath, username string) {
	t.Helper()
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(
		"INSERT INTO nucleus_admin_users (id, username, email, password_hash, is_superuser, created_at, updated_at) VALUES ('u1', ?, 'a@b.c', 'VIEJO', 1, 'now', 'now')",
		username,
	); err != nil {
		t.Fatalf("seed admin row: %v", err)
	}
}

func adminHash(t *testing.T, dbPath string) string {
	t.Helper()
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer conn.Close()
	var h string
	if err := conn.QueryRow("SELECT password_hash FROM nucleus_admin_users WHERE id = 'u1'").Scan(&h); err != nil {
		t.Fatalf("read hash: %v", err)
	}
	return h
}

// TestRunChangePassword_RefusesWhenAChainMakesTheHashIrrelevant pins that a
// recovery command does not report success without producing the effect the
// operator is asking for (QCD-FW-27).
//
// With `auth_backends` configured and the local backend not in it, the panel
// authenticates through the chain and never reads the local hash. The
// command still wrote it, printed "Password updated" and exited 0:
//
//	$ nucleus changepassword --config chain.yml --password NUEVA --no-input root
//	Password updated: root
//	EXIT=0
//	hash en la tabla tras el cambio: $2a$12$ddXL0ZFmEA2cW   (SÍ se escribió)
//
// The operator believes access is restored. It is not, and the class —
// exit 0 without the effect the user believes they got — is at its worst on
// the command someone reaches for when they are locked out.
func TestRunChangePassword_RefusesWhenAChainMakesTheHashIrrelevant(t *testing.T) {
	cfgPath, dbPath := writeAdminCLIConfigWithChain(t, "[dir]")
	createAdminUsersTable(t, dbPath)
	seedAdminRow(t, dbPath, "root")

	var out, errOut bytes.Buffer
	err := runChangePassword([]string{
		"--config", cfgPath, "--no-input", "--password", "NUEVA-local", "root",
	}, strings.NewReader(""), &out, &errOut)

	if err == nil {
		t.Fatalf("changepassword must refuse when the chain makes the hash irrelevant; stdout=%q", out.String())
	}
	// The message has to name the chain, or the operator cannot tell why.
	if !strings.Contains(err.Error(), "dir") || !strings.Contains(err.Error(), "auth_backends") {
		t.Errorf("the error must name auth_backends and the configured chain, got: %v", err)
	}
	// And it must not have written a hash nobody will read.
	if got := adminHash(t, dbPath); got != "VIEJO" {
		t.Errorf("the hash must be left untouched when the command refuses, got %q", got)
	}
}

// TestRunChangePassword_ProceedsWhenLocalIsInTheChain is the control that
// keeps the refusal narrow. With the local backend in the chain the hash IS
// consulted — after the FW-20 fix it is the break-glass path, which makes it
// more important, not less. Refusing here would break the recovery this
// command exists for.
func TestRunChangePassword_ProceedsWhenLocalIsInTheChain(t *testing.T) {
	cfgPath, dbPath := writeAdminCLIConfigWithChain(t, "[dir, local]")
	createAdminUsersTable(t, dbPath)
	seedAdminRow(t, dbPath, "root")

	var out, errOut bytes.Buffer
	if err := runChangePassword([]string{
		"--config", cfgPath, "--no-input", "--password", "NUEVA-local", "root",
	}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("changepassword must proceed when the local backend is in the chain: %v", err)
	}
	if got := adminHash(t, dbPath); got == "VIEJO" {
		t.Error("the hash must have been updated")
	}
}

// And with no chain at all, nothing changes.
func TestRunChangePassword_ProceedsWithNoChain(t *testing.T) {
	cfgPath, dbPath := writeAdminCLIConfig(t)
	createAdminUsersTable(t, dbPath)
	seedAdminRow(t, dbPath, "root")

	var out, errOut bytes.Buffer
	if err := runChangePassword([]string{
		"--config", cfgPath, "--no-input", "--password", "NUEVA-local", "root",
	}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("changepassword must work with no chain configured: %v", err)
	}
	if got := adminHash(t, dbPath); got == "VIEJO" {
		t.Error("the hash must have been updated")
	}
}
