package app

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// TestWarnLegacyStorageKeys_DeviationEmitsWarn confirms that actively
// configuring the legacy flat storage keys (any deviation from the
// DefaultConfig values) emits a one-time deprecation WARN pointing
// operators at the nested storage.* keys.
func TestWarnLegacyStorageKeys_DeviationEmitsWarn(t *testing.T) {
	// The deprecation warning is guarded by a process-wide sync.Once; reset it
	// so this test deterministically observes the emission.
	legacyStorageKeysDeprecationOnce = sync.Once{}
	t.Cleanup(func() { legacyStorageKeysDeprecationOnce = sync.Once{} })

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cfg := DefaultConfig()
	cfg.StoragePath = "/srv/uploads"
	warnLegacyStorageKeys(logger, &cfg)

	out := buf.String()
	if !strings.Contains(out, "storage_driver/storage_path are deprecated") || !strings.Contains(out, "storage.local.path") {
		t.Fatalf("expected deprecation WARN mentioning legacy and nested keys, got: %s", out)
	}
}

// TestWarnLegacyStorageKeys_DefaultsEmitNoWarn confirms that the
// DefaultConfig-populated legacy values do not trigger the WARN — presence
// of the defaults is not a signal that the deployment uses the legacy keys.
func TestWarnLegacyStorageKeys_DefaultsEmitNoWarn(t *testing.T) {
	legacyStorageKeysDeprecationOnce = sync.Once{}
	t.Cleanup(func() { legacyStorageKeysDeprecationOnce = sync.Once{} })

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	defaults := DefaultConfig()
	warnLegacyStorageKeys(logger, &defaults)

	if strings.Contains(buf.String(), "deprecated") {
		t.Fatalf("default legacy values must not emit a deprecation WARN, got: %s", buf.String())
	}
}

// TestWarnLegacyStorageKeys_NilTolerant mirrors resolveRBACPolicyFile's
// contract: nil logger or nil config must not panic.
func TestWarnLegacyStorageKeys_NilTolerant(t *testing.T) {
	legacyStorageKeysDeprecationOnce = sync.Once{}
	t.Cleanup(func() { legacyStorageKeysDeprecationOnce = sync.Once{} })

	def := DefaultConfig()
	warnLegacyStorageKeys(nil, &def)
	warnLegacyStorageKeys(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), nil)
}
