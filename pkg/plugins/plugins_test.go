package plugins

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseProviderFromBinary(t *testing.T) {
	provider, ok := ParseProviderFromBinary("goframe-plugin-twilio", GenericBinaryPrefix)
	if !ok || provider != "twilio" {
		t.Fatalf("unexpected generic parse result: ok=%v provider=%q", ok, provider)
	}

	provider, ok = ParseProviderFromBinary("goframe-mail-mailgun", LegacyMailBinaryPrefix)
	if !ok || provider != "mailgun" {
		t.Fatalf("unexpected legacy parse result: ok=%v provider=%q", ok, provider)
	}

	if _, ok := ParseProviderFromBinary("goframe-hello", GenericBinaryPrefix); ok {
		t.Fatal("expected invalid binary name to be rejected")
	}
}

func TestBuiltinMailDescriptorsFromProviders(t *testing.T) {
	descriptors := BuiltinMailDescriptorsFromProviders([]string{"noop", "smtp", "sendgrid"})
	if len(descriptors) != 3 {
		t.Fatalf("expected exactly 3 built-in mail descriptors, got %d", len(descriptors))
	}

	foundNoop := false
	for _, desc := range descriptors {
		if desc.Source != SourceBuiltinMail {
			t.Fatalf("unexpected built-in descriptor source: %s", desc.Source)
		}
		if desc.Provider == "noop" {
			foundNoop = true
			if !SupportsCapability(desc, "mail.send") {
				t.Fatalf("expected noop provider to support mail.send, got: %v", desc.Capabilities)
			}
		}
	}
	if !foundNoop {
		t.Fatalf("expected noop provider in built-in descriptors, got: %v", descriptors)
	}
}

func TestProbeCapabilities(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based executable test is unix-only")
	}

	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "goframe-plugin-acme")
	writeExecutable(t, pluginPath, `#!/bin/sh
if [ "$1" = "capabilities" ] && [ "$2" = "--json" ]; then
  echo '{"capabilities":["queue.publish","mail.send"]}'
  exit 0
fi
if [ "$1" = "capabilities" ]; then
  echo "queue.publish mail.send"
  exit 0
fi
exit 1
`)

	caps, err := ProbeCapabilities(context.Background(), pluginPath, time.Second)
	if err != nil {
		t.Fatalf("ProbeCapabilities failed: %v", err)
	}
	if len(caps) != 2 || caps[0] != "mail.send" || caps[1] != "queue.publish" {
		t.Fatalf("unexpected capabilities: %v", caps)
	}
}

func TestProbeCapabilitiesFallbackToPlainText(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based executable test is unix-only")
	}

	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "goframe-plugin-textonly")
	writeExecutable(t, pluginPath, `#!/bin/sh
if [ "$1" = "capabilities" ] && [ "$2" = "--json" ]; then
  echo "not-json"
  exit 0
fi
if [ "$1" = "capabilities" ]; then
  echo "webhook.deliver"
  exit 0
fi
exit 1
`)

	caps, err := ProbeCapabilities(context.Background(), pluginPath, time.Second)
	if err != nil {
		t.Fatalf("ProbeCapabilities fallback failed: %v", err)
	}
	if len(caps) != 1 || caps[0] != "webhook.deliver" {
		t.Fatalf("unexpected fallback capabilities: %v", caps)
	}
}

func TestDiscoverExternal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based executable test is unix-only")
	}

	dir := t.TempDir()
	genericPath := filepath.Join(dir, "goframe-plugin-twilio")
	writeExecutable(t, genericPath, `#!/bin/sh
if [ "$1" = "capabilities" ] && [ "$2" = "--json" ]; then
  echo '{"capabilities":["webhook.deliver","queue.publish"]}'
  exit 0
fi
if [ "$1" = "capabilities" ]; then
  echo "webhook.deliver queue.publish"
  exit 0
fi
exit 1
`)

	legacyPath := filepath.Join(dir, "goframe-mail-mailgun")
	writeExecutable(t, legacyPath, "#!/bin/sh\nexit 0\n")

	discovered := DiscoverExternal(dir, time.Second)
	if len(discovered) != 2 {
		t.Fatalf("expected 2 external descriptors, got %d: %v", len(discovered), discovered)
	}

	var foundGeneric bool
	var foundLegacy bool
	for _, desc := range discovered {
		switch {
		case desc.Provider == "twilio" && desc.Source == SourceExternalGeneric:
			foundGeneric = true
			if filepath.Clean(desc.BinaryPath) != filepath.Clean(genericPath) {
				t.Fatalf("unexpected generic path: got=%s want=%s", desc.BinaryPath, genericPath)
			}
			if !SupportsCapability(desc, "queue.publish") || !SupportsCapability(desc, "webhook.deliver") {
				t.Fatalf("unexpected generic capabilities: %v", desc.Capabilities)
			}
		case desc.Provider == "mailgun" && desc.Source == SourceExternalLegacyMail:
			foundLegacy = true
			if filepath.Clean(desc.BinaryPath) != filepath.Clean(legacyPath) {
				t.Fatalf("unexpected legacy path: got=%s want=%s", desc.BinaryPath, legacyPath)
			}
			if !SupportsCapability(desc, "mail.send") {
				t.Fatalf("expected legacy mail plugin to support mail.send: %v", desc.Capabilities)
			}
		}
	}
	if !foundGeneric || !foundLegacy {
		t.Fatalf("expected both generic and legacy plugins discovered, got: %v", discovered)
	}
}

func TestCollectInventory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based executable test is unix-only")
	}

	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "goframe-plugin-demo")
	writeExecutable(t, pluginPath, `#!/bin/sh
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

	inventory := CollectInventory(dir, []string{"noop", "smtp", "sendgrid"}, time.Second)
	if len(inventory) == 0 {
		t.Fatal("expected non-empty inventory")
	}

	foundBuiltin := false
	foundExternal := false
	for _, desc := range inventory {
		if desc.Source == SourceBuiltinMail && desc.Provider == "noop" {
			foundBuiltin = true
		}
		if desc.Source == SourceExternalGeneric && desc.Provider == "demo" {
			foundExternal = true
		}
	}
	if !foundBuiltin {
		t.Fatalf("expected built-in noop provider in inventory, got: %v", inventory)
	}
	if !foundExternal {
		t.Fatalf("expected external demo provider in inventory, got: %v", inventory)
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o755); err != nil {
		t.Fatalf("write executable failed: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatalf("chmod executable failed: %v", err)
		}
	}
}
