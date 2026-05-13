package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectSendTestEmailRecipients(t *testing.T) {
	recipients, err := collectSendTestEmailRecipients(
		"alice@example.com,bob@example.com",
		[]string{"carol@example.com", "alice@example.com"},
	)
	if err != nil {
		t.Fatalf("collectSendTestEmailRecipients failed: %v", err)
	}

	got := strings.Join(recipients, ",")
	want := "alice@example.com,bob@example.com,carol@example.com"
	if got != want {
		t.Fatalf("unexpected recipients: got %q want %q", got, want)
	}
}

func TestResolveMailDriver(t *testing.T) {
	if got := resolveMailDriver(""); got != "noop" {
		t.Fatalf("expected noop fallback, got %q", got)
	}
	if got := resolveMailDriver("SendGrid"); got != "sendgrid" {
		t.Fatalf("expected normalized sendgrid, got %q", got)
	}
}

func TestRunSendTestEmailDryRunIncludesDriver(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	// Use the SMTP built-in driver — only protocol-universal senders
	// (SMTP) ship in-tree; provider-specific drivers like sendgrid
	// install as `nucleus-plugin-<driver>` and surface in the dry-run
	// output as `plugin=nucleus-plugin-<driver>` (see TestRunSendTestEmailDryRunDriverOverride).
	raw := "mail_driver: smtp\nmail_from: noreply@example.com\nsmtp_host: smtp.example.com\nsmtp_port: 587\n"
	if err := os.WriteFile(cfgPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := runSendTestEmail([]string{
		"--config", cfgPath,
		"--to", "dev@example.com",
		"--dry-run",
	}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("runSendTestEmail dry-run failed: %v (stderr=%s)", err, errOut.String())
	}

	output := out.String()
	if !strings.Contains(output, "driver=smtp") {
		t.Fatalf("expected driver in dry-run output, got: %s", output)
	}
	if !strings.Contains(output, "smtp_host=smtp.example.com") {
		t.Fatalf("expected provider details in dry-run output, got: %s", output)
	}
}

func TestRunSendTestEmailDryRunDriverOverride(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	raw := "mail_driver: smtp\nmail_from: noreply@example.com\nsendgrid_endpoint: https://api.sendgrid.test/v3/mail/send\n"
	if err := os.WriteFile(cfgPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := runSendTestEmail([]string{
		"--config", cfgPath,
		"--driver", "sendgrid",
		"--to", "dev@example.com",
		"--dry-run",
	}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("runSendTestEmail dry-run with driver override failed: %v (stderr=%s)", err, errOut.String())
	}

	output := out.String()
	if !strings.Contains(output, "driver=sendgrid") {
		t.Fatalf("expected override driver in dry-run output, got: %s", output)
	}
	if strings.Contains(output, "driver=smtp") {
		t.Fatalf("expected smtp driver to be overridden, got: %s", output)
	}
}

func TestRunSendTestEmailRejectsNoopWithoutDryRun(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	raw := "mail_driver: noop\nmail_from: noreply@example.com\n"
	if err := os.WriteFile(cfgPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	err := runSendTestEmail([]string{
		"--config", cfgPath,
		"--to", "dev@example.com",
	}, strings.NewReader(""), &out, &errOut)
	if err == nil {
		t.Fatalf("expected sendtestemail without dry-run to reject noop driver; output=%s", out.String())
	}
	if !strings.Contains(err.Error(), "mail_driver is noop") {
		t.Fatalf("unexpected error: %v", err)
	}
}
