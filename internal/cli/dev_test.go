// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/internal/routedump"
)

// TestMain doubles as the application `nucleus dev` runs in the tests that
// use the fake builder: the test binary copied to the build output and
// started with NUCLEUS_DEV_HELPER=serve behaves like a small application
// — it listens on NUCLEUS_PORT, answers "/" with the build tag its
// builder wrote next to it, prints a route table under
// NUCLEUS_PRINT_ROUTES and exits on SIGTERM — so a restart is observable
// end to end without compiling anything. NUCLEUS_DEV_HELPER=loop runs the
// real command under the fake builder for the signal test.
func TestMain(m *testing.M) {
	switch os.Getenv("NUCLEUS_DEV_HELPER") {
	case "serve":
		devHelperServe()
		os.Exit(0)
	case "loop":
		devHelperLoop()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func devHelperServe() {
	tag, _ := os.ReadFile(os.Args[0] + ".tag")
	if routedump.Enabled(os.Getenv(routedump.EnvVar)) {
		_ = routedump.Encode(os.Stdout, routedump.Document{Env: os.Getenv("NUCLEUS_ENV"), Routes: []routedump.Route{
			{Method: "GET", Pattern: "/healthz"},
			{Method: "GET", Pattern: "/notes", Module: "notes"},
		}})
		return
	}
	addr := net.JoinHostPort(envOr("NUCLEUS_HOST", "127.0.0.1"), envOr("NUCLEUS_PORT", "0"))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: listen %s: %v\n", addr, err)
		os.Exit(1)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "build %s env %s path %s", strings.TrimSpace(string(tag)), os.Getenv("NUCLEUS_ENV"), r.URL.Path)
	})}
	go func() {
		<-devHelperStopSignal()
		_ = srv.Close()
	}()
	fmt.Fprintf(os.Stdout, "helper: listening on %s (build %s)\n", ln.Addr(), strings.TrimSpace(string(tag)))
	_ = srv.Serve(ln)
}

