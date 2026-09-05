// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// `nucleus generate module --mount` and the test it emits. The promise
// of the slice is "Mount() is the whole integration"; before --mount that
// line was still the person's to write, the slice shipped no test (`go
// test ./...` printed "[no test files]"), and every generate kind dropped
// an OpenAPI aggregator the slice never used. These tests pin the three
// fixes: the composition root is edited, the emitted test boots the slice
// and passes, and internal/contracts appears only for `generate resource`.
package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scaffoldOfflineProject runs `nucleus new` with --offline and pins the
// result to the local checkout, the fixture every test here starts from.
func scaffoldOfflineProject(t *testing.T, name string) string {
	t.Helper()
	repoRoot := repoRootForTest(t)
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	args := []string{name, "--out", outDir, "--offline", "--template", "mvc", "--module", "example.com/" + name}
	if err := runNew(args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew: %v\nstderr: %s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, name)
	pinGoModToLocalNucleus(t, projectDir, repoRoot)
	return projectDir
}

// runGoInProject is runGoCommand with the module-mode flag the other
// executable-scaffold tests set, returning the combined output.
func runGoInProject(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s (in %s) failed: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// The funnel this PR shortens: `nucleus new x`, `nucleus generate module
// notes --mount`, `go run .` — no hand edit of main.go in between. The
// project must build as edited, and `go test ./...` must run the emitted
// test (which boots the slice in-process) rather than report no test
// files. The default policy is read-only, so the emitted test expects the
// anonymous POST to be refused; the second module, generated with
// --with-policy, expects it to land — both expectations are baked into
// the generated file and proven here by running it.
func TestGenerateModuleMountEditsMainAndEmitsPassingTest(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and tests a scaffolded app; skipped with -short")
	}
	projectDir := scaffoldOfflineProject(t, "myapp")

	var stdout, stderr bytes.Buffer
	if err := runGenerate([]string{"module", "notes", "--out", projectDir, "--mount"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate module --mount: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Mounted in main.go:  Mount(notes.Module())") {
		t.Errorf("--mount must report the edit, got:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "test: "+filepath.Join(projectDir, "internal", "notes", "module_test.go")) {
		t.Errorf("the generator must report the emitted test, got:\n%s", stdout.String())
	}

	mainSrc := readFile(t, filepath.Join(projectDir, "main.go"))
	code := stripComments(mainSrc)
	if !strings.Contains(code, "\"example.com/myapp/internal/notes\"") {
		t.Errorf("main.go must import the module package:\n%s", mainSrc)
	}
	if n := strings.Count(code, "Mount(notes.Module())"); n != 1 {
		t.Errorf("main.go must mount the module exactly once in code, found %d:\n%s", n, mainSrc)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "internal", "contracts", "contracts.go")); !os.IsNotExist(err) {
		t.Errorf("generate module must not create internal/contracts/contracts.go (stat err=%v)", err)
	}

	// A second slice, open policy, also mounted: two Mount calls in the
	// chain, both modules booted by the same composition root.
	stdout.Reset()
	if err := runGenerate([]string{"module", "widget", "--out", projectDir, "--mount", "--with-policy"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate second module --mount: %v\nstderr: %s", err, stderr.String())
	}

	runGoInProject(t, projectDir, "mod", "tidy")
	runGoInProject(t, projectDir, "build", "./...")
	out := runGoInProject(t, projectDir, "test", "-count=1", "./...")
	for _, pkg := range []string{"example.com/myapp/internal/notes", "example.com/myapp/internal/widget"} {
		if !strings.Contains(out, "ok  \t"+pkg) {
			t.Errorf("go test ./... must run and pass the emitted test of %s, got:\n%s", pkg, out)
		}
	}
	if strings.Contains(out, "internal/notes\t[no test files]") {
		t.Errorf("the slice must ship a test; go test reported none:\n%s", out)
	}
	// Under -short the emitted test steps aside instead of booting.
	short := runGoInProject(t, projectDir, "test", "-short", "-count=1", "-v", "./internal/notes/")
	if !strings.Contains(short, "--- SKIP: TestModuleServesItsResource") {
		t.Errorf("the emitted test must skip under -short, got:\n%s", short)
	}
}

// The aggregator (internal/contracts/contracts.go) is what `generate
// resource` registers its contract file into; every other kind used to
// drop it too, so a project that asked for one model woke up with an
// OpenAPI package it never wanted. Only resource creates it now.
func TestGenerateContractsAggregatorOnlyForResource(t *testing.T) {
	projectDir := scaffoldOfflineProject(t, "aggr")
	aggregator := filepath.Join(projectDir, "internal", "contracts", "contracts.go")

	var stdout, stderr bytes.Buffer
	for _, args := range [][]string{
		{"module", "note", "--out", projectDir},
		{"model", "Tag", "--out", projectDir},
		{"migration", "add_index", "--out", projectDir},
		{"handler", "Ping", "--out", projectDir},
	} {
		if err := runGenerate(args, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runGenerate %v: %v\nstderr: %s", args, err, stderr.String())
		}
		if _, err := os.Stat(aggregator); !os.IsNotExist(err) {
			t.Fatalf("generate %s created %s (stat err=%v)", args[0], aggregator, err)
		}
	}
	if err := runGenerate([]string{"resource", "Widget", "--out", projectDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate resource: %v\nstderr: %s", err, stderr.String())
	}
	if _, err := os.Stat(aggregator); err != nil {
		t.Fatalf("generate resource must create the contracts aggregator its contract registers into: %v", err)
	}
}

// A hand-written composition root — nucleus.Run over the direct struct —
// has no chain for --mount to edit. The slice is still written; the
// command prints the two lines to add and fails, so a script notices.
func TestGenerateModuleMountRefusesHandWrittenMain(t *testing.T) {
	projectDir := scaffoldOfflineProject(t, "handmade")
	mainPath := filepath.Join(projectDir, "main.go")
	handmade := `package main

import (
	"log"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"

	_ "github.com/jcsvwinston/nucleus/drivers/sqlite"
)

func main() {
	if err := nucleus.Run(nucleus.App{}); err != nil {
		log.Fatal(err)
	}
}
`
	if err := os.WriteFile(mainPath, []byte(handmade), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := runGenerate([]string{"module", "notes", "--out", projectDir, "--mount"}, strings.NewReader(""), &stdout, &stderr)
	if !errors.Is(err, errNoBuilderChain) {
		t.Fatalf("want errNoBuilderChain, got %v\nstdout: %s", err, stdout.String())
	}
	for _, want := range []string{"import \"example.com/handmade/internal/notes\"", "nucleus.New().Mount(notes.Module())"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("the refusal must print the manual step %q, got:\n%s", want, stdout.String())
		}
	}
	if got := readFile(t, mainPath); got != handmade {
		t.Errorf("a refused --mount must leave main.go untouched:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "internal", "notes", "module.go")); err != nil {
		t.Errorf("the slice must still be written when only the mount is refused: %v", err)
	}
}

// Running --mount twice (the second time with --force to rewrite the
// slice) must not mount the module twice: a duplicate module name fails
// boot, and a tool that breaks the build on its second run is not a tool.
func TestGenerateModuleMountIsIdempotent(t *testing.T) {
	projectDir := scaffoldOfflineProject(t, "twice")
	var stdout, stderr bytes.Buffer
	for i := 0; i < 2; i++ {
		args := []string{"module", "notes", "--out", projectDir, "--mount"}
		if i == 1 {
			args = append(args, "--force")
		}
		if err := runGenerate(args, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("run %d: %v\nstderr: %s", i+1, err, stderr.String())
		}
	}
	if !strings.Contains(stdout.String(), "Already mounted in main.go") {
		t.Errorf("the second run must report the existing mount, got:\n%s", stdout.String())
	}
	code := stripComments(readFile(t, filepath.Join(projectDir, "main.go")))
	if n := strings.Count(code, "Mount(notes.Module())"); n != 1 {
		t.Errorf("main.go mounts the module %d times, want 1:\n%s", n, code)
	}
	if n := strings.Count(code, "\"example.com/twice/internal/notes\""); n != 1 {
		t.Errorf("main.go imports the module %d times, want 1:\n%s", n, code)
	}
}

// --mount and --data belong to the module kind; on any other kind they
// are a mistake to report, not a flag to ignore.
func TestGenerateMountAndDataAreModuleOnly(t *testing.T) {
	projectDir := scaffoldOfflineProject(t, "flags")
	var stdout, stderr bytes.Buffer
	if err := runGenerate([]string{"resource", "Widget", "--out", projectDir, "--mount"}, strings.NewReader(""), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "--mount applies to `generate module` only") {
		t.Errorf("resource --mount: want a usage error, got %v", err)
	}
	if err := runGenerate([]string{"model", "Tag", "--out", projectDir, "--data", "quark"}, strings.NewReader(""), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "--data applies to `generate module` only") {
		t.Errorf("model --data quark: want a usage error, got %v", err)
	}
	if err := runGenerate([]string{"module", "note", "--out", projectDir, "--data", "gorm"}, strings.NewReader(""), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "unsupported --data") {
		t.Errorf("module --data gorm: want an unsupported-value error, got %v", err)
	}
}

// --data quark renders the slice's storage on the Quark ORM over the
// framework-managed pool. The framework module does not require Quark, so
// the generator prints the `go get` the project needs; the module reports
// a client that cannot be derived instead of panicking.
func TestGenerateModuleDataQuarkRendersQuarkStorage(t *testing.T) {
	projectDir := scaffoldOfflineProject(t, "qapp")
	var stdout, stderr bytes.Buffer
	if err := runGenerate([]string{"module", "items", "--out", projectDir, "--data", "quark", "--mount"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate --data quark: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "go get github.com/jcsvwinston/quark github.com/jcsvwinston/quark/drivers/sqlite") {
		t.Errorf("the generator must print the quark modules to get, got:\n%s", stdout.String())
	}
	storage := readFile(t, filepath.Join(projectDir, "internal", "items", "items.go"))
	for _, want := range []string{`"github.com/jcsvwinston/quark"`, `quark.NewWithDB("sqlite", db)`, `quark.For[Record]`, `func (Record) TableName() string { return "items" }`} {
		if !strings.Contains(storage, want) {
			t.Errorf("quark storage is missing %q:\n%s", want, storage)
		}
	}
	if strings.Contains(storage, "database/sql\"\n") && strings.Contains(storage, "QueryContext") {
		t.Errorf("quark storage must not fall back to hand-written SQL:\n%s", storage)
	}
	module := readFile(t, filepath.Join(projectDir, "internal", "items", "module.go"))
	if !strings.Contains(module, "if storage, err = NewStorage(db); err != nil {") {
		t.Errorf("module.go must handle the quark client derivation error:\n%s", module)
	}
	if !strings.Contains(readFile(t, filepath.Join(projectDir, "internal", "items", "module_test.go")), "items.Module()") {
		t.Errorf("the quark slice must ship the same booted test")
	}
}

// The Quark variant compiles and its emitted test passes — proven against
// a Quark checkout, which this repository does not carry (the framework
// module must not require the ORM). Set NUCLEUS_QUARK_DIR to run it; the
// default lane skips, it never passes green on nothing.
func TestGenerateModuleDataQuarkBuildsAndPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and tests a scaffolded app; skipped with -short")
	}
	quarkDir := os.Getenv("NUCLEUS_QUARK_DIR")
	if quarkDir == "" {
		t.Skip("set NUCLEUS_QUARK_DIR to a Quark checkout to build the --data quark slice")
	}
	projectDir := scaffoldOfflineProject(t, "qbuild")
	var stdout, stderr bytes.Buffer
	if err := runGenerate([]string{"module", "items", "--out", projectDir, "--data", "quark", "--mount", "--with-policy"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate --data quark: %v\nstderr: %s", err, stderr.String())
	}
	goMod := filepath.Join(projectDir, "go.mod")
	raw, err := os.ReadFile(goMod)
	if err != nil {
		t.Fatal(err)
	}
	patched := string(raw) + "\nrequire github.com/jcsvwinston/quark v0.0.0\n\nreplace github.com/jcsvwinston/quark => \"" + quarkDir + "\"\n"
	if err := os.WriteFile(goMod, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	runGoInProject(t, projectDir, "mod", "tidy")
	runGoInProject(t, projectDir, "build", "./...")
	out := runGoInProject(t, projectDir, "test", "-count=1", "./internal/items/")
	if !strings.Contains(out, "ok  \texample.com/qbuild/internal/items") {
		t.Errorf("the quark slice's emitted test must pass, got:\n%s", out)
	}
}
