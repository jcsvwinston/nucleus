// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// DEP-2026-021 (NU-72) pins the v1 default of session_idle_timeout: zero,
// no expiry by inactivity. At v2.0.0 the default becomes 30m; the 2.0 PR
// changes the first test in the same commit as the default. The second
// test is the mechanism the flip relies on: the loader applies the struct
// defaults first and the file on top, so an explicit zero in the file
// survives a non-zero default — shown here on session_lifetime, whose
// default is already non-zero, because with a zero default "absent" and
// "zero" cannot be told apart.

func TestSessionIdleTimeout_DefaultIsZeroInV1(t *testing.T) {
	if got := DefaultConfig().SessionIdleTimeout; got != 0 {
		t.Fatalf("DefaultConfig().SessionIdleTimeout = %v, want 0 (no expiry by inactivity until v2.0.0)", got)
	}
}

func TestLoadConfig_AnExplicitZeroSurvivesANonZeroDefault(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	absent, err := LoadConfig(write("absent.yml", "port: 8080\n"))
	if err != nil {
		t.Fatalf("LoadConfig(absent): %v", err)
	}
	if absent.SessionLifetime != DefaultConfig().SessionLifetime || absent.SessionLifetime == 0 {
		t.Fatalf("absent key: SessionLifetime = %v, want the default %v", absent.SessionLifetime, DefaultConfig().SessionLifetime)
	}
	zero, err := LoadConfig(write("zero.yml", "port: 8080\nsession_lifetime: 0\nsession_idle_timeout: 0\n"))
	if err != nil {
		t.Fatalf("LoadConfig(zero): %v", err)
	}
	if zero.SessionLifetime != 0 {
		t.Fatalf("explicit zero: SessionLifetime = %v, want 0 (the file must win over the default)", zero.SessionLifetime)
	}
	if zero.SessionIdleTimeout != 0 {
		t.Fatalf("explicit zero: SessionIdleTimeout = %v, want 0", zero.SessionIdleTimeout)
	}
	thirty, err := LoadConfig(write("thirty.yml", "port: 8080\nsession_idle_timeout: 30m\n"))
	if err != nil {
		t.Fatalf("LoadConfig(thirty): %v", err)
	}
	if thirty.SessionIdleTimeout != 30*time.Minute {
		t.Fatalf("SessionIdleTimeout = %v, want 30m", thirty.SessionIdleTimeout)
	}
}
