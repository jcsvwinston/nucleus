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
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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
	// The umbrella's exit-0 guard (check_exit0_regressions.sh, A7) greps
	// this literal phrase in an empty directory; both fallback notes keep
	// it.
	if !strings.Contains(out, "listing framework-owned routes only") {
		t.Errorf("the fallback note must keep the literal phrase the umbrella guard greps:\n%s", out)
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
	if !strings.Contains(out, "listing framework-owned routes only") {
		t.Errorf("the --framework-only note must keep the literal phrase the umbrella guard greps:\n%s", out)
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

// freeLoopbackPort reserves a port on the loopback interface and releases it
// for the fixture to bind.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// A main that serves instead of booting through nucleus.Run ignores the
// variable. The command must not wait on it forever, and must not leave it
// listening behind: the run is bounded by --timeout and the whole process
// group is killed at the deadline.
func TestRoutesStopsAnApplicationThatServesInsteadOfPrinting(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs a Go program; skipped with -short")
	}
	dir := t.TempDir()
	port := freeLoopbackPort(t)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/serves\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Serves and never returns — the shape of a main that calls
	// http.ListenAndServe (or boots through pkg/app instead of
	// nucleus.Run), or of a nucleus.Run whose OnStart blocks.
	mainSrc := fmt.Sprintf(`package main

import "net/http"

func main() {
	if err := http.ListenAndServe(%q, nil); err != nil {
		panic(err)
	}
}
`, addr)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLAGS", "-mod=mod")

	var stdout, stderr bytes.Buffer
	started := time.Now()
	err := runRoutes([]string{"--dir", dir, "--timeout", "2s"}, strings.NewReader(""), &stdout, &stderr)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatalf("an application that serves instead of printing must be an error, got output:\n%s", stdout.String())
	}
	if elapsed > 20*time.Second {
		t.Errorf("the command must give up at the deadline (2s), took %s", elapsed)
	}
	for _, want := range []string{"kept running", "NUCLEUS_PRINT_ROUTES", "--framework-only", "--timeout"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error must contain %q, got: %v", want, err)
		}
	}
	// The listener is gone: nothing answers on the port any more.
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if dialErr != nil {
			break
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatalf("the application is still listening on %s after the command returned", addr)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// --config is the configuration-only listing's flag; the binary reads its
// own configuration. Inside a project the flag is refused instead of being
// ignored with exit 0 (the class the exit-0 guards exist for), and with
// --framework-only it keeps meaning what it always did.
func TestRoutesRefusesConfigOnTheBinaryPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/withconfig\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "nonexistent.yml")

	var stdout, stderr bytes.Buffer
	err := runRoutes([]string{"--dir", dir, "--config", missing}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatalf("--config on the binary path must be an error, got output:\n%s", stdout.String())
	}
	if !strings.Contains(err.Error(), "--framework-only") || !strings.Contains(err.Error(), "--config") {
		t.Errorf("the error must name --config and the --framework-only way out, got: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("nothing is listed when the flag is refused, got:\n%s", stdout.String())
	}

	// With --framework-only the flag is honoured, so a missing file is the
	// configuration error it always was.
	err = runRoutes([]string{"--dir", dir, "--config", missing, "--framework-only"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("--config <missing> --framework-only must fail on the missing file, got: %v", err)
	}
}

// A project pinned to a nucleus release that predates NUCLEUS_PRINT_ROUTES
// would serve instead of printing. The pre-check resolves the module the
// project uses (replace applied) and, when that copy has no
// internal/routedump, answers from configuration with a note instead of
// building and running the binary.
func TestRoutesFallsBackWhenTheNucleusRequirementPredatesTheVariable(t *testing.T) {
	root := t.TempDir()
	oldNucleus := filepath.Join(root, "nucleus-old")
	if err := os.MkdirAll(filepath.Join(oldNucleus, "pkg", "nucleus"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldNucleus, "go.mod"), []byte("module github.com/jcsvwinston/nucleus\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	goMod := "module example.com/old\n\ngo 1.22\n\nrequire github.com/jcsvwinston/nucleus v1.24.0\n\nreplace github.com/jcsvwinston/nucleus => ../nucleus-old\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	// A main that would serve forever if it were built and run.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { select {} }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRoutesTestConfig(t, dir)

	var stdout, stderr bytes.Buffer
	started := time.Now()
	if err := runRoutes([]string{"--dir", dir, "--timeout", "2s"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("routes on a project pinned to an older nucleus: %v\nstderr: %s", err, stderr.String())
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Errorf("the pre-check must answer without building or running the binary, took %s", elapsed)
	}
	out := stdout.String()
	for _, want := range []string{"NOTE:", "predates NUCLEUS_PRINT_ROUTES", "listing framework-owned routes only", "GET\t/healthz\t\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output must contain %q:\n%s", want, out)
		}
	}
}

// The module joined plain output as the LAST column so nothing a consumer
// read before moves: with --verbose, middleware=N stays third and the
// module comes fourth. The JSON shape carries the module key on every
// entry, empty for the framework's own routes, so consumers never need a
// presence check.
func TestRoutesPlainColumnsKeepTheirPositionsAndJSONAlwaysCarriesModule(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeRoutesTestConfig(t, dir)

	var stdout, stderr bytes.Buffer
	if err := runRoutes([]string{"--framework-only", "--config", cfgPath, "--verbose"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("routes --verbose: %v\nstderr: %s", err, stderr.String())
	}
	var healthz string
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.HasPrefix(line, "GET\t/healthz\t") {
			healthz = line
		}
	}
	if healthz == "" {
		t.Fatalf("no /healthz line in verbose output:\n%s", stdout.String())
	}
	cols := strings.Split(healthz, "\t")
	if len(cols) != 4 || !strings.HasPrefix(cols[2], "middleware=") || cols[3] != "" {
		t.Errorf("verbose plain output must be METHOD, PATTERN, middleware=N, MODULE (module empty for the framework), got %q", healthz)
	}

	stdout.Reset()
	if err := runRoutes([]string{"--framework-only", "--config", cfgPath, "--json"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("routes --json: %v\nstderr: %s", err, stderr.String())
	}
	var entries []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		t.Fatalf("stdout must be a JSON array: %v\n%s", err, stdout.String())
	}
	for _, e := range entries {
		if _, ok := e["module"]; !ok {
			t.Errorf("every JSON entry carries the module key (empty for the framework's own), got %v", e)
		}
	}
}
