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
	provider, ok := ParseProviderFromBinary("nucleus-plugin-twilio", GenericBinaryPrefix)
	if !ok || provider != "twilio" {
		t.Fatalf("unexpected generic parse result: ok=%v provider=%q", ok, provider)
	}

	if _, ok := ParseProviderFromBinary("nucleus-hello", GenericBinaryPrefix); ok {
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

	// Skip on macOS due to potential process execution restrictions in test environments
	if runtime.GOOS == "darwin" && os.Getenv("CI") != "true" {
		t.Skip("skipping on macOS outside CI due to process execution restrictions")
	}

	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "nucleus-plugin-acme")
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

	caps, err := ProbeCapabilities(context.Background(), pluginPath, 5*time.Second)
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

	// Skip on macOS due to potential process execution restrictions in test environments
	if runtime.GOOS == "darwin" && os.Getenv("CI") != "true" {
		t.Skip("skipping on macOS outside CI due to process execution restrictions")
	}

	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "nucleus-plugin-textonly")
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

	caps, err := ProbeCapabilities(context.Background(), pluginPath, 5*time.Second)
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

	// Skip on macOS due to potential process execution restrictions in test environments
	if runtime.GOOS == "darwin" && os.Getenv("CI") != "true" {
		t.Skip("skipping on macOS outside CI due to process execution restrictions")
	}

	dir := t.TempDir()
	genericPath := filepath.Join(dir, "nucleus-plugin-twilio")
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

	discovered := DiscoverExternal(dir, 5*time.Second)
	if len(discovered) != 1 {
		t.Fatalf("expected 1 external descriptor, got %d: %v", len(discovered), discovered)
	}

	desc := discovered[0]
	if desc.Provider != "twilio" || desc.Source != SourceExternalGeneric {
		t.Fatalf("unexpected descriptor: %+v", desc)
	}
	if filepath.Clean(desc.BinaryPath) != filepath.Clean(genericPath) {
		t.Fatalf("unexpected generic path: got=%s want=%s", desc.BinaryPath, genericPath)
	}
	if !SupportsCapability(desc, "queue.publish") || !SupportsCapability(desc, "webhook.deliver") {
		t.Fatalf("unexpected generic capabilities: %v", desc.Capabilities)
	}
}

func TestCollectInventory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based executable test is unix-only")
	}

	// Skip on macOS due to potential process execution restrictions in test environments
	if runtime.GOOS == "darwin" && os.Getenv("CI") != "true" {
		t.Skip("skipping on macOS outside CI due to process execution restrictions")
	}

	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "nucleus-plugin-demo")
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

	inventory := CollectInventory(dir, []string{"noop", "smtp", "sendgrid"}, 5*time.Second)
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
