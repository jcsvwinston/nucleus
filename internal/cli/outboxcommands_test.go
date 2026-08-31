package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jcsvwinston/nucleus/pkg/outbox"
)

// writeOutboxConfig writes a minimal config with the outbox enabled over a
// SQLite database in the temp dir, and returns (configPath, dbPath).
func writeOutboxConfig(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	configPath := filepath.Join(dir, "nucleus.yml")
	yaml := fmt.Sprintf("databases:\n  default:\n    url: sqlite://%s\noutbox:\n  enabled: true\n", dbPath)
	if err := os.WriteFile(configPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, dbPath
}

func seedFailedOutboxMessage(t *testing.T, dbPath string) string {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()
	store, err := outbox.NewStore(sqlDB, outbox.Config{Flavor: outbox.FlavorSQLite})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	msg, err := store.Enqueue(context.Background(), outbox.Entry{Topic: "t", Payload: map[string]any{}})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := sqlDB.Exec(`UPDATE nucleus_outbox SET status = 'failed', attempts = 5, last_error = 'boom' WHERE id = ?`, msg.ID); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	return msg.ID
}

// NF-8 end to end: `nucleus outbox requeue` moves failed messages back to
// pending without hand-written SQL.
func TestRunOutboxRequeue(t *testing.T) {
	configPath, dbPath := writeOutboxConfig(t)
	seedFailedOutboxMessage(t, dbPath)

	var out, errOut bytes.Buffer
	if err := runOutbox([]string{"requeue", "--config", configPath, "--json"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("outbox requeue: %v (stderr: %s)", err, errOut.String())
	}
	var report outboxRequeueReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out.String())
	}
	if report.Requeued != 1 || report.Status != "ok" {
		t.Fatalf("report = %+v; want requeued=1 status=ok", report)
	}

	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()
	var status string
	var attempts int
	if err := sqlDB.QueryRow(`SELECT status, attempts FROM nucleus_outbox`).Scan(&status, &attempts); err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if status != "pending" || attempts != 0 {
		t.Fatalf("after requeue: status=%s attempts=%d; want pending/0", status, attempts)
	}
}

func TestRunOutboxRequeueNothingFailed(t *testing.T) {
	configPath, _ := writeOutboxConfig(t)
	var out, errOut bytes.Buffer
	if err := runOutbox([]string{"requeue", "--config", configPath}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("outbox requeue: %v (stderr: %s)", err, errOut.String())
	}
	if !strings.Contains(out.String(), "nothing to requeue") {
		t.Fatalf("expected 'nothing to requeue' message, got: %s", out.String())
	}
}

func TestRunOutboxRequeueDisabledOutboxExplains(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "nucleus.yml")
	dbPath := filepath.Join(dir, "app.db")
	yaml := fmt.Sprintf("databases:\n  default:\n    url: sqlite://%s\n", dbPath)
	if err := os.WriteFile(configPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var out, errOut bytes.Buffer
	err := runOutbox([]string{"requeue", "--config", configPath}, strings.NewReader(""), &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "outbox.enabled") {
		t.Fatalf("disabled outbox err = %v; want mention of outbox.enabled", err)
	}
}

func TestRunOutboxUnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	err := runOutbox([]string{"nope"}, strings.NewReader(""), &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "requeue") {
		t.Fatalf("unknown subcommand err = %v; want available list", err)
	}
}
