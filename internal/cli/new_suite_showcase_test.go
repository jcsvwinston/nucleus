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

// showcaseKeptFiles are the files of examples/showcase_demo the suite
// template does not render: the module's real pins (the example builds
// standalone from the proxy, so its go.mod carries published tags where
// the scaffold carries one require) and the sum file they produce.
var showcaseKeptFiles = map[string]bool{"go.mod": true, "go.sum": true}

// renderShowcaseDemo scaffolds the showcase's parameters offline and
// returns the project directory.
func renderShowcaseDemo(t *testing.T) string {
	t.Helper()
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	args := []string{"showcase_demo", "--out", outDir, "--template", "suite", "--db", "sqlite", "--port", "8091", "--module", "github.com/jcsvwinston/nucleus/examples/showcase_demo", "--offline"}
	if err := runNew(args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew(showcase): %v\nstderr: %s", err, stderr.String())
	}
	return filepath.Join(outDir, "showcase_demo")
}

// TestShowcaseDemoMatchesSuiteTemplate makes examples/showcase_demo the
// committed output of `nucleus new showcase_demo --template suite`: every
// file the template renders is byte-identical in the example, and the
// example carries nothing else but its pins. So "the example is right" is
// mechanical — the docs embed the example, the scaffold writes it, and the
// two cannot drift. NUCLEUS_UPDATE_SHOWCASE=1 rewrites the example from
// the template (`make regen-showcase`); run `GOWORK=off go mod tidy` in
// the example afterwards if its imports changed.
func TestShowcaseDemoMatchesSuiteTemplate(t *testing.T) {
	repoRoot := repoRootForTest(t)
	exampleDir := filepath.Join(repoRoot, "examples", "showcase_demo")
	rendered := renderShowcaseDemo(t)
	update := os.Getenv("NUCLEUS_UPDATE_SHOWCASE") == "1"

	renderedFiles := listFiles(t, rendered)
	if update {
		for _, rel := range listFiles(t, exampleDir) {
			if showcaseKeptFiles[rel] {
				continue
			}
			if err := os.Remove(filepath.Join(exampleDir, filepath.FromSlash(rel))); err != nil {
				t.Fatal(err)
			}
		}
		for _, rel := range renderedFiles {
			src := filepath.Join(rendered, filepath.FromSlash(rel))
			dst := filepath.Join(exampleDir, filepath.FromSlash(rel))
			if showcaseKeptFiles[rel] {
				if _, err := os.Stat(dst); err == nil {
					continue
				}
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dst, []byte(readFile(t, src)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("examples/showcase_demo regenerated from the suite template (%d files)", len(renderedFiles))
	}

	for _, rel := range renderedFiles {
		if showcaseKeptFiles[rel] {
			continue
		}
		want := readFile(t, filepath.Join(rendered, filepath.FromSlash(rel)))
		gotPath := filepath.Join(exampleDir, filepath.FromSlash(rel))
		got, err := os.ReadFile(gotPath)
		if err != nil {
			t.Errorf("examples/showcase_demo/%s: the suite template renders it, the example lacks it (%v); run NUCLEUS_UPDATE_SHOWCASE=1 go test ./internal/cli -run TestShowcaseDemoMatchesSuiteTemplate", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("examples/showcase_demo/%s drifted from the suite template; regenerate it with NUCLEUS_UPDATE_SHOWCASE=1 go test ./internal/cli -run TestShowcaseDemoMatchesSuiteTemplate (edit the template, never the example)", rel)
		}
	}
	renderedSet := map[string]bool{}
	for _, rel := range renderedFiles {
		renderedSet[rel] = true
	}
	for _, rel := range listFiles(t, exampleDir) {
		if !renderedSet[rel] && !showcaseKeptFiles[rel] {
			t.Errorf("examples/showcase_demo/%s is not rendered by the suite template: the example is the template's output and carries nothing of its own but go.mod and go.sum", rel)
		}
	}
	for rel := range showcaseKeptFiles {
		if _, err := os.Stat(filepath.Join(exampleDir, rel)); err != nil {
			t.Errorf("examples/showcase_demo/%s must exist: the example pins real tags so it builds from the proxy", rel)
		}
	}
}

// siblingCheckouts returns the orbit and quark checkouts next to this
// repository (NUCLEUS_SIBLING_CHECKOUTS names another parent directory),
// or "" when either is missing.
func siblingCheckouts(repoRoot string) (orbit, quark string) {
	parent := os.Getenv("NUCLEUS_SIBLING_CHECKOUTS")
	if parent == "" {
		parent = filepath.Dir(repoRoot)
	}
	orbit, quark = filepath.Join(parent, "orbit"), filepath.Join(parent, "quark")
	for _, dir := range []string{orbit, quark} {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
			return "", ""
		}
	}
	return orbit, quark
}

// pinGoModToSiblingCheckouts extends pinGoModToLocalNucleus with replace
// directives for the suite modules the scaffold imports, pointed at the
// sibling checkouts, so the suite scaffold compiles without the proxy.
func pinGoModToSiblingCheckouts(t *testing.T, projectDir, repoRoot, orbit, quark string) {
	t.Helper()
	pinGoModToLocalNucleus(t, projectDir, repoRoot)
	goModPath := filepath.Join(projectDir, "go.mod")
	raw, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.Write(raw)
	for module, dir := range map[string]string{
		"github.com/jcsvwinston/orbit":                 orbit,
		"github.com/jcsvwinston/orbit/quarkbridge":     filepath.Join(orbit, "quarkbridge"),
		"github.com/jcsvwinston/orbit/quarkdatasource": filepath.Join(orbit, "quarkdatasource"),
		"github.com/jcsvwinston/quark":                 quark,
		"github.com/jcsvwinston/quark/drivers/sqlite":  filepath.Join(quark, "drivers", "sqlite"),
	} {
		fmt.Fprintf(&b, "\nrequire %s v0.0.0\n\nreplace %s => %q\n", module, module, dir)
	}
	if err := os.WriteFile(goModPath, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSuiteScaffoldBootsWithSiblingCheckouts is the one boot of the suite
// scaffold this repository can run: the framework module cannot require
// orbit or quark, so the scaffold is compiled against the sibling
// checkouts next to this repository (go.work-style replace directives) —
// and skipped, not faked, when they are not there. It scaffolds
// --template suite untouched, builds it, runs its own test, boots it and
// asks over HTTP for what the docs promise: the seeded article, a create,
// a duplicate answered 409, an unknown path answered 404, and the admin
// login with the bootstrap credentials listing the Quark-backed models.
// Gated behind -short like the other scaffold builds.
func TestSuiteScaffoldBootsWithSiblingCheckouts(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and boots the suite scaffold; skipped with -short")
	}
	repoRoot := repoRootForTest(t)
	orbit, quark := siblingCheckouts(repoRoot)
	if orbit == "" {
		t.Skip("no orbit and quark checkouts next to this repository (set NUCLEUS_SIBLING_CHECKOUTS); the umbrella's workspace lane boots the suite scaffold")
	}

	outDir := t.TempDir()
	port := freeLoopbackPort(t)
	var stdout, stderr bytes.Buffer
	args := []string{"suitecheck", "--out", outDir, "--template", "suite", "--module", "example.com/suitecheck", "--port", fmt.Sprint(port), "--offline"}
	if err := runNew(args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew(suite): %v\nstderr: %s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, "suitecheck")
	pinGoModToSiblingCheckouts(t, projectDir, repoRoot, orbit, quark)
	runGoCommand(t, projectDir, "mod", "tidy")
	runGoCommand(t, projectDir, "vet", "./...")
	runGoCommand(t, projectDir, "test", "./...")
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
	expect := func(what string, got, want int, body string) {
		t.Helper()
		if got != want {
			stop()
			t.Fatalf("%s: want %d, got %d body=%s\n--- app log ---\n%s", what, want, got, body, appLog.String())
		}
	}

	code, body := do(http.MethodGet, "/api/articles", "", "", nil)
	expect("GET /api/articles", code, http.StatusOK, body)
	if !strings.Contains(body, `"Hello, Quantum"`) {
		t.Errorf("GET /api/articles: want the seeded article, got %s", body)
	}
	article := `{"author_id":1,"title":"probe","body":"live feed"}`
	code, body = do(http.MethodPost, "/api/articles", "application/json", article, nil)
	expect("POST /api/articles", code, http.StatusCreated, body)
	code, body = do(http.MethodPost, "/api/articles", "application/json", article, nil)
	expect("POST /api/articles (duplicate title)", code, http.StatusConflict, body)
	code, body = do(http.MethodGet, "/nope", "", "", nil)
	expect("GET /nope", code, http.StatusNotFound, body)

	// The login form is a browser route under CSRF: a browser sends
	// Sec-Fetch-Site and passes the origin check, a bare POST does not.
	form := url.Values{"username": {"admin"}, "password": {"quickstart"}}.Encode()
	code, body = do(http.MethodPost, "/admin/login", "application/x-www-form-urlencoded", form, nil)
	expect("POST /admin/login without a browser origin header", code, 419, body)
	code, body = do(http.MethodPost, "/admin/login", "application/x-www-form-urlencoded", form, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if code < 200 || code > 399 {
		expect("POST /admin/login", code, http.StatusSeeOther, body)
	}
	code, body = do(http.MethodGet, "/admin/api/models", "", "", nil)
	expect("GET /admin/api/models", code, http.StatusOK, body)
	for _, model := range []string{"Author", "Article"} {
		if !strings.Contains(body, model) {
			t.Errorf("Data Studio must list the Quark-backed %s model, got %s", model, body)
		}
	}
	code, body = do(http.MethodGet, "/admin/api/models/Author", "", "", nil)
	expect("GET /admin/api/models/Author", code, http.StatusOK, body)
	if !strings.Contains(body, "Ada Lovelace") {
		t.Errorf("Data Studio must serve the seeded author, got %s", body)
	}

	stop()
	if warns := strings.Count(appLog.String(), "level=WARN"); warns != 0 {
		t.Errorf("the suite scaffold must boot with zero WARN lines, got %d:\n%s", warns, appLog.String())
	}
}
