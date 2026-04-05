package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunPluginList(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based plugin executable test is unix-only")
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "goframe.yaml")
	if err := os.WriteFile(cfgPath, []byte("mail_driver: sendgrid\n"), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	writePluginExecutable(t, filepath.Join(dir, "goframe-plugin-twilio"), `#!/bin/sh
if [ "$1" = "capabilities" ] && [ "$2" = "--json" ]; then
  echo '{"capabilities":["queue.publish","webhook.deliver"]}'
  exit 0
fi
if [ "$1" = "capabilities" ]; then
  echo "queue.publish webhook.deliver"
  exit 0
fi
exit 1
`)
	writePluginExecutable(t, filepath.Join(dir, "goframe-mail-mailgun"), "#!/bin/sh\nexit 0\n")

	previousPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+previousPath); err != nil {
		t.Fatalf("set PATH failed: %v", err)
	}
	defer func() {
		_ = os.Setenv("PATH", previousPath)
	}()

	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := runPlugin([]string{"list", "--config", cfgPath}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("runPlugin list failed: %v (stderr=%s)", err, errOut.String())
	}

	text := out.String()
	if !strings.Contains(text, "Active mail driver: sendgrid") {
		t.Fatalf("expected active mail driver in output: %s", text)
	}
	if !strings.Contains(text, "twilio") || !strings.Contains(text, "queue.publish") {
		t.Fatalf("expected generic plugin capabilities in output: %s", text)
	}
	if !strings.Contains(text, "mailgun") || !strings.Contains(text, "external_legacy_mail") {
		t.Fatalf("expected legacy plugin in output: %s", text)
	}
}

func TestRunPluginDoctorInvalidMailDriver(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "goframe.yaml")
	if err := os.WriteFile(cfgPath, []byte("mail_driver: missingdriver\n"), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	err := runPlugin([]string{"doctor", "--config", cfgPath}, strings.NewReader(""), &out, &errOut)
	if err == nil {
		t.Fatalf("expected plugin doctor to fail for invalid mail driver; output=%s", out.String())
	}
	if !strings.Contains(out.String(), "plugin.mail_driver\terror") {
		t.Fatalf("expected mail driver error in doctor output: %s", out.String())
	}
}

func TestRunPluginTestDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based plugin executable test is unix-only")
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "goframe.yaml")
	if err := os.WriteFile(cfgPath, []byte("mail_driver: noop\n"), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	writePluginExecutable(t, filepath.Join(dir, "goframe-plugin-acme"), `#!/bin/sh
if [ "$1" = "capabilities" ] && [ "$2" = "--json" ]; then
  echo '{"capabilities":["queue.publish"]}'
  exit 0
fi
if [ "$1" = "capabilities" ]; then
  echo "queue.publish"
  exit 0
fi
exit 1
`)

	previousPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+previousPath); err != nil {
		t.Fatalf("set PATH failed: %v", err)
	}
	defer func() {
		_ = os.Setenv("PATH", previousPath)
	}()

	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := runPlugin([]string{
		"test",
		"--config", cfgPath,
		"--provider", "acme",
		"--capability", "queue.publish",
	}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("runPlugin test discovery failed: %v (stderr=%s)", err, errOut.String())
	}

	text := out.String()
	if !strings.Contains(text, "status\tok") {
		t.Fatalf("expected ok status in plugin test output: %s", text)
	}
	if !strings.Contains(text, "source\texternal_generic") {
		t.Fatalf("expected external generic source in plugin test output: %s", text)
	}
}

func TestRunPluginTestExecuteLegacyWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based plugin executable test is unix-only")
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "goframe.yaml")
	if err := os.WriteFile(cfgPath, []byte("mail_driver: noop\n"), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	writePluginExecutable(t, filepath.Join(dir, "goframe-mail-mailgun"), "#!/bin/sh\nexit 0\n")

	previousPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+previousPath); err != nil {
		t.Fatalf("set PATH failed: %v", err)
	}
	defer func() {
		_ = os.Setenv("PATH", previousPath)
	}()

	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := runPlugin([]string{
		"test",
		"--config", cfgPath,
		"--provider", "mailgun",
		"--capability", "mail.send",
		"--execute",
	}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("runPlugin test execute (legacy) should warn, not fail: %v (stderr=%s)", err, errOut.String())
	}

	text := out.String()
	if !strings.Contains(text, "status\twarning") {
		t.Fatalf("expected warning status for legacy execute mode: %s", text)
	}
}

func writePluginExecutable(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o755); err != nil {
		t.Fatalf("write plugin executable failed: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatalf("chmod plugin executable failed: %v", err)
		}
	}
}
