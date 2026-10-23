// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRunNewAPIWithGeneratedModuleBoots is DEP-2026-017 against the starter,
// and the measurement behind decision D9 of the 2.0 dry run. The api
// starter's contract is "no authz" (owner decision of 2026-10-06, kept at
// 2.0): `nucleus new --template api` writes WithoutDefaults() without
// WithAuthz(), and a core-only application refuses to start with module rows
// it cannot enforce. So `nucleus generate module notes --mount` on that
// starter writes its policy rows commented out, and the pair boots: GET and
// POST /notes both answer, since nothing is enforced, and the module's own
// test — which boots the way main.go does — is green. The same project with
// the rows uncommented is measured both ways: with WithAuthz() added to
// main.go it boots and enforces them (GET /notes 200, POST /notes 403);
// without it, it refuses to start, naming the module, the routes that would
// answer anyone and the notice — the refusal the commented rows avoid.
//
// Gated behind -short like the other scaffold builds.
func TestRunNewAPIWithGeneratedModuleBoots(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and boots the scaffold; skipped with -short")
	}
	repoRoot := repoRootForTest(t)
	outDir := t.TempDir()
	port := freeLoopbackPort(t)

	var stdout, stderr bytes.Buffer
	args := []string{"apinotes", "--out", outDir, "--template", "api", "--module", "example.com/apinotes", "--port", fmt.Sprint(port), "--offline"}
	if err := runNew(args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew(api): %v\nstderr: %s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, "apinotes")
	pinGoModToLocalNucleus(t, projectDir, repoRoot)
	mainPath := filepath.Join(projectDir, "main.go")
	if src := stripComments(readFile(t, mainPath)); strings.Contains(src, "WithAuthz()") || !strings.Contains(src, "WithoutDefaults()") {
		t.Fatalf("the api starter's contract is WithoutDefaults() without WithAuthz():\n%s", src)
	}
	stdout.Reset()
	stderr.Reset()
	if err := runGenerate([]string{"module", "notes", "--out", projectDir, "--mount"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate module --mount: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "policy: NONE") || !strings.Contains(stdout.String(), "DEP-2026-017") {
		t.Errorf("the generator does not say the rows are commented out on a root without an enforcer:\n%s", stdout.String())
	}
	modulePath := filepath.Join(projectDir, "internal", "notes", "module.go")
	moduleSrc := readFile(t, modulePath)
	if !strings.Contains(moduleSrc, "\t\t// Policies: []nucleus.PolicyRule{") || strings.Contains(stripComments(moduleSrc), "Policies:") {
		t.Fatalf("on the api starter the generated module must carry its rows commented out:\n%s", moduleSrc)
	}
	if !strings.Contains(moduleSrc, "WithAuthz()") || !strings.Contains(moduleSrc, "DEP-2026-017") {
		t.Errorf("the commented rows must name WithAuthz() and the notice:\n%s", moduleSrc)
	}

	boot := func(t *testing.T) (log string, exited bool, stop func()) {
		t.Helper()
		runGoCommand(t, projectDir, "mod", "tidy")
		runGoCommand(t, projectDir, "build", "-o", "app", ".")
		app := exec.Command(filepath.Join(projectDir, "app"))
		app.Dir = projectDir
		var appLog bytes.Buffer
		app.Stdout = &appLog
		app.Stderr = &appLog
		if err := app.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- app.Wait() }()
		stopped := false
		stop = func() {
			if stopped {
				return
			}
			stopped = true
			_ = app.Process.Kill()
			<-done
		}
		t.Cleanup(stop)
		base := fmt.Sprintf("http://127.0.0.1:%d", port)
		client := &http.Client{Timeout: 2 * time.Second}
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-done:
				stopped = true
				return appLog.String(), true, func() {}
			default:
			}
			if resp, err := client.Get(base + "/healthz"); err == nil {
				resp.Body.Close()
				return "", false, stop
			}
			time.Sleep(150 * time.Millisecond)
		}
		stop()
		t.Fatalf("the application neither listened nor exited:\n%s", appLog.String())
		return "", false, nil
	}
	probe := func(t *testing.T, method, path string, want int) {
		t.Helper()
		req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), strings.NewReader(`{"name":"x"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s %s: want %d, got %d body=%s", method, path, want, resp.StatusCode, body)
		}
	}

	t.Run("the starter boots with a generated module and nothing is enforced", func(t *testing.T) {
		_, exited, stop := boot(t)
		if exited {
			t.Fatal("the api starter with a generated module exited at boot")
		}
		probe(t, http.MethodGet, "/notes", http.StatusOK)
		probe(t, http.MethodPost, "/notes", http.StatusCreated)
		stop()
		// The module's own test boots WithoutDefaults() as main.go does and
		// expects the write to land, as it just did over HTTP.
		runGoCommand(t, projectDir, "test", "./internal/notes/")
	})

	t.Run("with WithAuthz() and the rows uncommented, the starter enforces them", func(t *testing.T) {
		src := readFile(t, mainPath)
		if !strings.Contains(src, "\t\tWithoutDefaults().\n") {
			t.Fatalf("main.go does not carry WithoutDefaults() where the test expects it:\n%s", src)
		}
		if err := os.WriteFile(mainPath, []byte(strings.Replace(src, "\t\tWithoutDefaults().\n", "\t\tWithoutDefaults().\n\t\tWithAuthz().\n", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		uncommented := uncommentPolicyRows(moduleSrc)
		if !strings.Contains(stripComments(uncommented), "Policies:") {
			t.Fatalf("uncommenting the rows did not produce a Policies field:\n%s", uncommented)
		}
		if err := os.WriteFile(modulePath, []byte(uncommented), 0o644); err != nil {
			t.Fatal(err)
		}
		_, exited, _ := boot(t)
		if exited {
			t.Fatal("the api starter with WithAuthz() and the module's rows exited at boot")
		}
		probe(t, http.MethodGet, "/notes", http.StatusOK)
		probe(t, http.MethodPost, "/notes", http.StatusForbidden)
	})

	t.Run("the rows uncommented without WithAuthz() refuse to start", func(t *testing.T) {
		src := readFile(t, mainPath)
		if err := os.WriteFile(mainPath, []byte(strings.Replace(src, "\t\tWithAuthz().\n", "", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		log, exited, _ := boot(t)
		if !exited {
			t.Fatal("a core-only application without WithAuthz() started with a module whose rows it cannot enforce")
		}
		for _, want := range []string{"module policies DISCARDED", "notes (", "POST /notes", "WithAuthz()", "DEP-2026-017"} {
			if !strings.Contains(log, want) {
				t.Errorf("the refusal does not say %q:\n%s", want, log)
			}
		}
	})
}

// uncommentPolicyRows is the edit the generated module's comment asks for:
// delete the `// ` the generator put in front of the Policies literal.
func uncommentPolicyRows(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		rest, ok := strings.CutPrefix(line, "\t\t// ")
		if !ok {
			continue
		}
		if strings.HasPrefix(rest, "Policies: []nucleus.PolicyRule{") || strings.HasPrefix(rest, "\t{Subject:") || rest == "}," {
			lines[i] = "\t\t" + rest
		}
	}
	return strings.Join(lines, "\n")
}
