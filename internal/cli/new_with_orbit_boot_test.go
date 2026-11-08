// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWithOrbitScaffoldBootsWithSiblingCheckouts boots what `nucleus new
// --with orbit` writes on the mvc and api templates, against the orbit and
// quark checkouts next to this repository (skipped, not faked, without
// them; CI's suite-starter lane checks them out at their latest release).
//
// The panel is mounted on a quarkdatasource adapter over a Quark client
// derived from the framework-managed pool (NU-159): orbit's
// Config.DataSource is required from its 2.0 (DEP-2026-002), and in 1.x a
// nil one boots an empty Data Studio on a deprecated default that logs a
// WARN at start. The walk asks for what the scaffold promises with no
// models registered yet: /healthz, the login gate under /admin, a login
// with the bootstrap credentials, Data Studio answering with an empty
// catalogue — and zero WARN lines, which is what tells the 1.x default
// path from the data source the scaffold passes.
//
// Pointed at an orbit checkout on its 2.0 branch (NUCLEUS_SIBLING_CHECKOUTS),
// the same walk proves the scaffold starts where a panel without a data
// source refuses to.
func TestWithOrbitScaffoldBootsWithSiblingCheckouts(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and boots the --with orbit scaffolds; skipped with -short")
	}
	repoRoot := repoRootForTest(t)
	orbit, quark := siblingCheckouts(repoRoot)
	if orbit == "" {
		if os.Getenv("NUCLEUS_REQUIRE_SIBLING_CHECKOUTS") != "" {
			t.Fatalf("NUCLEUS_REQUIRE_SIBLING_CHECKOUTS is set and there are no orbit and quark checkouts next to %s (or under NUCLEUS_SIBLING_CHECKOUTS)", repoRoot)
		}
		t.Skip("no orbit and quark checkouts next to this repository (set NUCLEUS_SIBLING_CHECKOUTS); CI's suite-starter lane boots the --with orbit scaffolds")
	}

	for _, tmpl := range []string{"mvc", "api"} {
		t.Run(tmpl, func(t *testing.T) {
			outDir := t.TempDir()
			port := freeLoopbackPort(t)
			var stdout, stderr bytes.Buffer
			args := []string{"panelcheck", "--out", outDir, "--template", tmpl, "--with", "orbit", "--module", "example.com/panelcheck", "--port", fmt.Sprint(port), "--offline"}
			if err := runNew(args, strings.NewReader(""), &stdout, &stderr); err != nil {
				t.Fatalf("runNew(%s --with orbit): %v\nstderr: %s", tmpl, err, stderr.String())
			}
			projectDir := filepath.Join(outDir, "panelcheck")
			pinGoModToSiblingCheckouts(t, projectDir, repoRoot, orbit, quark)
			runGoCommand(t, projectDir, "mod", "tidy")
			runGoCommand(t, projectDir, "vet", "./...")
			runGoCommand(t, projectDir, "build", "-o", "app", ".")

			app := exec.Command(filepath.Join(projectDir, "app"))
			app.Dir = projectDir
			app.Env = append(os.Environ(), "ADMIN_BOOTSTRAP_PASSWORD=quickstart")
			var appLog bytes.Buffer
			app.Stdout = &appLog
			app.Stderr = &appLog
			if err := app.Start(); err != nil {
				t.Fatal(err)
			}
			stopped := false
			stop := func() {
				if stopped {
					return
				}
				stopped = true
				_ = app.Process.Kill()
				_ = app.Wait()
			}
			t.Cleanup(stop)

			base := fmt.Sprintf("http://127.0.0.1:%d", port)
			jar, _ := cookiejar.New(nil)
			client := &http.Client{Timeout: 5 * time.Second, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			waitForHealthz(t, client, base, func() string { stop(); return appLog.String() })

			do := func(method, path, contentType, body string, headers map[string]string) (int, string) {
				t.Helper()
				req, err := http.NewRequest(method, base+path, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				if contentType != "" {
					req.Header.Set("Content-Type", contentType)
				}
				for k, v := range headers {
					req.Header.Set(k, v)
				}
				resp, err := client.Do(req)
				if err != nil {
					stop()
					t.Fatalf("%s %s: %v\n%s", method, path, err, appLog.String())
				}
				defer resp.Body.Close()
				out, _ := io.ReadAll(resp.Body)
				return resp.StatusCode, string(out)
			}

			// The panel answers under /admin before anyone logs in: its
			// login gate, not a 404 and not a failure of the panel.
			code, body := do(http.MethodGet, "/admin/", "", "", nil)
			if code >= 400 {
				stop()
				t.Fatalf("GET /admin/: want the panel's login gate (2xx or a redirect), got %d body=%s\n--- app log ---\n%s", code, body, appLog.String())
			}
			form := url.Values{"username": {"admin"}, "password": {"quickstart"}}.Encode()
			code, body = do(http.MethodPost, "/admin/login", "application/x-www-form-urlencoded", form, map[string]string{"Sec-Fetch-Site": "same-origin"})
			if code < 200 || code > 399 {
				stop()
				t.Fatalf("POST /admin/login: want a success or a redirect, got %d body=%s\n--- app log ---\n%s", code, body, appLog.String())
			}
			// Data Studio is on the Quark adapter with nothing registered:
			// it answers, with an empty catalogue, instead of the 1.x
			// default over pkg/model (which the WARN count below catches)
			// or the 2.0 refusal to start (which /healthz above catches).
			code, body = do(http.MethodGet, "/admin/api/models", "", "", nil)
			if code != http.StatusOK {
				stop()
				t.Fatalf("GET /admin/api/models: want 200, got %d body=%s\n--- app log ---\n%s", code, body, appLog.String())
			}
			if !strings.Contains(body, `"models":[]`) {
				t.Errorf("GET /admin/api/models: the scaffold registers no model, want an empty catalogue, got %s", body)
			}

			stop()
			if warns := len(warnLine.FindAllString(appLog.String(), -1)); warns != 0 {
				t.Errorf("%s --with orbit must boot and serve the walk above with zero WARN lines (a nil DataSource would log orbit's deprecation), got %d:\n%s", tmpl, warns, appLog.String())
			}
		})
	}
}
