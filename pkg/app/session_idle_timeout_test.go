// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// DEP-2026-021 (NU-72): since v2.0.0 session_idle_timeout ships as 30m, the
// inactivity timeout ASVS V3.3.2 asks for; in v1 it shipped as zero. The
// first test pins the default. The second is the mechanism the flip relies
// on: the loader applies the struct defaults first and the file on top, so
// an explicit zero in the file survives the non-zero default — shown on
// session_lifetime as well as on session_idle_timeout itself.

func TestSessionIdleTimeout_DefaultIsThirtyMinutes(t *testing.T) {
	if got := DefaultConfig().SessionIdleTimeout; got != 30*time.Minute {
		t.Fatalf("DefaultConfig().SessionIdleTimeout = %v, want 30m (DEP-2026-021)", got)
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
