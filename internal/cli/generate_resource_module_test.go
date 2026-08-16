// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// DX-21 (DX audit 2026-08-16, §4.C7/C8): `nucleus generate resource` produced
// code the framework could not mount — the handler's Mount expected
// *router.Mux (incompatible with nucleus.Router, hence the 78-line adapter
// every app wrote by hand) and the repository was an in-memory map that never
// touched the table its own migration created.
//
// The contract now: against a scaffolded app, `generate resource widget`
// emits a mountable Module() (internal/modules/widget_module.go) wired to a
// database-backed repository. This test performs the full walkthrough:
// scaffold → generate resource → Mount the module → migrate → boot → create
// via POST → RESTART the process → the record must still be there. An
// in-memory placeholder passes every single-process test; only a restart
// proves persistence.
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

func bootScaffoldApp(t *testing.T, projectDir, base string) *exec.Cmd {
	t.Helper()
	app := exec.Command(filepath.Join(projectDir, "app"))
	app.Dir = projectDir
	var appLog bytes.Buffer
	app.Stdout = &appLog
	app.Stderr = &appLog
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if app.ProcessState == nil {
			_ = app.Process.Kill()
			_, _ = app.Process.Wait()
		}
	})
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("app did not come up\nlog:\n%s", appLog.String())
		}
		resp, err := client.Get(base + "/healthz")
		if err == nil {
			resp.Body.Close()
			return app
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func stopScaffoldApp(t *testing.T, app *exec.Cmd) {
	t.Helper()
	_ = app.Process.Kill()
	_, _ = app.Process.Wait()
}

func TestGenerateResourceProducesMountablePersistentModule(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and boots a scaffolded app; skipped with -short")
	}
	repoRoot := repoRootForTest(t)

	// 1. nucleus new myapp --template mvc.
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	newArgs := []string{"myapp", "--out", outDir, "--template", "mvc", "--module", "example.com/myapp"}
	if err := runNew(newArgs, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew: %v\nstderr: %s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, "myapp")
	pinGoModToLocalNucleus(t, projectDir, repoRoot)

	// 2. nucleus generate resource widget.
	stdout.Reset()
	stderr.Reset()
	if err := runGenerate([]string{"resource", "widget", "--out", projectDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("generate resource: %v\nstderr: %s", err, stderr.String())
	}

	// The scaffold must emit a mountable module...
	modulePathGo := filepath.Join(projectDir, "internal", "modules", "widget_module.go")
	moduleSrc, err := os.ReadFile(modulePathGo)
	if err != nil {
		t.Fatalf("generate resource did not emit a mountable module (%s): %v — this is the 78-line-adapter gap (DX-21)", modulePathGo, err)
	}
	if !strings.Contains(string(moduleSrc), "func WidgetModule() nucleus.ModuleSpec") {
		t.Fatalf("widget_module.go does not export WidgetModule() nucleus.ModuleSpec:\n%s", moduleSrc)
	}

	// ...and a repository that is NOT an in-memory placeholder.
	repoSrc, err := os.ReadFile(filepath.Join(projectDir, "internal", "repositories", "widget_repository.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(repoSrc), "IN-MEMORY placeholder") || strings.Contains(string(repoSrc), "map[uint]") {
		t.Fatalf("generated repository is still the in-memory map placeholder (DX-21):\n%s", repoSrc)
	}

	// 3. Mount the module the way the CLI's own output instructs.
	mainPath := filepath.Join(projectDir, "main.go")
	mainSrc, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(mainSrc),
		"\"github.com/jcsvwinston/nucleus/pkg/nucleus\"",
		"\"example.com/myapp/internal/modules\"\n\n\t\"github.com/jcsvwinston/nucleus/pkg/nucleus\"", 1)
	if !strings.Contains(patched, "internal/modules") {
		t.Fatalf("could not add the modules import to scaffold main.go:\n%s", mainSrc)
	}
	patched = strings.Replace(patched, "if err := nucleus.New().", "if err := nucleus.New().\n\t\tMount(modules.WidgetModule()).", 1)
	if !strings.Contains(patched, "Mount(modules.WidgetModule())") {
		t.Fatalf("could not add the Mount line to scaffold main.go:\n%s", mainSrc)
	}
	if err := os.WriteFile(mainPath, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}

	// Allow anonymous CRUD on /widgets (default-deny) and exempt it from CSRF,
	// mirroring the quickstart rows the scaffold ships for /notes.
	policyPath := filepath.Join(projectDir, "rbac_policy.csv")
	policy, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	policyRows := `
p, anonymous, /widgets, read, allow
p, anonymous, /widgets, create, allow
p, anonymous, /widgets/*, read, allow
p, anonymous, /widgets/*, update, allow
p, anonymous, /widgets/*, delete, allow
`
	if err := os.WriteFile(policyPath, append(policy, []byte(policyRows)...), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(projectDir, "nucleus.yml")
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfgStr := strings.Replace(string(cfg), `csrf_exempt_paths: ["/api/", "/notes"]`, `csrf_exempt_paths: ["/api/", "/notes", "/widgets"]`, 1)
	if !strings.Contains(cfgStr, `"/widgets"`) {
		t.Fatalf("could not exempt /widgets from CSRF in nucleus.yml:\n%s", cfg)
	}

	// Pick a free port.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	cfgStr = strings.Replace(cfgStr, "port: 8080", fmt.Sprintf("port: %d", port), 1)
	if err := os.WriteFile(cfgPath, []byte(cfgStr), 0o644); err != nil {
		t.Fatal(err)
	}

	// 4. Migrate (the generated create_widgets_table migration applies too).
	oldWd, _ := os.Getwd()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	var migOut, migErr bytes.Buffer
	if err := runMigrate([]string{"up"}, strings.NewReader(""), &migOut, &migErr); err != nil {
		t.Fatalf("migrate up: %v\nstderr: %s", err, migErr.String())
	}

	// 5. Build, boot, create.
	build := exec.Command("go", "build", "-o", "app", ".")
	build.Dir = projectDir
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the scaffold+resource app: %v\n%s", err, out)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 2 * time.Second}

	app := bootScaffoldApp(t, projectDir, base)
	post, err := client.Post(base+"/widgets", "application/json", strings.NewReader(`{"name":"martillo"}`))
	if err != nil {
		t.Fatal(err)
	}
	postBody, _ := io.ReadAll(post.Body)
	post.Body.Close()
	if post.StatusCode != http.StatusCreated {
		t.Fatalf("POST /widgets: want 201, got %d body=%s", post.StatusCode, postBody)
	}

	// 6. RESTART. A map-backed repository forgets here; a real one does not.
	stopScaffoldApp(t, app)
	_ = bootScaffoldApp(t, projectDir, base)

	resp, err := client.Get(base + "/widgets")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /widgets after restart: want 200, got %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "martillo") {
		t.Fatalf("the widget created before the restart is gone — repository does not persist (DX-21):\n%s", body)
	}
}
