// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

// NU-102: external plugins ran without an allowlist, and the `plugins.*`
// keys the plugin reference proposed were refused by the strict
// configuration check. The block is now part of the schema, enforced by the
// mail runtime when set, and opt-in until v2.0.0 (DEP-2026-014).

func writePluginsConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nucleus.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPluginsBlock_AcceptedByTheStrictCheck(t *testing.T) {
	cfg, err := LoadConfig(writePluginsConfig(t, `plugins:
  allow_external: true
  allowed:
    - provider: sendgrid
      capabilities: [mail.send]
    - provider: kafka
      capabilities: [queue.publish, webhook.deliver]
  commands: [lint, deploy]
`))
	if err != nil {
		t.Fatalf("the plugins block the reference documents is refused: %v", err)
	}
	p := cfg.Plugins.Policy()
	if p.DenyExternal || len(p.Allowed) != 2 || len(p.Commands) != 2 {
		t.Fatalf("policy = %+v", p)
	}
	if err := p.AllowPlugin("kafka", plugins.CapabilityWebhookDeliver); err != nil {
		t.Errorf("a listed capability is refused: %v", err)
	}
	if err := p.AllowPlugin("mailgun", plugins.CapabilityMailSend); err == nil {
		t.Error("an unlisted provider is allowed")
	}
	if err := p.AllowCommand("wipe"); err == nil {
		t.Error("an unlisted command is allowed")
	}
}

