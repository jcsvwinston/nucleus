// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Regression tests for the P0 "exit 0 sin efecto" items of the 2026-08-16
// DX audit that live in the nucleus CLI:
//
//   - DX-3 (A1): `nucleus health --config /tmp/nope.yml` reported
//     `overall ok`, exited 0, and left a stray nucleus.db in the cwd — a
//     typo in an EXPLICIT config path silently ran on defaults.
//   - DX-4 (A2): `nucleus migrate --migrations /tmp/nope up` printed
//     `Migrations applied`, exited 0 — and CREATED the directory it was
//     asked to read.
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An explicitly supplied --config that does not exist must fail loudly and
// name the path — never fall back to defaults in silence.
func TestExplicitMissingConfigFailsLoudly(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yml")

	var stdout, stderr strings.Builder
	err := runHealth([]string{"--config", missing}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatalf("health with a missing explicit --config must fail, got success\nstdout: %s", stdout.String())
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("the error must name the missing path %q, got: %v", missing, err)
	}
}

// Without --config and without nucleus.yml, defaults must keep working —
// the strictness applies only to an explicitly supplied path.
func TestDefaultConfigStillOptional(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	var stdout, stderr strings.Builder
	if err := runHealth(nil, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("health with no config anywhere must run on defaults: %v\nstderr: %s", err, stderr.String())
	}
}

// migrate up over a nonexistent migrations dir must fail without creating it.
func TestMigrateUpDoesNotInventTheMigrationsDir(t *testing.T) {
	cfgPath, _ := writeAdminCLIConfig(t)
	missing := filepath.Join(t.TempDir(), "nope")

	var stdout, stderr strings.Builder
	err := runMigrate([]string{"--config", cfgPath, "--migrations", missing, "up"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatalf("migrate up over a nonexistent dir must fail, got success\nstdout: %s", stdout.String())
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("the error must name the missing dir %q, got: %v", missing, err)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Errorf("migrate up CREATED the directory it was asked to read: %v", statErr)
	}
}
