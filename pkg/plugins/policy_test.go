// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPolicy_ZeroValueAllowsEverything(t *testing.T) {
	var p Policy
	if err := p.AllowPlugin("sendgrid", CapabilityMailSend); err != nil {
		t.Errorf("the zero policy refused a plugin: %v", err)
	}
	if err := p.AllowCommand("lint"); err != nil {
		t.Errorf("the zero policy refused a command: %v", err)
	}
	if p.ListsPlugins() || p.ListsCommands() {
		t.Error("the zero policy lists nothing")
	}
}

func TestPolicy_AllowPlugin(t *testing.T) {
	p := Policy{Allowed: []Allowance{
		{Provider: "SendGrid", Capabilities: []string{"Mail.Send"}},
		{Provider: "kafka", Capabilities: []string{CapabilityQueuePublish}},
	}}
	cases := []struct {
		provider, capability string
		allowed              bool
		key                  string
	}{
		{"sendgrid", CapabilityMailSend, true, ""},
		{"sendgrid", "", true, ""}, // listed: may be asked for its capabilities
		{"sendgrid", CapabilityQueuePublish, false, "plugins.allowed"},
		{"kafka", CapabilityQueuePublish, true, ""},
		{"mailgun", CapabilityMailSend, false, "plugins.allowed"},
		{"mailgun", "", false, "plugins.allowed"},
	}
	for _, tc := range cases {
		err := p.AllowPlugin(tc.provider, tc.capability)
		if (err == nil) != tc.allowed {
			t.Errorf("AllowPlugin(%q, %q) = %v, want allowed=%t", tc.provider, tc.capability, err, tc.allowed)
			continue
		}
		var refused *RefusedError
		if err != nil && (!errors.As(err, &refused) || refused.Key != tc.key) {
			t.Errorf("AllowPlugin(%q, %q) = %v, want a RefusedError naming %s", tc.provider, tc.capability, err, tc.key)
		}
	}
}

func TestPolicy_DenyExternalWins(t *testing.T) {
	p := Policy{DenyExternal: true, Allowed: []Allowance{{Provider: "sendgrid", Capabilities: []string{CapabilityMailSend}}}, Commands: []string{"lint"}}
	if err := p.AllowPlugin("sendgrid", CapabilityMailSend); err == nil || !strings.Contains(err.Error(), "plugins.allow_external is false") {
		t.Errorf("a listed plugin under allow_external: false = %v", err)
	}
	if err := p.AllowCommand("lint"); err == nil || !strings.Contains(err.Error(), "plugins.allow_external is false") {
		t.Errorf("a listed command under allow_external: false = %v", err)
	}
}

func TestPolicy_AllowCommand(t *testing.T) {
	p := Policy{Commands: []string{"lint", "Deploy"}}
	if err := p.AllowCommand("deploy"); err != nil {
		t.Errorf("a listed command was refused: %v", err)
	}
	err := p.AllowCommand("wipe")
	if err == nil || !strings.Contains(err.Error(), `plugins.commands does not list "wipe"`) {
		t.Errorf("an unlisted command = %v", err)
	}
	// The command list does not restrict plugins, nor the other way round.
	if err := p.AllowPlugin("sendgrid", CapabilityMailSend); err != nil {
		t.Errorf("plugins.commands restricted a plugin: %v", err)
	}
}

// TestDiscoverAllowed_NeverRunsARefusedBinary: a binary the policy refuses
// is listed, and not executed — not even to read its capabilities.
func TestDiscoverAllowed_NeverRunsARefusedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based executable test is unix-only")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	for _, provider := range []string{"listed", "unlisted"} {
		writePluginRuntimeExecutable(t, filepath.Join(dir, GenericBinaryPrefix+provider), `#!/bin/sh
echo `+provider+` >> "`+marker+`"
echo '{"capabilities":["mail.send"]}'
`)
	}
	got := DiscoverAllowed(dir, 10*time.Second, Policy{Allowed: []Allowance{{Provider: "listed", Capabilities: []string{CapabilityMailSend}}}})
	if len(got) != 2 {
		t.Fatalf("discovered %d, want 2: %+v", len(got), got)
	}
	byProvider := map[string]Descriptor{}
	for _, d := range got {
		byProvider[d.Provider] = d
	}
	if d := byProvider["listed"]; d.Refused != "" || len(d.Capabilities) != 1 {
		t.Errorf("the listed plugin: %+v", d)
	}
	if d := byProvider["unlisted"]; d.Refused == "" || len(d.Capabilities) != 0 {
		t.Errorf("the unlisted plugin must be listed refused and unprobed: %+v", d)
	}
	ran, _ := os.ReadFile(marker)
	if strings.Contains(string(ran), "unlisted") {
		t.Errorf("the refused binary was executed: %q", ran)
	}
}