func devHelperLoop() {
	// The loop's own child inherits this environment; it must behave as
	// the application, not as another loop.
	_ = os.Setenv("NUCLEUS_DEV_HELPER", "serve")
	devBuild = fakeDevBuild(nil, nil)
	if err := runDev([]string{"--dir", os.Getenv("NUCLEUS_DEV_HELPER_DIR"), "--port", os.Getenv("NUCLEUS_DEV_HELPER_PORT")}, strings.NewReader(""), os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// fakeDevBuild is a builder that compiles nothing: it copies the test
// binary to the output path and writes the generation next to it. fail
// decides, per generation, whether the build fails (with output "boom");
// builds counts the calls.
func fakeDevBuild(builds *atomic.Int32, fail func(generation int) bool) func(context.Context, string, string) (string, error) {
	var generation atomic.Int32
	return func(_ context.Context, _ string, bin string) (string, error) {
		gen := int(generation.Add(1))
		if builds != nil {
			builds.Add(1)
		}
		if fail != nil && fail(gen) {
			return "./main.go:3:1: boom: syntax error (build " + strconv.Itoa(gen) + ")", fmt.Errorf("exit status 1")
		}
		self, err := os.Executable()
		if err != nil {
			return "", err
		}
		src, err := os.ReadFile(self)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(bin, src, 0o755); err != nil {
			return "", err
		}
		return "", os.WriteFile(bin+".tag", []byte(strconv.Itoa(gen)), 0o644)
	}
}

// syncBuffer is a bytes.Buffer the application's pass-through writes and
// the test's reads can share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// waitFor polls cond for up to timeout.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// httpGet returns the body of GET url, or "" when nothing answers.
func httpGet(url string) (int, string) {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(url)
	if err != nil {
		return 0, ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// writeDevProject writes a minimal Go project (go.mod, main.go, nucleus.yml)
// for the fake builder; nothing in it is compiled.
func writeDevProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":      "module example.com/devproject\n\ngo 1.22\n",
		"main.go":     "package main\n\nfunc main() {}\n",
		"nucleus.yml": "port: 8080\nhost: 127.0.0.1\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// startDevLoop runs devLoop in the background and returns the output
// buffer, the cancel and a wait that returns the loop's result (callable
// more than once; the cleanup calls it too).
func startDevLoop(t *testing.T, opts devOptions) (*syncBuffer, context.CancelFunc, func() error) {
	t.Helper()
	out := &syncBuffer{}
	opts.stdout = out
	opts.stderr = out
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	var result error
	go func() {
		result = devLoop(ctx, opts)
		close(finished)
	}()
	wait := func() error {
		select {
		case <-finished:
			return result
		case <-time.After(15 * time.Second):
			t.Fatalf("devLoop did not return after cancel; output so far:\n%s", out.String())
			return nil
		}
	}
	t.Cleanup(func() {
		cancel()
		wait()
	})
	return out, cancel, wait
}

// The loop builds, starts the binary on --port with NUCLEUS_ENV set, and
// on a change to a watched file rebuilds and restarts: the same port then
// answers from the new build. A build error keeps the previous binary
// serving and prints the compiler's output; the next change rebuilds
// again. Cancelling stops the application and removes the build
// directory.
func TestDevRebuildsAndRestartsOnChange(t *testing.T) {
	dir := writeDevProject(t)
	tmpRoot := t.TempDir()
	t.Setenv("TMPDIR", tmpRoot)
	t.Setenv("NUCLEUS_DEV_HELPER", "serve")
	port := freeLoopbackPort(t)
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/"

	var builds atomic.Int32
	devBuild = fakeDevBuild(&builds, func(gen int) bool { return gen == 2 })
	t.Cleanup(func() { devBuild = buildMainPackage })

	out, cancel, wait := startDevLoop(t, devOptions{dir: dir, root: dir, port: port, debounce: 100 * time.Millisecond, printRoutes: true})

	waitFor(t, 10*time.Second, "the first build to answer", func() bool {
		_, body := httpGet(url)
		return strings.HasPrefix(body, "build 1 env development")
	})
	if !strings.Contains(out.String(), "[dev] build #1 ok") {
		t.Errorf("the first build must be reported:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "GET     /notes  (notes)") {
		t.Errorf("--print-routes must print the route table of the built binary:\n%s", out.String())
	}

	// A change → build #2, which fails: the first build keeps serving.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { broken }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "build #2 to fail", func() bool {
		return strings.Contains(out.String(), "[dev] build #2 failed:")
	})
	if !strings.Contains(out.String(), "boom: syntax error") {
		t.Errorf("the compiler output must be printed:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "keeping the last good binary running (build #1)") {
		t.Errorf("a failed build must keep the previous binary:\n%s", out.String())
	}
	if _, body := httpGet(url); !strings.HasPrefix(body, "build 1 ") {
		t.Errorf("the previous build must still answer after a failed build, got %q", body)
	}

	// Another change → build #3, which succeeds and replaces build #1.
	if err := os.WriteFile(filepath.Join(dir, "migrations", "001_init.up.sql"), []byte("select 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 15*time.Second, "build #3 to answer on the port", func() bool {
		_, body := httpGet(url)
		return strings.HasPrefix(body, "build 3 ")
	})
	if !strings.Contains(out.String(), "[dev] stopped the previous application") {
		t.Errorf("the restart must be reported:\n%s", out.String())
	}

	// A file the loop does not watch changes nothing.
	before := builds.Load()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if got := builds.Load(); got != before {
		t.Errorf("a change to an unwatched file triggered a build (%d -> %d)", before, got)
	}

	cancel()
	if err := wait(); err != nil {
		t.Fatalf("devLoop returned %v on cancel", err)
	}
	waitForPortToClose(t, "127.0.0.1:"+strconv.Itoa(port))
	leftovers, _ := filepath.Glob(filepath.Join(tmpRoot, "nucleus-dev-*"))
	if len(leftovers) != 0 {
		t.Errorf("the build directory must be removed when the loop ends, left: %v", leftovers)
	}
}

// With --proxy the command listens on the port itself: the proxy paths go
// to the front-end dev server, everything else to the application on its
// loopback port; while nothing answers there, the front says so with a
// 503 instead of refusing the connection.
func TestDevProxyForwardsAssetsAndTheRest(t *testing.T) {
	dir := writeDevProject(t)
	t.Setenv("NUCLEUS_DEV_HELPER", "serve")
	frontEnd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "asset %s", r.URL.Path)
	}))
	t.Cleanup(frontEnd.Close)
	proxyURL, err := parseDevProxyURL(frontEnd.URL)
	if err != nil {
		t.Fatal(err)
	}
	port := freeLoopbackPort(t)
	base := "http://127.0.0.1:" + strconv.Itoa(port)

	// The first build never finishes until released, so the front is up
	// while the application is not.
	release := make(chan struct{})
	inner := fakeDevBuild(nil, nil)
	devBuild = func(ctx context.Context, dir, bin string) (string, error) {
		<-release
		return inner(ctx, dir, bin)
	}
	t.Cleanup(func() { devBuild = buildMainPackage })

	out, _, _ := startDevLoop(t, devOptions{dir: dir, root: dir, port: port, proxy: proxyURL, proxyPaths: splitDevProxyPaths(defaultDevProxyPaths)})

	waitFor(t, 10*time.Second, "the front to listen", func() bool {
		code, _ := httpGet(base + "/")
		return code != 0
	})
	if code, body := httpGet(base + "/"); code != http.StatusServiceUnavailable || !strings.Contains(body, "not answering") {
		t.Errorf("before the application is up the front must answer 503 saying so, got %d %q", code, body)
	}
	if code, body := httpGet(base + "/static/app.css"); code != http.StatusOK || body != "asset /static/app.css" {
		t.Errorf("/static must reach the front-end dev server, got %d %q", code, body)
	}
	if code, body := httpGet(base + "/assets"); code != http.StatusOK || body != "asset /assets" {
		t.Errorf("/assets (bare prefix) must reach the front-end dev server, got %d %q", code, body)
	}
	close(release)
	waitFor(t, 10*time.Second, "the application to answer through the front", func() bool {
		_, body := httpGet(base + "/api/notes")
		return strings.HasPrefix(body, "build 1 env development path /api/notes")
	})
	if !strings.Contains(out.String(), "[dev] proxy listening on") {
		t.Errorf("the proxy must announce itself:\n%s", out.String())
	}
}

// watchedDevFile is the filter: sources, the config, the policy file and
// the migrations and templates trees rebuild; editor artefacts and the
// rest do not.
func TestDevWatchedFiles(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "proj")
	cases := map[string]bool{
		"main.go":                          true,
		"internal/notes/notes_test.go":     true,
		"nucleus.yml":                      true,
		"rbac_policy.csv":                  true,
		"migrations/001_init.up.sql":       true,
		"internal/notes/migrations/1.sql":  true,
		"templates/notes/index.html":       true,
		"internal/notes/templates/x.html":  true,
		"README.md":                        false,
		"notes.txt":                        false,
		".main.go.swp":                     false,
		"main.go~":                         false,
		"internal/.hidden.go":              false,
		"static/app.css":                   false,
		"templates":                        false,
		"migrations":                       false,
		"config/settings.yml":              false,
		"internal/notes/templates_test.md": false,
	}
	for rel, want := range cases {
		if got := watchedDevFile(root, filepath.Join(root, filepath.FromSlash(rel))); got != want {
			t.Errorf("watchedDevFile(%q) = %v, want %v", rel, got, want)
		}
	}
	if watchedDevFile(root, filepath.Join(string(filepath.Separator), "elsewhere", "main.go")) {
		t.Errorf("a path outside the root must not be watched")
	}
}

