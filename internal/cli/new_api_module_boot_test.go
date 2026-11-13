// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// listeningLine is the framework's line once the socket is bound (NU-115):
// `nucleus: server listening` with the address it holds — the port the
// system assigned when nucleus.yml says 0.
var listeningLine = regexp.MustCompile(`msg="nucleus: server listening" addr=[^ :]+:(\d+)`)

// TestRunNewAPIWithGeneratedModuleBoots is DEP-2026-017 against the starter,
// and the measurement behind decision D9 of the 2.0 contract, taken in 1.x
// (NU-127). The api starter's contract is "no authz" (owner decision of
// 2026-10-06, kept at 2.0): `nucleus new --template api` writes
// WithoutDefaults() without WithAuthz(), so a module whose rows keep writes
// for a role would have them discarded at boot — every write answering
// anyone, one ERROR line — and from v2.0.0 would keep the application from
// starting. So `nucleus generate module notes --mount` on that starter
// writes its policy rows commented out, and the pair boots with nothing
// discarded: GET and POST /notes both answer, since nothing is enforced,
// the boot log carries no `module policies DISCARDED` line, and the
// module's own test — which boots the way main.go does — is green. The
// same project with the rows uncommented is measured both ways: with
// WithAuthz() added to main.go it enforces them (GET /notes 200, POST
// /notes 403); without it — the combination the generator no longer
// produces — it starts in 1.x, the write lands anyway and the boot log
// names the module, the routes that answer anyone and the notice.
//
// Each boot binds port 0 and the test reads the port the system assigned
// off the listening line, so no port is reserved and released ahead of a
// child that binds it later (the OR-149/NU-171 race). Each subtest writes
// the main.go and module.go it measures, so none depends on the edits of
// the one before it.
//
// Gated behind -short like the other scaffold builds.
func TestRunNewAPIWithGeneratedModuleBoots(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and boots the scaffold; skipped with -short")
	}
	repoRoot := repoRootForTest(t)
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	// `nucleus new` refuses --port 0, so the starter is written with a
	// placeholder port and nucleus.yml is edited to 0 below.
	args := []string{"apinotes", "--out", outDir, "--template", "api", "--module", "example.com/apinotes", "--port", "18080", "--offline"}
	if err := runNew(args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew(api): %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nucleus generate module writes its policy rows commented out here") {
		t.Errorf("`nucleus new --template api` must say what the generator does on this starter:\n%s", stdout.String())
	}
	projectDir := filepath.Join(outDir, "apinotes")
	pinGoModToLocalNucleus(t, projectDir, repoRoot)
	configPath := filepath.Join(projectDir, "nucleus.yml")
	config := readFile(t, configPath)
	if !strings.Contains(config, "port: 18080\n") {
		t.Fatalf("nucleus.yml does not carry the port where the test expects it:\n%s", config)
	}
	if err := os.WriteFile(configPath, []byte(strings.Replace(config, "port: 18080\n", "port: 0\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(projectDir, "main.go")
	if src := stripComments(readFile(t, mainPath)); strings.Contains(src, "WithAuthz()") || !strings.Contains(src, "WithoutDefaults()") {
		t.Fatalf("the api starter's contract is WithoutDefaults() without WithAuthz():\n%s", src)
	}
	readme := readFile(t, filepath.Join(projectDir, "README.md"))
	if !strings.Contains(readme, "the module with its policy rows commented out") || strings.Contains(readme, "its own policy rows") {
		t.Errorf("the README written into the api project must describe what the generator writes there:\n%s", readme)
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
	// The sources every subtest starts from: main.go as --mount left it,
	// module.go as the generator wrote it.
	mainSrc := readFile(t, mainPath)
	moduleSrc := readFile(t, modulePath)
	if !strings.Contains(moduleSrc, "\t\t// Policies: []nucleus.PolicyRule{") || strings.Contains(stripComments(moduleSrc), "Policies:") {
		t.Fatalf("on the api starter the generated module must carry its rows commented out:\n%s", moduleSrc)
	}
	if !strings.Contains(mainSrc, "\t\tWithoutDefaults().\n") {
		t.Fatalf("main.go does not carry WithoutDefaults() where the test expects it:\n%s", mainSrc)
	}
	withAuthz := strings.Replace(mainSrc, "\t\tWithoutDefaults().\n", "\t\tWithoutDefaults().\n\t\tWithAuthz().\n", 1)
	uncommented := uncommentPolicyRows(moduleSrc)
	if !strings.Contains(stripComments(uncommented), "Policies:") {
		t.Fatalf("uncommenting the rows did not produce a Policies field:\n%s", uncommented)
	}
	writeProject := func(t *testing.T, main, module string) {
		t.Helper()
		if err := os.WriteFile(mainPath, []byte(main), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(modulePath, []byte(module), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// boot builds and starts the project on port 0, and returns the base
	// URL it listens on, the log so far, whether the process exited before
	// listening, and a stop.
	boot := func(t *testing.T) (base string, log func() string, exited bool, stop func()) {
		t.Helper()
		runGoCommand(t, projectDir, "mod", "tidy")
		runGoCommand(t, projectDir, "build", "-o", "app", ".")
		app := exec.Command(filepath.Join(projectDir, "app"))
		app.Dir = projectDir
		appLog := &syncBuffer{}
		app.Stdout = appLog
		app.Stderr = appLog
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
		log = appLog.String
		client := &http.Client{Timeout: 2 * time.Second}
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-done:
				stopped = true
				return "", log, true, func() {}
			default:
			}
			if m := listeningLine.FindStringSubmatch(appLog.String()); m != nil {
				base = "http://127.0.0.1:" + m[1]
				if resp, err := client.Get(base + "/healthz"); err == nil {
					resp.Body.Close()
					return base, log, false, stop
				}
			}
			time.Sleep(150 * time.Millisecond)
		}
		stop()
		t.Fatalf("the application neither listened nor exited:\n%s", appLog.String())
		return "", nil, false, nil
	}
	probe := func(t *testing.T, base, method, path string, want int) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(`{"name":"x"}`))
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

	t.Run("the starter boots with a generated module, nothing is enforced and nothing is discarded", func(t *testing.T) {
		writeProject(t, mainSrc, moduleSrc)
		base, log, exited, stop := boot(t)
		if exited {
			t.Fatalf("the api starter with a generated module exited at boot:\n%s", log())
		}
		probe(t, base, http.MethodGet, "/notes", http.StatusOK)
		probe(t, base, http.MethodPost, "/notes", http.StatusCreated)
		stop()
		if strings.Contains(log(), "module policies DISCARDED") {
			t.Errorf("the module declares no rows, so the boot has nothing to discard:\n%s", log())
		}
		// The module's own test boots WithoutDefaults() as main.go does and
		// expects the write to land, as it just did over HTTP.
		runGoCommand(t, projectDir, "test", "-count=1", "./internal/notes/")
	})

	t.Run("with WithAuthz() and the rows uncommented, the starter enforces them", func(t *testing.T) {
		writeProject(t, withAuthz, uncommented)
		base, log, exited, stop := boot(t)
		if exited {
			t.Fatalf("the api starter with WithAuthz() and the module's rows exited at boot:\n%s", log())
		}
		probe(t, base, http.MethodGet, "/notes", http.StatusOK)
		probe(t, base, http.MethodPost, "/notes", http.StatusForbidden)
		stop()
		if strings.Contains(log(), "module policies DISCARDED") {
			t.Errorf("with WithAuthz() the rows are loaded, not discarded:\n%s", log())
		}
	})

	t.Run("the rows uncommented without WithAuthz() are discarded, said once, and the write lands (1.x)", func(t *testing.T) {
		writeProject(t, mainSrc, uncommented)
		if src := stripComments(readFile(t, modulePath)); !strings.Contains(src, "Policies:") {
			t.Fatalf("this subtest measures a module WITH rows on a root without an enforcer:\n%s", src)
		}
		base, log, exited, stop := boot(t)
		if exited {
			t.Fatalf("in 1.x a core-only application without WithAuthz() still starts with a module whose rows it cannot enforce (it refuses from v2.0.0):\n%s", log())
		}
		probe(t, base, http.MethodGet, "/notes", http.StatusOK)
		probe(t, base, http.MethodPost, "/notes", http.StatusCreated)
		stop()
		for _, want := range []string{"module policies DISCARDED", "notes (", "POST /notes", "WithAuthz()", "DEP-2026-017"} {
			if !strings.Contains(log(), want) {
				t.Errorf("the boot log does not say %q:\n%s", want, log())
			}
		}
		if n := strings.Count(log(), "module policies DISCARDED"); n != 1 {
			t.Errorf("the discarded rows are said once, said %d times:\n%s", n, log())
		}
	})
}
