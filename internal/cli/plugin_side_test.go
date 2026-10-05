// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A11 N10: `plugin test --execute` sends real envelopes (NU-101), the
// external commands on PATH are listed, and the plugins block of the
// configuration decides which external executables run (NU-102).

func skipWithoutShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-based plugin executable test is unix-only")
	}
}

// pathDir is a fresh directory put first on PATH, with no configuration in
// the working directory and none named by NUCLEUS_CONFIG.
func pathDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NUCLEUS_CONFIG", "")
	t.Chdir(t.TempDir())
	return dir
}

func runCLI(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(args, strings.NewReader(""), &out, &errb)
	return code, out.String(), errb.String()
}

// echoPlugin advertises caps, appends every request envelope to capture and
// answers ok, echoing the request id.
func echoPlugin(t *testing.T, dir, provider, capture string, caps ...string) {
	t.Helper()
	writePluginExecutable(t, filepath.Join(dir, "nucleus-plugin-"+provider), `#!/bin/sh
if [ "$1" = "capabilities" ]; then
  echo '{"capabilities":["`+strings.Join(caps, `","`)+`"]}'
  exit 0
fi
req=$(cat)
printf '%s\n' "$req" >> "`+capture+`"
id=$(printf '%s' "$req" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
echo "{\"version\":\"v1\",\"request_id\":\"$id\",\"ok\":true,\"output\":{\"accepted\":true}}"
`)
}

type testReport struct {
	Status   string `json:"status"`
	Details  string `json:"details"`
	ExitCode int    `json:"exit_code"`
	Stderr   string `json:"stderr"`
	Checks   []struct {
		Capability string `json:"capability"`
		Status     string `json:"status"`
		ExitCode   int    `json:"exit_code"`
		RequestID  string `json:"request_id"`
	} `json:"checks"`
}

func decodeTestReport(t *testing.T, stdout string) testReport {
	t.Helper()
	var r testReport
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("plugin test printed no JSON report: %v\n%s", err, stdout)
	}
	return r
}

// NU-101: a plugin that answers every request with garbage and exit 50
// used to pass `plugin test --execute` as "execute smoke check passed".
func TestPluginTestExecute_FailsWithThePluginsExitCodeAndStderr(t *testing.T) {
	skipWithoutShell(t)
	dir := pathDir(t)
	writePluginExecutable(t, filepath.Join(dir, "nucleus-plugin-broken"), `#!/bin/sh
if [ "$1" = "capabilities" ]; then
  echo '{"capabilities":["mail.send"]}'
  exit 0
fi
cat >/dev/null
echo 'this is not an envelope'
echo 'smtp relay unreachable' >&2
exit 50
`)
	code, stdout, stderr := runCLI("plugin", "test", "--provider", "broken", "--capability", "mail.send", "--execute", "--timeout", "30s", "--json")
	if code != 50 {
		t.Errorf("exit %d, want the plugin's 50 (stderr %q)", code, stderr)
	}
	r := decodeTestReport(t, stdout)
	if r.Status != "error" || r.ExitCode != 50 || !strings.Contains(r.Stderr, "smtp relay unreachable") {
		t.Errorf("report: %+v", r)
	}
	if !strings.Contains(r.Details, "exited 50") || !strings.Contains(r.Details, "smtp relay unreachable") {
		t.Errorf("details do not carry the exit code and stderr: %q", r.Details)
	}
}