// The flags are validated before anything is built or watched.
func TestDevRejectsBadFlagsAndNonProjects(t *testing.T) {
	outside := t.TempDir()
	var out, errOut bytes.Buffer
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--dir", outside}, "no go.mod at or above"},
		{[]string{"--dir", outside, "extra"}, "usage: nucleus dev [flags]"},
		{[]string{"--dir", outside, "--port", "70000"}, "invalid --port"},
		{[]string{"--dir", outside, "--proxy", "::not a url"}, "invalid --proxy"},
		{[]string{"--dir", outside, "--proxy", "ftp://host"}, "expected an http or https URL"},
		{[]string{"--dir", outside, "--proxy", "localhost:5173", "--proxy-paths", ",/"}, "--proxy-paths must name"},
		{[]string{"--dir", outside, "--debounce", "0s"}, "--debounce must be positive"},
	}
	for _, tc := range cases {
		out.Reset()
		errOut.Reset()
		code := Run(append([]string{"dev"}, tc.args...), strings.NewReader(""), &out, &errOut)
		if code == 0 {
			t.Errorf("nucleus dev %s succeeded, want an error", strings.Join(tc.args, " "))
			continue
		}
		if !strings.Contains(errOut.String(), tc.want) {
			t.Errorf("nucleus dev %s: want %q in stderr, got:\n%s", strings.Join(tc.args, " "), tc.want, errOut.String())
		}
	}
	u, err := parseDevProxyURL("localhost:5173")
	if err != nil || u.String() != "http://localhost:5173" {
		t.Errorf("a bare host:port must be completed to http, got %v %v", u, err)
	}
	if got := splitDevProxyPaths(" /static/ ,assets,,/ "); strings.Join(got, ",") != "/static,/assets" {
		t.Errorf("splitDevProxyPaths normalised to %v", got)
	}
}

