// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestMakeMigrationsRendersThroughTheSiblingQuarkCLI is the one run of
// `nucleus makemigrations` against the real quark CLI this repository can
// make: the framework module cannot require quark, so the CLI is built
// from the sibling checkout next to this repository (cmd/quark, a module
// of its own, linked to the library beside it through a workspace), and
// the test is skipped, not faked, when the checkout is not there. It
// scaffolds a project, writes a model with `generate model`, and renders
// the pair in the five dialects the framework names. CI's suite-starter
// lane checks out the latest release of quark — the one a user's `go
// install …/cmd/quark@latest` resolves — and sets
// NUCLEUS_REQUIRE_SIBLING_CHECKOUTS, which turns the skip into a failure.
func TestMakeMigrationsRendersThroughTheSiblingQuarkCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the quark CLI from the sibling checkout; skipped with -short")
	}
	repoRoot := repoRootForTest(t)
	_, quark := siblingCheckouts(repoRoot)
	if quark == "" {
		if os.Getenv("NUCLEUS_REQUIRE_SIBLING_CHECKOUTS") != "" {
			t.Fatalf("NUCLEUS_REQUIRE_SIBLING_CHECKOUTS is set and there is no quark checkout next to %s (or under NUCLEUS_SIBLING_CHECKOUTS)", repoRoot)
		}
		t.Skip("no quark checkout next to this repository (set NUCLEUS_SIBLING_CHECKOUTS); CI's suite-starter lane runs this")
	}

	// cmd/quark requires the library by version and carries no replace
	// (go install forbids one); a workspace links it to the checkout
	// beside it, the way quark's CI and release jobs build it
	// (scripts/ci/link_workspace.sh there): the two modules in use, and a
	// VERSIONED replace of the library at the version the CLI's go.mod
	// names, so the floor is served from the tree even when it names a
	// tag the proxy does not have yet.
	work := t.TempDir()
	cliDir := filepath.Join(quark, "cmd", "quark")
	workFile := filepath.Join(work, "go.work")
	runGoCommand(t, work, "work", "init", quark, cliDir)
	cliMod, err := os.ReadFile(filepath.Join(cliDir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if m := regexp.MustCompile(`(?m)^\s*github\.com/jcsvwinston/quark (v\S+)`).FindSubmatch(cliMod); m != nil {
		runGoCommand(t, work, "work", "edit", "-replace", "github.com/jcsvwinston/quark@"+string(m[1])+"="+quark)
	}
	bin := filepath.Join(work, "quark")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = filepath.Join(quark, "cmd", "quark")
	build.Env = append(os.Environ(), "GOWORK="+workFile)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build of the sibling's cmd/quark: %v\n%s", err, out)
	}
	t.Setenv(quarkCLIEnv, bin)

	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := runNew([]string{"mmcheck", "--out", outDir, "--template", "api", "--module", "example.com/mmcheck", "--offline"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew: %v\nstderr: %s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, "mmcheck")
	if err := runGenerate([]string{"model", "Gear", "--out", projectDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("generate model: %v\nstderr: %s", err, stderr.String())
	}

	// What each engine's DDL must carry for the generated model's key —
	// the mapping is quark's; this only checks the pair is that engine's.
	marks := map[string]string{
		"sqlite":     "INTEGER PRIMARY KEY AUTOINCREMENT",
		"postgresql": "BIGSERIAL PRIMARY KEY",
		"mysql":      "BIGINT AUTO_INCREMENT PRIMARY KEY",
		"mssql":      "IDENTITY(1,1) PRIMARY KEY",
		"oracle":     "GENERATED ALWAYS AS IDENTITY PRIMARY KEY",
	}
	for _, dialect := range []string{"sqlite", "postgresql", "mysql", "mssql", "oracle"} {
		t.Run(dialect, func(t *testing.T) {
			migDir := filepath.Join(t.TempDir(), "migrations")
			name := "init_" + dialect
			_, args, err := canonicalizeCommand("makemigrations", []string{"--config", filepath.Join(projectDir, "nucleus.yml"), "--migrations", migDir, "--dialect", dialect, name})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(args, " "), "--from-models "+filepath.Join(projectDir, "internal", "models")) {
				t.Fatalf("the alias did not find the project's models: %q", args)
			}
			var stdout, stderr bytes.Buffer
			if err := runMigrate(args, strings.NewReader(""), &stdout, &stderr); err != nil {
				t.Fatalf("makemigrations %s: %v\nstderr: %s", dialect, err, stderr.String())
			}
			up, down := readPair(t, migDir, name)
			for _, want := range []string{"with quark v", "for " + dialect + ".", "-- Models: Gear -> gears.", "CREATE TABLE", "gears", marks[dialect], "name "} {
				if !strings.Contains(up, want) {
					t.Errorf("%s up file lacks %q:\n%s", dialect, want, up)
				}
			}
			if !strings.Contains(down, "DROP TABLE") || !strings.Contains(down, "gears") {
				t.Errorf("%s down file:\n%s", dialect, down)
			}
			if dialect == "oracle" && !strings.Contains(up, ")\n/\n") {
				t.Errorf("oracle statements end with a / line:\n%s", up)
			}
			if !strings.Contains(stdout.String(), "1 model(s), quark v") {
				t.Errorf("stdout:\n%s", stdout.String())
			}
		})
	}
}