func TestPluginTestExecute_OneEnvelopePerCapability(t *testing.T) {
	skipWithoutShell(t)
	dir := pathDir(t)
	capture := filepath.Join(t.TempDir(), "capture.jsonl")
	echoPlugin(t, dir, "multi", capture, "mail.send", "queue.publish")

	code, stdout, stderr := runCLI("plugin", "test", "--provider", "multi", "--execute", "--timeout", "30s", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	r := decodeTestReport(t, stdout)
	if r.Status != "ok" || len(r.Checks) != 2 {
		t.Fatalf("report: %+v", r)
	}
	raw, _ := os.ReadFile(capture)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"capability":"mail.send"`) || !strings.Contains(lines[1], `"capability":"queue.publish"`) {
		t.Fatalf("the plugin received %d envelope(s):\n%s", len(lines), raw)
	}
	if !strings.Contains(lines[0], r.Checks[0].RequestID) || !strings.Contains(lines[0], `"source":"nucleus plugin test"`) {
		t.Errorf("the envelope does not carry the request id and the test's metadata: %s", lines[0])
	}
}

func TestPluginTestExecute_SendsTheGivenPayload(t *testing.T) {
	skipWithoutShell(t)
	dir := pathDir(t)
	capture := filepath.Join(t.TempDir(), "capture.jsonl")
	echoPlugin(t, dir, "custom", capture, "mail.send")
	payload := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payload, []byte(`{"from":"ops@acme.test","to":["ana@acme.test"],"subject":"real","body":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := runCLI("plugin", "test", "--provider", "custom", "--capability", "mail.send", "--execute", "--payload", payload, "--timeout", "30s", "--json"); code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	raw, _ := os.ReadFile(capture)
	if !strings.Contains(string(raw), `"to":["ana@acme.test"]`) {
		t.Fatalf("the plugin did not receive the given payload: %s", raw)
	}

	if code, _, stderr := runCLI("plugin", "test", "--provider", "custom", "--execute", "--payload", payload); code == 0 || !strings.Contains(stderr, "--payload needs --execute and --capability") {
		t.Errorf("--payload without --capability: exit %d, %q", code, stderr)
	}
}

func TestPluginTestExecute_WarnsWhenTheResponseDoesNotEchoTheRequestID(t *testing.T) {
	skipWithoutShell(t)
	dir := pathDir(t)
	writePluginExecutable(t, filepath.Join(dir, "nucleus-plugin-noid"), `#!/bin/sh
if [ "$1" = "capabilities" ]; then echo '{"capabilities":["mail.send"]}'; exit 0; fi
cat >/dev/null
echo '{"version":"v1","ok":true,"output":{"accepted":true}}'
`)
	code, stdout, _ := runCLI("plugin", "test", "--provider", "noid", "--execute", "--timeout", "30s", "--json")
	r := decodeTestReport(t, stdout)
	if code != 0 || r.Status != "warning" || !strings.Contains(r.Details, "does not echo request_id") {
		t.Errorf("exit %d, report %+v", code, r)
	}
}

func TestPluginTestExecute_RefusedByTheAllowlistWithoutRunning(t *testing.T) {
	skipWithoutShell(t)
	dir := pathDir(t)
	capture := filepath.Join(t.TempDir(), "capture.jsonl")
	echoPlugin(t, dir, "unlisted", capture, "mail.send")
	if err := os.WriteFile("nucleus.yml", []byte("plugins:\n  allowed:\n    - provider: sendgrid\n      capabilities: [mail.send]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runCLI("plugin", "test", "--provider", "unlisted", "--execute", "--json")
	r := decodeTestReport(t, stdout)
	if code == 0 || r.Status != "error" || !strings.Contains(r.Details, "it was not executed") {
		t.Errorf("exit %d, report %+v", code, r)
	}
	if _, err := os.Stat(capture); err == nil {
		t.Error("the refused plugin received an envelope")
	}
}

// EX-08: an external command on PATH is listed by the root help and by
// `plugin list`; one a built-in shadows is not offered by the help.
func TestExternalCommands_Listed(t *testing.T) {
	skipWithoutShell(t)
	dir := pathDir(t)
	writePluginExecutable(t, filepath.Join(dir, "nucleus-lintx"), "#!/bin/sh\necho lint\n")
	writePluginExecutable(t, filepath.Join(dir, "nucleus-serve"), "#!/bin/sh\necho shadowed\n")
	writePluginExecutable(t, filepath.Join(dir, "nucleus-plugin-mailx"), "#!/bin/sh\necho '{\"capabilities\":[\"mail.send\"]}'\n")

	for _, args := range [][]string{{"--help"}, {"help"}, {}} {
		_, stdout, _ := runCLI(args...)
		if !strings.Contains(stdout, "lintx ") || !strings.Contains(stdout, filepath.Join(dir, "nucleus-lintx")) {
			t.Errorf("nucleus %v does not list nucleus-lintx:\n%s", args, stdout)
		}
		if strings.Contains(stdout, filepath.Join(dir, "nucleus-serve")) {
			t.Errorf("nucleus %v offers nucleus-serve, which the built-in serve shadows", args)
		}
		if strings.Contains(stdout, "mailx") {
			t.Errorf("nucleus %v lists a capability plugin as a command", args)
		}
	}

	_, stdout, _ := runCLI("plugin", "list", "--json")
	var report pluginListReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("plugin list --json: %v\n%s", err, stdout)
	}
	got := map[string]externalCommand{}
	for _, c := range report.Commands {
		got[c.Name] = c
	}
	if c, ok := got["lintx"]; !ok || c.Shadowed {
		t.Errorf("plugin list: lintx = %+v (%v)", c, report.Commands)
	}
	if c, ok := got["serve"]; !ok || !c.Shadowed {
		t.Errorf("plugin list: serve must be listed shadowed: %+v", c)
	}
	if _, ok := got["plugin-mailx"]; ok {
		t.Error("plugin list lists a capability plugin among the commands")
	}
}

// EX-12 for the dispatcher: with plugins.commands set, `nucleus <name>`
// runs only the commands it lists; allow_external: false runs none.
func TestExternalCommands_Allowlist(t *testing.T) {
	skipWithoutShell(t)
	dir := pathDir(t)
	marker := filepath.Join(t.TempDir(), "ran")
	writePluginExecutable(t, filepath.Join(dir, "nucleus-tool"), "#!/bin/sh\necho \"$@\" >> \""+marker+"\"\necho ran\n")
	cfg := filepath.Join(t.TempDir(), "nucleus.yml")
	t.Setenv("NUCLEUS_CONFIG", cfg)

	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ran := func() bool { _, err := os.Stat(marker); return err == nil }

	write("plugins:\n  commands: [other]\n")
	code, _, stderr := runCLI("tool", "x")
	if code == 0 || ran() || !strings.Contains(stderr, `plugins.commands does not list "tool"`) {
		t.Fatalf("an unlisted command: exit %d, ran %t, %q", code, ran(), stderr)
	}
	if code, _, _ := runCLI("help", "tool"); code == 0 || ran() {
		t.Fatalf("help of an unlisted command ran it")
	}

	write("plugins:\n  allow_external: false\n  commands: [tool]\n")
	if code, _, stderr := runCLI("tool"); code == 0 || ran() || !strings.Contains(stderr, "allow_external is false") {
		t.Fatalf("allow_external: false: exit %d, %q", code, stderr)
	}

	// A configuration that declares the block and does not load cannot
	// have its allowlist applied: refused, not run unchecked.
	write("plugins:\n  commands: [tool]\nprot: 9999\n")
	if code, _, stderr := runCLI("tool"); code == 0 || ran() || !strings.Contains(stderr, "declares a plugins block and does not load") {
		t.Fatalf("a broken configuration with a plugins block: exit %d, %q", code, stderr)
	}

	write("plugins:\n  commands: [tool]\n")
	if code, stdout, stderr := runCLI("tool", "x"); code != 0 || !ran() || !strings.Contains(stdout, "ran") {
		t.Fatalf("a listed command: exit %d, %q %q", code, stdout, stderr)
	}
	if code, _, _ := runCLI("help", "tool"); code != 0 {
		t.Fatalf("help of a listed command exited %d", code)
	}
	raw, _ := os.ReadFile(marker)
	if !strings.Contains(string(raw), "--help") {
		t.Errorf("nucleus help tool did not run nucleus-tool --help: %q", raw)
	}
}

// The allowlist is opt-in: a configuration without the block — even one
// that does not load — leaves the dispatch as it was.
func TestExternalCommands_NoAllowlistRunsAsBefore(t *testing.T) {
	skipWithoutShell(t)
	dir := pathDir(t)
	writePluginExecutable(t, filepath.Join(dir, "nucleus-tool"), "#!/bin/sh\necho ran\nexit 3\n")
	if err := os.WriteFile("nucleus.yml", []byte("prot: 9999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := runCLI("tool"); code != 3 || !strings.Contains(stdout, "ran") {
		t.Fatalf("exit %d, %q %q", code, stdout, stderr)
	}
}