// The real thing: a scaffolded project is built with go build, served on
// a free port, rebuilt when main.go changes and kept serving when the
// edit does not compile.
func TestDevRunsAScaffoldedProject(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs a scaffolded app; skipped with -short")
	}
	projectDir := scaffoldProjectWithModule(t)
	t.Setenv("GOFLAGS", "-mod=mod")
	tmpRoot := t.TempDir()
	t.Setenv("TMPDIR", tmpRoot)
	port := freeLoopbackPort(t)
	healthz := "http://127.0.0.1:" + strconv.Itoa(port) + "/healthz"

	out, cancel, wait := startDevLoop(t, devOptions{dir: projectDir, root: projectDir, port: port, debounce: 100 * time.Millisecond})

	waitFor(t, 120*time.Second, "the scaffold to build and answer /healthz", func() bool {
		code, _ := httpGet(healthz)
		return code != 0
	})
	if !strings.Contains(out.String(), "[dev] build #1 ok") {
		t.Fatalf("the first build must be reported:\n%s", out.String())
	}

	// An edit that compiles → rebuild and restart.
	mainPath := filepath.Join(projectDir, "main.go")
	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, append([]byte("// edited by the dev test\n"), src...), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 120*time.Second, "build #2 to start", func() bool {
		return strings.Contains(out.String(), "[dev] started build #2")
	})
	waitFor(t, 30*time.Second, "the restarted application to answer", func() bool {
		code, _ := httpGet(healthz)
		return code != 0
	})

	// An edit that does not compile → the compiler output, build #2 keeps serving.
	if err := os.WriteFile(mainPath, []byte("package main\n\nfunc main() { this does not compile }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 120*time.Second, "build #3 to fail", func() bool {
		return strings.Contains(out.String(), "[dev] build #3 failed:")
	})
	if !strings.Contains(out.String(), "keeping the last good binary running (build #2)") {
		t.Errorf("the failed build must keep build #2:\n%s", out.String())
	}
	if code, _ := httpGet(healthz); code == 0 {
		t.Errorf("build #2 must still answer after the failed build")
	}

	cancel()
	if err := wait(); err != nil {
		t.Fatalf("devLoop returned %v on cancel", err)
	}
	waitForPortToClose(t, "127.0.0.1:"+strconv.Itoa(port))
	leftovers, _ := filepath.Glob(filepath.Join(tmpRoot, "nucleus-dev-*"))
	if len(leftovers) != 0 {
		t.Errorf("the build directory must be removed, left: %v", leftovers)
	}
}
