// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/internal/cli"
	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

// The example plugin is tested the way an application runs it: built into
// an executable, put on PATH, and reached through the real runtime — the
// mail sender `mail_driver: maildir` builds, and `nucleus plugin test
// --execute`. Nothing here calls deliver directly.

// buildPlugin builds this package into dir/nucleus-plugin-maildir and puts
// dir first on PATH, with MAILDIR pointing at a fresh Maildir it returns.
func buildPlugin(t *testing.T) (bin, maildirPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the PATH lookup below assumes a POSIX executable name")
	}
	dir := t.TempDir()
	bin = filepath.Join(dir, plugins.GenericBinaryPrefix+"maildir")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build the example plugin: %v\n%s", err, out)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	maildirPath = filepath.Join(t.TempDir(), "Maildir")
	t.Setenv("MAILDIR", maildirPath)
	return bin, maildirPath
}

// delivered reads the messages in the Maildir's new/.
func delivered(t *testing.T, maildirPath string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(maildirPath, "new"))
	if err != nil {
		t.Fatalf("read the Maildir: %v", err)
	}
	var out []string
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(maildirPath, "new", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(raw))
	}
	if left, _ := os.ReadDir(filepath.Join(maildirPath, "tmp")); len(left) != 0 {
		t.Errorf("tmp/ still holds %d file(s): a delivery must end in new/", len(left))
	}
	return out
}

