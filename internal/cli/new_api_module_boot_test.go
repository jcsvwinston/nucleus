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

// TestRunNewAPIWithGeneratedModuleBoots is DEP-2026-017 against the starter:
// `nucleus new --template api` followed by `nucleus generate module notes
// --mount` must boot. The generated module keeps its writes for an
// authenticated subject, and a core-only application without WithAuthz()
// refuses to start with rows it cannot enforce — so the api template carries
// WithAuthz(), and the rows are enforced there as on the default stack: GET
// /notes answers 200, POST /notes 403. The same project with WithAuthz()
// removed from main.go is measured too: it refuses to start, naming the
// module, the routes that would answer anyone, and the notice.
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
	stdout.Reset()
	stderr.Reset()
	if err := runGenerate([]string{"module", "notes", "--out", projectDir, "--mount"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate module --mount: %v\nstderr: %s", err, stderr.String())
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

	t.Run("the starter boots and enforces the module's rows", func(t *testing.T) {
		_, exited, stop := boot(t)
		if exited {
			t.Fatal("the api starter with a generated module exited at boot")
		}
		defer stop()
		base := fmt.Sprintf("http://127.0.0.1:%d", port)
		client := &http.Client{Timeout: 2 * time.Second}
		for _, p := range []struct {
			method string
			path   string
			want   int
		}{
			{http.MethodGet, "/notes", http.StatusOK},
			{http.MethodPost, "/notes", http.StatusForbidden},
		} {
			req, err := http.NewRequest(p.method, base+p.path, strings.NewReader(`{"title":"x"}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", p.method, p.path, err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != p.want {
				t.Errorf("%s %s on the api starter with a generated module: want %d, got %d body=%s", p.method, p.path, p.want, resp.StatusCode, body)
			}
		}
	})

	t.Run("without WithAuthz the same project refuses to start", func(t *testing.T) {
		mainPath := filepath.Join(projectDir, "main.go")
		src := readFile(t, mainPath)
		if !strings.Contains(src, "\t\tWithAuthz().\n") {
			t.Fatalf("main.go does not carry WithAuthz() where the test expects it:\n%s", src)
		}
		if err := os.WriteFile(mainPath, []byte(strings.Replace(src, "\t\tWithAuthz().\n", "", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		// The skeleton's rbac_policy_file is refused first on such an
		// application (the keys half of DEP-2026-017); dropped here so the
		// measurement is the module's rows, which the generator writes and
		// no configuration edit removes.
		cfgPath := filepath.Join(projectDir, "nucleus.yml")
		cfg := readFile(t, cfgPath)
		if !strings.Contains(cfg, "rbac_policy_file: rbac_policy.csv\n") {
			t.Fatalf("nucleus.yml does not name the policy file where the test expects it:\n%s", cfg)
		}
		if err := os.WriteFile(cfgPath, []byte(strings.Replace(cfg, "rbac_policy_file: rbac_policy.csv\n", "", 1)), 0o644); err != nil {
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
