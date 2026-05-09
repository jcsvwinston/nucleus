package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMailProviders(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	if err := os.WriteFile(cfgPath, []byte("mail_driver: sendgrid\n"), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	err := runMailProviders([]string{"--config", cfgPath}, strings.NewReader(""), &out, &errOut)
	if err != nil {
		t.Fatalf("runMailProviders failed: %v (stderr=%s)", err, errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, "Active driver: sendgrid") {
		t.Fatalf("missing active driver in output: %s", text)
	}
	if !strings.Contains(text, "sendgrid") {
		t.Fatalf("missing sendgrid provider in output: %s", text)
	}
}
