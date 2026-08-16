// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// DX-11 (DX audit 2026-08-16, §3.2): the cliff, converted into a test.
//
// quickstart.md tells the reader to copy examples/mvc_api as the model for
// their first module. Doing exactly that onto a fresh `nucleus new` (mvc)
// scaffold produced 403 FORBIDDEN on GET /notes and 419 CSRF_FAILED on
// POST /notes — the canonical example was written against
// WithoutDefaults() and never exercised the two barriers the scaffold
// turns on. This test performs the documented walkthrough literally:
// scaffold → copy the example module and its migration → add the Mount
// line the quickstart shows → migrate → boot → curl. It must end in
// 200/201 without editing anything else.
package cli

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func copyFileTo(t *testing.T, src, dstDir string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, filepath.Base(src)), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestQuickstartExampleWorksOnScaffold(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and boots a scaffolded app; skipped with -short")
	}
	repoRoot := repoRootForTest(t)

	// 1. nucleus new myapp --template mvc (the default template).
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	newArgs := []string{"myapp", "--out", outDir, "--template", "mvc", "--module", "example.com/myapp"}
	if err := runNew(newArgs, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew: %v\nstderr: %s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, "myapp")
	pinGoModToLocalNucleus(t, projectDir, repoRoot)

	// 2. Copy the example module and its migration — the quickstart's
	// "use examples/mvc_api as the model for your own first module".
	notesSrc := filepath.Join(repoRoot, "examples", "mvc_api", "internal", "notes")
	for _, f := range []string{"note.go", "controller.go", "module.go"} {
		copyFileTo(t, filepath.Join(notesSrc, f), filepath.Join(projectDir, "internal", "notes"))
	}
	migSrc := filepath.Join(repoRoot, "examples", "mvc_api", "migrations")
	for _, f := range []string{"001_create_notes.up.sql", "001_create_notes.down.sql"} {
		copyFileTo(t, filepath.Join(migSrc, f), filepath.Join(projectDir, "migrations"))
	}

	// The example package imports itself by its example path in module.go's
	// package docs only; the code is self-contained. The scaffold main
	// needs the Mount line the quickstart shows.
	mainPath := filepath.Join(projectDir, "main.go")
	mainSrc, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(mainSrc),
		"\"github.com/jcsvwinston/nucleus/pkg/nucleus\"",
		"\"example.com/myapp/internal/notes\"\n\n\t\"github.com/jcsvwinston/nucleus/pkg/nucleus\"", 1)
	if !strings.Contains(patched, "internal/notes") {
		t.Fatalf("could not add the notes import to scaffold main.go:\n%s", mainSrc)
	}
	patched = strings.Replace(patched, "if err := nucleus.New().", "if err := nucleus.New().\n\t\tMount(notes.Module()).", 1)
	if !strings.Contains(patched, "Mount(notes.Module())") {
		t.Fatalf("could not add the Mount line to scaffold main.go:\n%s", mainSrc)
	}
	if err := os.WriteFile(mainPath, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pick a free port and point the scaffold config at it.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	cfgPath := filepath.Join(projectDir, "nucleus.yml")
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfgStr := strings.Replace(string(cfg), "port: 8080", fmt.Sprintf("port: %d", port), 1)
	if err := os.WriteFile(cfgPath, []byte(cfgStr), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. Migrate — the quickstart's `nucleus migrate up`.
	oldWd, _ := os.Getwd()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	var migOut, migErr bytes.Buffer
	if err := runMigrate([]string{"up"}, strings.NewReader(""), &migOut, &migErr); err != nil {
		t.Fatalf("migrate up: %v\nstderr: %s", err, migErr.String())
	}

	// 4. Build and boot.
	build := exec.Command("go", "build", "-o", "app", ".")
	build.Dir = projectDir
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the scaffold+example app: %v\n%s", err, out)
	}
	app := exec.Command(filepath.Join(projectDir, "app"))
	app.Dir = projectDir
	var appLog bytes.Buffer
	app.Stdout = &appLog
	app.Stderr = &appLog
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Process.Kill(); _, _ = app.Process.Wait() })

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("app did not come up\nlog:\n%s", appLog.String())
		}
		resp, err := client.Get(base + "/healthz")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	// 5. The cliff: these two used to answer 403 and 419.
	resp, err := client.Get(base + "/notes")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /notes on the documented walkthrough: want 200, got %d body=%s\napp log:\n%s", resp.StatusCode, body, appLog.String())
	}

	post, err := client.Post(base+"/notes", "application/json", strings.NewReader(`{"title":"first","body":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	postBody, _ := io.ReadAll(post.Body)
	post.Body.Close()
	if post.StatusCode != http.StatusCreated {
		t.Errorf("POST /notes on the documented walkthrough: want 201, got %d body=%s", post.StatusCode, postBody)
	}
}
