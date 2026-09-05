// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// `nucleus routes` and `nucleus migrate status` used to be blind to the
// binary: routes built a fresh app from nucleus.yml (opening the database
// and writing boot logs to stdout) and listed only /healthz while the
// binary served a mounted module's six routes; migrate status read the
// migrations directory and said "No migration files found" while the
// module's embedded migration sat applied in the ledger. Both now read
// what the compiled application knows.
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scaffoldProjectWithModule renders the mvc scaffold, pins it to this
// checkout, generates a `notes` feature slice and mounts it in main.go —
// the shape of the audit's reproduction — and returns the project dir.
func scaffoldProjectWithModule(t *testing.T) string {
	t.Helper()
	repoRoot := repoRootForTest(t)
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	newArgs := []string{"myapp", "--out", outDir, "--template", "mvc", "--module", "example.com/myapp"}
	if err := runNew(newArgs, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew: %v\nstderr: %s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, "myapp")
	pinGoModToLocalNucleus(t, projectDir, repoRoot)

	stdout.Reset()
	stderr.Reset()
	if err := runGenerate([]string{"module", "notes", "--out", projectDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate module: %v\nstderr: %s", err, stderr.String())
	}

	mainPath := filepath.Join(projectDir, "main.go")
	mainSrc, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(mainSrc),
		"\"github.com/jcsvwinston/nucleus/pkg/nucleus\"",
		"\"example.com/myapp/internal/notes\"\n\n\t\"github.com/jcsvwinston/nucleus/pkg/nucleus\"", 1)
	patched = strings.Replace(patched, "if err := nucleus.New().",
		"if err := nucleus.New().\n\t\tMount(notes.Module()).", 1)
	if !strings.Contains(patched, "Mount(notes.Module())") {
		t.Fatalf("could not add the Mount line to scaffold main.go:\n%s", mainSrc)
	}
	if err := os.WriteFile(mainPath, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	return projectDir
}

// The command runs the project's binary with NUCLEUS_PRINT_ROUTES and lists
// every route it serves, attributed to its module, with nothing but the
// table on stdout. The same run applied the module's embedded migration,
// which migrate status then reports from the ledger.
func TestRoutesAndMigrateStatusReadTheCompiledBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs a scaffolded app; skipped with -short")
	}
	projectDir := scaffoldProjectWithModule(t)
	// The scaffold's go.mod is patched with local replaces; -mod=mod lets
	// `go run` resolve them without a go.sum round-trip (as the other
	// scaffold build tests do).
	t.Setenv("GOFLAGS", "-mod=mod")

	var stdout, stderr bytes.Buffer
	if err := runRoutes([]string{"--dir", projectDir, "--json"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("routes --json: %v\nstderr: %s", err, stderr.String())
	}
	var routes []routeEntry
	if err := json.Unmarshal(stdout.Bytes(), &routes); err != nil {
		t.Fatalf("stdout must be the JSON table alone (no boot log lines), got error %v:\n%s", err, stdout.String())
	}
	byKey := map[string]string{}
	for _, r := range routes {
		byKey[r.Method+" "+r.Pattern] = r.Module
	}
	for _, want := range []string{"GET /notes", "POST /notes", "GET /notes/{id}", "PUT /notes/{id}", "DELETE /notes/{id}"} {
		module, ok := byKey[want]
		if !ok {
			t.Errorf("route %q of the mounted module is missing from the listing: %v", want, byKey)
			continue
		}
		if module != "notes" {
			t.Errorf("route %q attributed to %q, want notes", want, module)
		}
	}
	if module, ok := byKey["GET /healthz"]; !ok || module != "" {
		t.Errorf("GET /healthz must be listed as a framework route (module empty), got present=%v module=%q", ok, module)
	}

	// Plain text: one line per route with the module column, no NOTE.
	stdout.Reset()
	if err := runRoutes([]string{"--dir", projectDir, "--path", "/notes"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("routes --path: %v\nstderr: %s", err, stderr.String())
	}
	plain := stdout.String()
	if strings.Contains(plain, "NOTE:") {
		t.Errorf("no blindness note when the binary was read:\n%s", plain)
	}
	if !strings.Contains(plain, "GET\t/notes\tnotes\n") {
		t.Errorf("plain output must carry METHOD, PATTERN and MODULE columns, got:\n%s", plain)
	}
	if strings.Contains(plain, "/healthz") {
		t.Errorf("--path /notes must filter out /healthz:\n%s", plain)
	}

	// The boot that printed the routes ran the module's OnStart, which
	// applied its embedded migration into the project database. The
	// migrations directory holds nothing for it — the ledger does.
	oldWd, _ := os.Getwd()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	stdout.Reset()
	if err := runMigrate([]string{"--config", "nucleus.yml", "status"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("migrate status: %v\nstderr: %s", err, stderr.String())
	}
	status := stdout.String()
	if strings.Contains(status, "No migration files found") {
		t.Fatalf("migrate status ignored the module ledger rows:\n%s", status)
	}
	if !strings.Contains(status, "notes/000001_create_notes\tapplied\t") {
		t.Errorf("migrate status must list the module's applied embedded migration, got:\n%s", status)
	}
}

func writeRoutesTestConfig(t *testing.T, dir string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	cfg := fmt.Sprintf("database_default: default\ndatabases:\n  default:\n    url: sqlite://%s\nlog_level: error\nlog_format: text\n",
		filepath.Join(dir, "app.db"))
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

// Outside a Go project there is no binary to run: the configuration-only
// listing answers, and says so.
func TestRoutesWithoutGoModFallsBackToConfiguration(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeRoutesTestConfig(t, dir)

	var stdout, stderr bytes.Buffer
	if err := runRoutes([]string{"--dir", dir, "--config", cfgPath}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("routes: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "NOTE: no go.mod in "+dir) {
		t.Errorf("the fallback must say why the binary was not read:\n%s", out)
	}
	if !strings.Contains(out, "GET\t/healthz\t\n") {
		t.Errorf("framework routes must still be listed (module column empty):\n%s", out)
	}
}

// --framework-only never builds: a go.mod whose main cannot compile is not
// touched, and the note names the flag.
func TestRoutesFrameworkOnlySkipsTheBuild(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeRoutesTestConfig(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/broken\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { this does not compile }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := runRoutes([]string{"--dir", dir, "--config", cfgPath, "--framework-only"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("routes --framework-only: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "NOTE: --framework-only") || !strings.Contains(out, "GET\t/healthz\t\n") {
		t.Errorf("--framework-only must list the configuration-only routes with its note:\n%s", out)
	}
}

// A project whose main never reaches nucleus.Run prints no table; the
// error says what the binary must do instead of listing nothing.
func TestRoutesFailsWhenTheBinaryPrintsNoTable(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a Go program; skipped with -short")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/silent\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hello\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLAGS", "-mod=mod")

	var stdout, stderr bytes.Buffer
	err := runRoutes([]string{"--dir", dir}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatalf("a binary that prints no table must be an error, got output:\n%s", stdout.String())
	}
	if !strings.Contains(err.Error(), "NUCLEUS_PRINT_ROUTES") || !strings.Contains(err.Error(), "--framework-only") {
		t.Errorf("the error must name the variable and the fallback flag, got: %v", err)
	}
}