func TestPluginsBlock_DefaultAllowsEverything(t *testing.T) {
	cfg, err := LoadConfig(writePluginsConfig(t, "port: 8080\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins.AllowExternal != nil {
		t.Errorf("allow_external unset must stay nil (true), got %v", *cfg.Plugins.AllowExternal)
	}
	p := cfg.Plugins.Policy()
	if p.ListsPlugins() || p.ListsCommands() {
		t.Errorf("no block must be no allowlist: %+v", p)
	}
	if err := p.AllowPlugin("anything", plugins.CapabilityMailSend); err != nil {
		t.Errorf("the default refused a plugin: %v", err)
	}
	// A Config built by hand, without the defaults, is the same answer.
	if (Config{}).Plugins.Policy().ListsPlugins() {
		t.Error("the zero Config refuses plugins")
	}
}

func TestPluginsBlock_AllowExternalFromFileAndEnv(t *testing.T) {
	cfg, err := LoadConfig(writePluginsConfig(t, "plugins:\n  allow_external: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Plugins.Policy().DenyExternal {
		t.Error("allow_external: false in the file does not deny")
	}

	t.Setenv("NUCLEUS_PLUGINS__ALLOW_EXTERNAL", "false")
	cfg, err = LoadConfig(writePluginsConfig(t, "port: 8080\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Plugins.Policy().DenyExternal {
		t.Error("NUCLEUS_PLUGINS__ALLOW_EXTERNAL=false does not deny")
	}
}

func TestPluginsBlock_RefusesEntriesThatCannotMatch(t *testing.T) {
	cases := map[string]string{
		"no capabilities": "plugins:\n  allowed:\n    - provider: sendgrid\n",
		// A misspelt key inside a list entry is not seen by the key check;
		// it decodes as an entry with no capabilities.
		"misspelt capabilities": "plugins:\n  allowed:\n    - provider: sendgrid\n      capabilites: [mail.send]\n",
		"no provider":           "plugins:\n  allowed:\n    - capabilities: [mail.send]\n",
		"a path for a provider": "plugins:\n  allowed:\n    - provider: /usr/local/bin/nucleus-plugin-x\n      capabilities: [mail.send]\n",
		"not a capability":      "plugins:\n  allowed:\n    - provider: sendgrid\n      capabilities: [send]\n",
		"a path for a command":  "plugins:\n  commands: [./nucleus-x]\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := LoadConfig(writePluginsConfig(t, body))
			if !errors.Is(err, ErrInvalidConfigValue) || !strings.Contains(err.Error(), "plugins.") {
				t.Fatalf("want an invalid-value error naming the plugins key, got %v", err)
			}
		})
	}
}

func TestPluginsBlock_UnknownKeyIsStillRefused(t *testing.T) {
	_, err := LoadConfig(writePluginsConfig(t, "plugins:\n  exec_timeout: 10s\n"))
	if err == nil || !strings.Contains(err.Error(), "plugins.exec_timeout") {
		t.Fatalf("a plugins key that does nothing must stay unknown: %v", err)
	}
}

// mailPluginOnPath puts a nucleus-plugin-<provider> advertising mail.send
// first on PATH; it writes a marker file whenever it is executed.
func mailPluginOnPath(t *testing.T, provider string) (marker string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-based plugin executable is unix-only")
	}
	dir := t.TempDir()
	marker = filepath.Join(dir, "ran")
	script := "#!/bin/sh\necho \"$@\" >> \"" + marker + "\"\n" +
		"if [ \"$1\" = \"capabilities\" ]; then echo '{\"capabilities\":[\"mail.send\"]}'; exit 0; fi\n" +
		"cat >/dev/null\necho '{\"version\":\"v1\",\"ok\":true,\"output\":{\"accepted\":true}}'\n"
	if err := os.WriteFile(filepath.Join(dir, plugins.GenericBinaryPrefix+provider), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
}

func TestPluginsBlock_TheAppRefusesAnUnlistedMailPlugin(t *testing.T) {
	marker := mailPluginOnPath(t, "allowtest")
	cfg := testAppConfig()
	cfg.MailDriver = "allowtest"
	cfg.Plugins = PluginsConfig{Allowed: []PluginAllowance{{Provider: "sendgrid", Capabilities: []string{plugins.CapabilityMailSend}}}}
	_, err := New(cfg)
	if err == nil || !strings.Contains(err.Error(), `plugins.allowed has no entry for provider "allowtest"`) {
		t.Fatalf("New with an unlisted mail plugin: %v", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("the refused plugin was executed")
	}

	denied := false
	cfg.Plugins = PluginsConfig{AllowExternal: &denied}
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "plugins.allow_external is false") {
		t.Fatalf("New under allow_external: false: %v", err)
	}
}

func TestPluginsBlock_TheAppRunsAListedMailPlugin(t *testing.T) {
	mailPluginOnPath(t, "allowtest")
	cfg := testAppConfig()
	cfg.MailDriver = "allowtest"
	cfg.LogFormat = "text"
	cfg.Plugins = PluginsConfig{Allowed: []PluginAllowance{{Provider: "allowtest", Capabilities: []string{plugins.CapabilityMailSend}}}}
	var a *App
	out := captureStdout(t, func() {
		var err error
		if a, err = New(cfg); err != nil {
			t.Fatalf("New with a listed mail plugin: %v", err)
		}
	})
	defer func() { _ = a.Shutdown(context.Background()) }()
	if lines := linesContaining(out, "DEP-2026-014"); len(lines) != 0 {
		t.Errorf("a listed plugin must not carry the deprecation warning:\n%s", strings.Join(lines, "\n"))
	}
}

// Before v2.0.0 an unlisted plugin still runs (QADR-0010: no behaviour flip
// before the major), and the boot log says once that it will not.
func TestPluginsBlock_NoAllowlistRunsAndWarns(t *testing.T) {
	mailPluginOnPath(t, "allowtest")
	cfg := testAppConfig()
	cfg.MailDriver = "allowtest"
	cfg.LogFormat = "text"
	cfg.LogLevel = "info"
	var a *App
	out := captureStdout(t, func() {
		var err error
		if a, err = New(cfg); err != nil {
			t.Fatalf("an application that started before the allowlist no longer starts: %v", err)
		}
	})
	defer func() { _ = a.Shutdown(context.Background()) }()
	lines := linesContaining(out, "external mail plugin runs without an allowlist")
	if len(lines) != 1 {
		t.Fatalf("want one warning line, got %d:\n%s", len(lines), out)
	}
	for _, want := range []string{"level=WARN", "driver=allowtest", "plugins.allowed", "DEP-2026-014", "v2.0.0"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the warning does not say %q:\n%s", want, lines[0])
		}
	}
}
