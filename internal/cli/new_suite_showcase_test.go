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
	"regexp"
	"strings"
	"testing"
	"time"
)

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
	// Two loggers write to that log: the framework's slog handler
	// (`level=WARN`) and Quark's standard logger (`2026/09/07 23:51:01 WARN
	// …`). Counting only the slog spelling once hid the ORM's "List()
	// called without explicit Limit()" line the first GET /api/articles
	// used to produce; the pattern matches both spellings and no other
	// word (WARNING never appears in either logger).
	if warns := len(warnLine.FindAllString(appLog.String(), -1)); warns != 0 {
		t.Errorf("the suite scaffold must boot and serve the walk above with zero WARN lines (slog or the standard logger), got %d:\n%s", warns, appLog.String())
	}
}

// warnLine matches a WARN line from either logger of the suite app: slog's
// text handler (`level=WARN`) and the standard logger Quark writes to
// (`… WARN …`, the level as a bare word).
var warnLine = regexp.MustCompile(`(?m)^.*(level=WARN|\bWARN\b).*$`)
