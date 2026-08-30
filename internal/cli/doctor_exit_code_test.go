package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeUnhealthyConfig writes a config the security check is certain to
// reject: a catch-all trusted_proxies means the framework honours forwarding
// headers from any caller.
func writeUnhealthyConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "nucleus.yml")
	const yaml = "trusted_proxies:\n  - 0.0.0.0/0\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestRunDoctor_ExitCodeDoesNotDependOnOutputFormat pins that the verdict is
// the report's, not the renderer's (QCD-CLI-7).
//
// `doctor` returned the failure from the TEXT rendering path only: the JSON
// branch encoded the report and returned nil, so the same unhealthy report
// exited 1 as text and 0 as JSON. The one mode that never failed was exactly
// the one CI uses.
func TestRunDoctor_ExitCodeDoesNotDependOnOutputFormat(t *testing.T) {
	path := writeUnhealthyConfig(t)

	var textOut, textErr bytes.Buffer
	textVerdict := runDoctor([]string{"--check", "security", "--config", path}, strings.NewReader(""), &textOut, &textErr)

	var jsonOut, jsonErr bytes.Buffer
	jsonVerdict := runDoctor([]string{"--check", "security", "--config", path, "--json"}, strings.NewReader(""), &jsonOut, &jsonErr)

	// Precondition: the report really is unhealthy, or the test proves nothing.
	var report doctorReport
	if err := json.Unmarshal(jsonOut.Bytes(), &report); err != nil {
		t.Fatalf("decode doctor report: %v", err)
	}
	if report.OverallStatus != "unhealthy" {
		t.Fatalf("precondition: expected an unhealthy report, got %q", report.OverallStatus)
	}

	if textVerdict == nil {
		t.Error("text mode must fail on an unhealthy report")
	}
	if jsonVerdict == nil {
		t.Errorf("JSON mode must fail on the SAME unhealthy report; text said %v, JSON said nil", textVerdict)
	}

	// And the JSON stays machine-readable: the failure is the exit code, not
	// a diagnostic glued onto stdout.
	if !json.Valid(jsonOut.Bytes()) {
		t.Errorf("JSON output must stay parseable, got: %s", jsonOut.String())
	}
}

// TestRunDoctor_HealthyJSONStillSucceeds is the control: making JSON fail on
// unhealthy must not make it fail on everything.
func TestRunDoctor_HealthyJSONStillSucceeds(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := runDoctor([]string{"--check", "tasks", "--json"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("a warning-only report must still exit 0 in JSON mode: %v", err)
	}
}