func TestAdvertisesMailSend(t *testing.T) {
	bin, _ := buildPlugin(t)
	caps, err := plugins.ProbeCapabilities(context.Background(), bin, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(caps) != 1 || caps[0] != plugins.CapabilityMailSend {
		t.Fatalf("capabilities = %v, want [mail.send]", caps)
	}
}

// TestDeliversThroughTheMailRuntime is the path an application takes:
// `mail_driver: maildir` resolves to the binary on PATH, and Send hands it
// the envelope.
func TestDeliversThroughTheMailRuntime(t *testing.T) {
	_, maildirPath := buildPlugin(t)
	sender, err := mail.NewSender(mail.Config{Driver: "maildir", Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("mail_driver: maildir does not resolve to the plugin: %v", err)
	}
	err = sender.Send(context.Background(), mail.Message{
		From:    "app@example.test",
		To:      []string{"ana@example.test", "luis@example.test"},
		Subject: "Confirmación de registro",
		Body:    "Hola Ana",
		Headers: map[string]string{"X-Campaign": "welcome"},
	})
	if err != nil {
		t.Fatalf("Send through the plugin: %v", err)
	}
	got := delivered(t, maildirPath)
	if len(got) != 1 {
		t.Fatalf("%d message(s) in new/, want 1", len(got))
	}
	for _, want := range []string{
		"From: app@example.test\n",
		"To: ana@example.test, luis@example.test\n",
		"Subject: =?utf-8?q?Confirmaci=C3=B3n_de_registro?=\n",
		"X-Campaign: welcome\n",
		"X-Nucleus-Request-Id: req_",
		"\n\nHola Ana\n",
	} {
		if !strings.Contains(got[0], want) {
			t.Errorf("the delivered message lacks %q:\n%s", want, got[0])
		}
	}
}

// TestRefusesWhatTheContractCannotCarry: a header with a line break is a
// validation failure — exit 10, a non-retriable error the host reads, and
// nothing delivered.
func TestRefusesWhatTheContractCannotCarry(t *testing.T) {
	bin, maildirPath := buildPlugin(t)
	req, err := plugins.NewRequestEnvelope("maildir", plugins.CapabilityMailSend, 10*time.Second, plugins.MailSendPayload{
		From: "app@example.test", To: []string{"ana@example.test"}, Subject: "hi\r\nBcc: everyone@example.test", Body: "x",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = plugins.ExecuteRequest(context.Background(), bin, req, 10*time.Second)
	var execErr *plugins.ExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("want an ExecutionError, got %v", err)
	}
	if execErr.ExitCode != plugins.ExitCodeValidation || execErr.Code != "INVALID_MESSAGE" || execErr.Retriable {
		t.Errorf("exit %d code %q retriable %t; want exit 10, INVALID_MESSAGE, not retriable", execErr.ExitCode, execErr.Code, execErr.Retriable)
	}
	if !strings.Contains(execErr.Stderr, "subject contains a line break") {
		t.Errorf("stderr does not say why: %q", execErr.Stderr)
	}
	if _, statErr := os.Stat(filepath.Join(maildirPath, "new")); statErr == nil {
		if got := delivered(t, maildirPath); len(got) != 0 {
			t.Errorf("a refused message was delivered: %v", got)
		}
	}
}

// TestUnwritableMaildirIsTransient: a Maildir that cannot be created is a
// transient failure (exit 20) the host may retry.
func TestUnwritableMaildirIsTransient(t *testing.T) {
	bin, _ := buildPlugin(t)
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAILDIR", filepath.Join(blocker, "Maildir"))
	req, _ := plugins.NewRequestEnvelope("maildir", plugins.CapabilityMailSend, 10*time.Second,
		plugins.MailSendPayload{From: "a@example.test", To: []string{"b@example.test"}, Subject: "s", Body: "b"}, nil)
	_, err := plugins.ExecuteRequest(context.Background(), bin, req, 10*time.Second)
	var execErr *plugins.ExecutionError
	if !errors.As(err, &execErr) || execErr.ExitCode != plugins.ExitCodeTransient || !execErr.Retriable {
		t.Fatalf("want exit 20, retriable; got %v", err)
	}
}

// TestPassesPluginTestExecute: the CLI's contract check sends the plugin a
// real envelope and reads a response that echoes its request id; the sample
// message lands in the Maildir.
func TestPassesPluginTestExecute(t *testing.T) {
	_, maildirPath := buildPlugin(t)
	t.Chdir(t.TempDir()) // no nucleus.yml: the defaults, no allowlist
	var out, errb bytes.Buffer
	code := cli.Run([]string{"plugin", "test", "--provider", "maildir", "--execute", "--timeout", "30s", "--json"}, strings.NewReader(""), &out, &errb)
	if code != 0 {
		t.Fatalf("nucleus plugin test --execute exited %d:\n%s%s", code, out.String(), errb.String())
	}
	var report struct {
		Status string `json:"status"`
		Checks []struct {
			Capability string `json:"capability"`
			Status     string `json:"status"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode the report: %v\n%s", err, out.String())
	}
	if report.Status != "ok" || len(report.Checks) != 1 || report.Checks[0].Capability != plugins.CapabilityMailSend || report.Checks[0].Status != "ok" {
		t.Fatalf("report: %s", out.String())
	}
	if got := delivered(t, maildirPath); len(got) != 1 || !strings.Contains(got[0], "Subject: nucleus plugin test") {
		t.Fatalf("the sample message was not delivered: %v", got)
	}
}

// TestRefusedByTheAllowlist: an application whose configuration lists other
// plugins does not run this one — the CLI refuses it without executing it,
// and the mail runtime refuses the driver.
func TestRefusedByTheAllowlist(t *testing.T) {
	_, maildirPath := buildPlugin(t)
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("nucleus.yml", []byte("plugins:\n  allowed:\n    - provider: sendgrid\n      capabilities: [mail.send]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := cli.Run([]string{"plugin", "test", "--provider", "maildir", "--execute", "--json"}, strings.NewReader(""), &out, &errb)
	if code == 0 || !strings.Contains(out.String(), "plugins.allowed has no entry for provider") {
		t.Fatalf("plugin test of an unlisted plugin: exit %d\n%s%s", code, out.String(), errb.String())
	}
	_, err := mail.NewSender(mail.Config{Driver: "maildir", Plugins: plugins.Policy{Allowed: []plugins.Allowance{{Provider: "sendgrid", Capabilities: []string{"mail.send"}}}}})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("mail.NewSender for an unlisted plugin: %v", err)
	}
	if _, statErr := os.Stat(maildirPath); statErr == nil {
		t.Fatal("the refused plugin delivered something")
	}
}
