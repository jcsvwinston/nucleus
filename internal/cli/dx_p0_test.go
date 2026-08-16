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
	"regexp"
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

// DX-13: the CLI's config path must validate unknown keys exactly like the
// builder path — `prot: 9999` used to report `overall ok` via the CLI while
// `go run .` said `did you mean port?`.
func TestCLIConfigRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	cfg := "prot: 9999\ndatabases:\n  default:\n    url: sqlite://" + filepath.Join(dir, "x.db") + "\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	err := runHealth([]string{"--config", cfgPath}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatalf("health with an unknown config key must fail, got success\nstdout: %s", stdout.String())
	}
	if !strings.Contains(err.Error(), "prot") || !strings.Contains(strings.ToLower(err.Error()), "did you mean") {
		t.Errorf("the error must name 'prot' with a did-you-mean hint, got: %v", err)
	}
}

// DX-12: the minimal-API page must list every nucleus.* / model.* symbol
// examples/mvc_api actually uses — and stay honest when the example evolves.
func TestMinimalAPIPageMatchesExample(t *testing.T) {
	repoRoot := repoRootForTest(t)
	page, err := os.ReadFile(filepath.Join(repoRoot, "website", "docs", "getting-started", "minimal-api.md"))
	if err != nil {
		t.Fatalf("minimal-api page missing: %v", err)
	}

	used := map[string]struct{}{}
	exDir := filepath.Join(repoRoot, "examples", "mvc_api")
	err = filepath.Walk(exDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, rErr := os.ReadFile(path)
		if rErr != nil {
			return rErr
		}
		for _, m := range regexp.MustCompile(`\b(nucleus|model)\.[A-Z][A-Za-z]*`).FindAllString(string(src), -1) {
			used[m] = struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(used) == 0 {
		t.Fatal("no symbols found in examples/mvc_api — walker broken?")
	}
	for sym := range used {
		if !strings.Contains(string(page), "`"+sym+"`") {
			t.Errorf("minimal-api page does not list %s, which the canonical example uses", sym)
		}
	}
}
