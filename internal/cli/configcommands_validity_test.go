// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// `config print` is the last CLI surface that reads a config file without
// judging it: it merges and renders with provenance, so an invalid file
// prints clean and the operator learns it is unbootable from the next
// command's crash instead.
//
// Printing it is still right — you print a broken config precisely to see
// what it resolved to. The fix is to say so, on stderr, so stdout stays
// machine-parseable.
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCLIConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nucleus.yml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigPrint_WarnsOnUnbootableConfig(t *testing.T) {
	path := writeCLIConfig(t, "session_cookie_name: \"__Host-session\"\nsession_cookie_secure: false\n")

	var out, errOut bytes.Buffer
	if err := runConfig([]string{"print", "--config", path}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("print must still render an invalid config, not fail: %v", err)
	}

	if !strings.Contains(out.String(), "session_cookie_name") {
		t.Errorf("the resolved values must still be printed, got:\n%s", out.String())
	}
	warning := errOut.String()
	if !strings.Contains(warning, "session_cookie_secure=true") {
		t.Errorf("stderr must say WHY the config will not boot, got: %q", warning)
	}
	if strings.Contains(out.String(), "will not boot") {
		t.Errorf("the warning belongs on stderr; stdout must stay machine-parseable, got:\n%s", out.String())
	}
}

func TestConfigPrint_SilentOnValidConfig(t *testing.T) {
	path := writeCLIConfig(t, "log_level: info\n")

	var out, errOut bytes.Buffer
	if err := runConfig([]string{"print", "--config", path}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("print a valid config: %v", err)
	}
	if strings.TrimSpace(errOut.String()) != "" {
		t.Errorf("a valid config must produce no warning, got: %q", errOut.String())
	}
}

// --json exists to be piped. A warning on stdout would corrupt the document.
func TestConfigPrint_JSONStaysCleanWhenInvalid(t *testing.T) {
	path := writeCLIConfig(t, "log_level: verbose\n")

	var out, errOut bytes.Buffer
	if err := runConfig([]string{"print", "--json", "--config", path}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("print --json: %v", err)
	}
	if first := strings.TrimSpace(out.String()); !strings.HasPrefix(first, "{") {
		t.Errorf("stdout must be a JSON document, got: %.80s", first)
	}
	if !strings.Contains(errOut.String(), "log_level") {
		t.Errorf("the warning must still reach stderr, got: %q", errOut.String())
	}
}
