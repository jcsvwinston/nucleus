// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package catalogbench

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/internal/cli"
	"github.com/jcsvwinston/nucleus/internal/knownproviders"
)

// env carries what the probes share: the CLI, one scaffolded starter built
// against this checkout, and the projects derived from it by `nucleus add`.
//
// The starter is `nucleus new --template api --offline`, the smallest project
// the CLI writes, pinned to this checkout with replace directives the way the
// CLI's own build tests pin it. Every module this repository publishes gets a
// replace too, so `nucleus add <x>` resolves the module from the checkout
// under measurement and not from whatever the proxy holds. A probe that needs
// a project copies the starter rather than scaffolding again; the Go build
// cache makes the copies cheap to build.
type env struct {
	tb   testing.TB
	root string // the nucleus checkout under measurement
	work string // where every project the probes write lives

	cliMu sync.Mutex // cli.Run keeps output-style globals; one call at a time

	dryOnce sync.Once
	dry     string

	baseOnce sync.Once
	base     *project
	baseLog  string
	baseErr  error

	mu       sync.Mutex
	variants map[string]*variant
	prepOnce sync.Once
	prepWG   sync.WaitGroup

	fakeOnce sync.Once
	fake     *fakeEndpoint

	idpOnce sync.Once
	idpSrv  *standInIdP

	sentryOnce sync.Once
	sentrySrv  *standInSentry

	stripeOnce sync.Once
	stripeSrv  *standInStripe
}

type project struct {
	dir string
	bin string
}

func newEnv(tb testing.TB) *env {
	work, err := os.MkdirTemp("", "catalogbench-")
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = os.RemoveAll(work) })
	e := &env{tb: tb, root: repoRoot(tb), work: work, variants: map[string]*variant{}}
	// A run filtered to one entry still started every module build; let
	// them finish before their directories go.
	tb.Cleanup(e.prepWG.Wait)
	return e
}

// ---- the CLI ---------------------------------------------------------------

type cliResult struct {
	code   int
	stdout string
	stderr string
}

func (r cliResult) all() string { return r.stdout + r.stderr }

// cli runs the real command line in-process, exactly as `nucleus <args>`.
func (e *env) cli(args ...string) cliResult {
	e.cliMu.Lock()
	defer e.cliMu.Unlock()
	var out, errb bytes.Buffer
	code := cli.Run(args, strings.NewReader(""), &out, &errb)
	return cliResult{code: code, stdout: out.String(), stderr: errb.String()}
}

// dryDir is a module root a dry run can point at: `nucleus add --dry-run`
// needs a go.mod and a file to name, and changes neither.
func (e *env) dryDir() string {
	e.dryOnce.Do(func() {
		dir := filepath.Join(e.work, "dry")
		must(e.tb, os.MkdirAll(dir, 0o755))
		must(e.tb, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/dry\n\ngo 1.26\n"), 0o644))
		must(e.tb, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
		e.dry = dir
	})
	return e.dry
}

// dryRun is `nucleus add <name> --dry-run` against the dry module.
func (e *env) dryRun(name string) cliResult {
	return e.cli("add", name, "--dry-run", "--dir", e.dryDir())
}

// goGetTargets returns every `go get` target a command's output names, as
// written — module path plus @version when there is one. One `go get` can
// name several modules, and a hand-back line chains several `go get`s.
func goGetTargets(out string) []string {
	var targets []string
	for _, chunk := range strings.Split(out, "go get ")[1:] {
		for _, tok := range strings.Fields(chunk) {
			tok = strings.Trim(tok, "`\"'")
			if !strings.Contains(tok, "/") || !strings.Contains(tok, ".") || strings.ContainsAny(tok, "&#=") {
				break
			}
			targets = append(targets, tok)
		}
	}
	return targets
}

// moduleOf strips the version from a `go get` target.
func moduleOf(target string) string {
	if i := strings.Index(target, "@"); i >= 0 {
		return target[:i]
	}
	return target
}

// catalogEntry is a name `nucleus add` accepts and the target it resolves to.
type catalogEntry struct {
	name   string
	target string // as the dry run prints it: module[@version]
}

// candidateNames are the names the bench asks `nucleus add` about: the ones
// the CLI's own table publishes (read from it, so a new entry is asked about
// without editing the bench), the secrets resolver the table keys by scheme,
// and the fifteen entries the A11 catalog names. Order matters only for
// which spelling of a module is reported first.
func candidateNames() []string {
	names := []string{"postgres", "mysql", "sqlite", "sqlserver", "oracle"}
	for _, n := range knownproviders.DBDriverNames() {
		p, _ := knownproviders.DBDriver(n)
		names = append(names, p.Name)
	}
	names = append(names, knownproviders.TelemetryExporterNames()...)
	names = append(names, knownproviders.StorageProviderNames()...)
	names = append(names, knownproviders.AuthBackendNames()...)
	names = append(names, "aws-sm")
	return append(names, arcEntries...)
}

// arcEntries are the fifteen entries the A11 catalog is to install.
var arcEntries = []string{
	"oidc", "saml", "apikeys", "accounts", "sql-queue", "redis-cache", "websockets",
	"stripe", "sentry", "s3", "gcs", "azure", "ldap", "otlp", "prometheus",
}

// catalog lists what `nucleus add` accepts, one row per module.
func (e *env) catalog() []catalogEntry {
	seen := map[string]bool{}
	var out []catalogEntry
	for _, name := range candidateNames() {
		r := e.dryRun(name)
		if r.code != 0 {
			continue
		}
		targets := goGetTargets(r.stdout)
		if len(targets) == 0 {
			continue
		}
		if seen[moduleOf(targets[0])] {
			continue
		}
		seen[moduleOf(targets[0])] = true
		out = append(out, catalogEntry{name: name, target: targets[0]})
	}
	return out
}

// ---- the starter and the projects derived from it -------------------------

// scaffold returns the pinned, tidied and built api starter.
func (e *env) scaffold(t *testing.T) *project {
	t.Helper()
	if testing.Short() {
		t.Skip("-short: this probe scaffolds, builds and boots an application")
	}
	p, err := e.baseProject()
	if err != nil {
		skipIfOffline(t, e.baseLog)
		t.Fatalf("the api starter does not build against this checkout: %v\n%s", err, e.baseLog)
	}
	return p
}

func (e *env) baseProject() (*project, error) {
	e.baseOnce.Do(func() {
		out := filepath.Join(e.work, "base")
		r := e.cli("new", "benchapp", "--out", out, "--template", "api", "--module", "example.com/benchapp", "--offline")
		if r.code != 0 {
			e.baseLog, e.baseErr = r.all(), fmt.Errorf("nucleus new exited %d", r.code)
			return
		}
		dir := filepath.Join(out, "benchapp")
		if err := pinToCheckout(dir, e.root); err != nil {
			e.baseErr = err
			return
		}
		if log, err := goRun(dir, "mod", "tidy"); err != nil {
			e.baseLog, e.baseErr = log, err
			return
		}
		if log, err := goRun(dir, "build", "-o", exeName("app"), "."); err != nil {
			e.baseLog, e.baseErr = log, err
			return
		}
		e.base = &project{dir: dir, bin: filepath.Join(dir, exeName("app"))}
	})
	return e.base, e.baseErr
}

var nucleusRequire = regexp.MustCompile(`(?m)^require github\.com/jcsvwinston/nucleus v\S+$`)

// pinToCheckout points the starter at this checkout: the framework and its
// SQLite driver through require+replace (the scaffold imports both), and
// every other module this repository publishes through a replace alone, so
// that `nucleus add` — which runs `go get <module>@<released version>`, and a
// replace without a version covers every version — resolves the module from
// the checkout under measurement.
func pinToCheckout(dir, root string) error {
	goMod := filepath.Join(dir, "go.mod")
	raw, err := os.ReadFile(goMod)
	if err != nil {
		return err
	}
	pinned := nucleusRequire.ReplaceAllString(string(raw), "require github.com/jcsvwinston/nucleus v0.0.0")
	if pinned == string(raw) {
		return fmt.Errorf("the starter's go.mod has no nucleus require line:\n%s", raw)
	}
	var b strings.Builder
	b.WriteString(pinned)
	fmt.Fprintf(&b, "\nreplace github.com/jcsvwinston/nucleus => %q\n", root)
	fmt.Fprintf(&b, "\nrequire github.com/jcsvwinston/nucleus/drivers/sqlite v0.0.0\n")
	for _, m := range repoModules(root) {
		fmt.Fprintf(&b, "\nreplace %s => %q\n", m.path, m.dir)
	}
	return os.WriteFile(goMod, []byte(b.String()), 0o644)
}

type repoModule struct{ path, dir, rel string }

// repoModules lists the optional modules this repository publishes, read
// from their go.mod files.
func repoModules(root string) []repoModule {
	var out []repoModule
	for _, parent := range []string{"drivers", "exporters", "providers"} {
		entries, _ := os.ReadDir(filepath.Join(root, parent))
		for _, d := range entries {
			if !d.IsDir() {
				continue
			}
			dir := filepath.Join(root, parent, d.Name())
			raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			if err != nil {
				continue
			}
			first, _, _ := strings.Cut(string(raw), "\n")
			path := strings.TrimSpace(strings.TrimPrefix(first, "module"))
			out = append(out, repoModule{path: path, dir: dir, rel: parent + "/" + d.Name()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

// variant is the starter after `nucleus add <name>`, then built.
type variant struct {
	addOnce, buildOnce sync.Once
	dir                string
	add                cliResult
	addErr             error
	bin                string
	buildLog           string
	buildErr           error
}

func (e *env) variantFor(name string) *variant {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.variants[name]
	if !ok {
		v = &variant{}
		e.variants[name] = v
	}
	return v
}

// prepareAdd copies the starter and runs `nucleus add <name>` in the copy.
// No *testing.T: the module entries are prepared concurrently.
func (e *env) prepareAdd(name string) *variant {
	v := e.variantFor(name)
	v.addOnce.Do(func() {
		base, err := e.baseProject()
		if err != nil {
			v.addErr = err
			return
		}
		dir, err := os.MkdirTemp(e.work, "add-"+name+"-")
		if err != nil {
			v.addErr = err
			return
		}
		if err := copyProject(base.dir, dir); err != nil {
			v.addErr = err
			return
		}
		v.dir = dir
		v.add = e.cli("add", name, "--dir", dir)
	})
	return v
}

// prepareBuild builds the project prepareAdd left, the way its author
// would: `go build` with the module graph read-only.
func (e *env) prepareBuild(name string) *variant {
	v := e.prepareAdd(name)
	v.buildOnce.Do(func() {
		if v.addErr != nil {
			v.buildErr = v.addErr
			return
		}
		if v.add.code != 0 {
			v.buildErr = fmt.Errorf("nucleus add %s exited %d", name, v.add.code)
			return
		}
		v.buildLog, v.buildErr = goRun(v.dir, "build", "-o", exeName("app"), ".")
		v.bin = filepath.Join(v.dir, exeName("app"))
	})
	return v
}

func (e *env) added(t *testing.T, name string) *variant {
	t.Helper()
	e.scaffold(t)
	v := e.prepareAdd(name)
	if v.addErr != nil {
		t.Fatalf("prepare %s: %v", name, v.addErr)
	}
	skipIfOffline(t, v.add.all())
	return v
}

func (e *env) built(t *testing.T, name string) *variant {
	t.Helper()
	e.scaffold(t)
	e.prepareModules()
	v := e.prepareBuild(name)
	if v.addErr != nil {
		t.Fatalf("prepare %s: %v", name, v.addErr)
	}
	skipIfOffline(t, v.add.all())
	skipIfOffline(t, v.buildLog)
	return v
}

// prepareModules adds and builds every module-backed entry concurrently,
// two at a time: each build already uses every core, and the first build of
// a cloud SDK is the slow part of this bench.
func (e *env) prepareModules() {
	e.prepOnce.Do(func() {
		if _, err := e.baseProject(); err != nil {
			return
		}
		sem := make(chan struct{}, 2)
		for _, m := range moduleEntries {
			name := m.name
			e.prepWG.Add(1)
			go func() {
				defer e.prepWG.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				e.prepareBuild(name)
			}()
		}
	})
}

// copyProject copies a project tree, leaving out what a build or a boot
// left behind.
func copyProject(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." {
			return nil
		}
		base := filepath.Base(path)
		if base == exeName("app") || strings.HasPrefix(base, "app.db") || strings.HasSuffix(base, ".log") {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	})
}

// goRun runs the go command in dir, outside any workspace and with the
// module graph read-only unless the subcommand writes it.
func goRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("go %s: %v\n%s", strings.Join(args, " "), err, out), err
	}
	return string(out), nil
}

var offlineSignals = []string{
	"dial tcp", "no such host", "proxy.golang.org", "module lookup disabled",
	"GOPROXY=off", "i/o timeout", "TLS handshake timeout", "server misbehaving",
}

// skipIfOffline skips when the go command could not reach the module proxy:
// a probe that needs a module download and cannot get one has measured the
// network, not the catalog, and must not record a verdict.
func skipIfOffline(t *testing.T, goOutput string) {
	t.Helper()
	for _, s := range offlineSignals {
		if strings.Contains(goOutput, s) {
			t.Skipf("the module proxy is unreachable from this machine (%q); nothing was measured:\n%s", s, goOutput)
		}
	}
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// ---- booting a project ------------------------------------------------------

// boot is what a project said and did when started.
type boot struct {
	listening bool   // it reported the server listening
	output    string // everything it wrote, start to stop
	exitErr   error  // why it stopped before listening, if it did
}

// syncBuffer is a bytes.Buffer two goroutines can share.
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

// bootWith starts bin in a fresh directory holding config as nucleus.yml,
// waits until it says it is listening or it exits, calls visit with its port
// while it is up, and stops it.
func bootWith(t *testing.T, bin, config string, extraEnv []string, visit func(port int)) boot {
	t.Helper()
	return bootIn(t, t.TempDir(), bin, config, extraEnv, func(port int, _ func() string) {
		if visit != nil {
			visit(port)
		}
	})
}

// bootIn is bootWith in a directory the caller names — the application's
// SQLite database lands there, for a probe that reads it — with what the
// application has logged so far handed to visit.
func bootIn(t *testing.T, dir, bin, config string, extraEnv []string, visit func(port int, output func() string)) boot {
	t.Helper()
	must(t, os.WriteFile(filepath.Join(dir, "nucleus.yml"), []byte(config), 0o644))
	port := freePort(t)
	cmd := exec.Command(bin)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(),
		fmt.Sprintf("NUCLEUS_PORT=%d", port),
		"NUCLEUS_HOST=127.0.0.1",
		"NUCLEUS_LOG_LEVEL=info",
		// No probe reaches a real cloud: AWS clients skip the instance
		// metadata service, and a GCS client talks to the bench's endpoint.
		"AWS_EC2_METADATA_DISABLED=true",
	), extraEnv...)
	var out syncBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var b boot
	deadline := time.After(45 * time.Second)
wait:
	for {
		select {
		case err := <-done:
			b.exitErr = err
			if b.exitErr == nil {
				b.exitErr = fmt.Errorf("exited without listening")
			}
			b.output = out.String()
			return b
		case <-deadline:
			break wait
		case <-time.After(50 * time.Millisecond):
			// The framework logs "server listening" just before it binds
			// the port, so the line alone lets a probe dial a port nothing
			// accepts on yet ("connection refused", seen in CI on EN-09).
			// Listening is the line AND a port that takes a connection.
			if strings.Contains(out.String(), "nucleus: server listening") && accepts(port) {
				b.listening = true
				break wait
			}
		}
	}
	if b.listening && visit != nil {
		visit(port, out.String)
	}
	_ = cmd.Process.Kill()
	<-done
	b.output = out.String()
	if !b.listening {
		b.exitErr = fmt.Errorf("no listening line within the deadline")
	}
	return b
}

// accepts reports whether something takes a TCP connection on the port.
func accepts(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// firstLines shortens a boot's output for a log line.
func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	return strings.Join(lines, "\n")
}

// get fetches path from an application on port and returns the status and
// the body.
func get(port int, path string) (int, string) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
	if err != nil {
		return 0, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// fakeEndpoint stands in for an object store so a storage provider that
// checks its bucket at start-up can do so without a cloud: it answers the
// bucket-location query S3 clients ask and 200 to everything else, and
// remembers what it was asked.
type fakeEndpoint struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen []string
}

func (e *env) fakeStore() *fakeEndpoint {
	e.fakeOnce.Do(func() {
		f := &fakeEndpoint{}
		f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.seen = append(f.seen, r.Method+" "+r.URL.RequestURI())
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/xml")
			if _, ok := r.URL.Query()["location"]; ok {
				_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		e.tb.Cleanup(f.srv.Close)
		e.fake = f
	})
	return e.fake
}

func (f *fakeEndpoint) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seen...)
}

// ---- the repository tree ---------------------------------------------------

// repoRoot finds the nucleus module root from the test's working directory.
func repoRoot(tb testing.TB) string {
	tb.Helper()
	dir, err := os.Getwd()
	if err != nil {
		tb.Fatal(err)
	}
	for {
		if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil &&
			strings.Contains(string(b), "module github.com/jcsvwinston/nucleus\n") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatal("repository root not found")
		}
		dir = parent
	}
}

// sourceMatches returns the non-test Go files under rel (relative to the
// repository root) whose contents match re. Reading the source is how a probe
// measures a capability that only exists as an API the author would call —
// the alternative, a probe that compiles against a name nobody has chosen,
// cannot be written.
func sourceMatches(tb testing.TB, rel string, re *regexp.Regexp) []string {
	tb.Helper()
	root := repoRoot(tb)
	var out []string
	_ = filepath.WalkDir(filepath.Join(root, rel), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err == nil && re.Match(b) {
			r, _ := filepath.Rel(root, path)
			out = append(out, r)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// skipDir names the directories no probe searches: the site's toolchain, VCS
// metadata, and this bench, whose probes name what they look for.
func skipDir(name string) bool {
	switch name {
	case "node_modules", ".git", ".docusaurus", "build", "catalogbench":
		return true
	}
	return false
}

// existingDirs returns the candidates (relative to the repository root)
// that exist as directories.
func existingDirs(tb testing.TB, candidates ...string) []string {
	tb.Helper()
	root := repoRoot(tb)
	var out []string
	for _, c := range candidates {
		if st, err := os.Stat(filepath.Join(root, c)); err == nil && st.IsDir() {
			out = append(out, c)
		}
	}
	return out
}

// goModsRequiring returns the go.mod files in the repository that require
// any of the modules given.
func goModsRequiring(tb testing.TB, modules ...string) []string {
	tb.Helper()
	root := repoRoot(tb)
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDir(d.Name()) || d.Name() == "website" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
			return nil
		}
		raw, _ := os.ReadFile(path)
		for _, m := range modules {
			if strings.Contains(string(raw), m) {
				r, _ := filepath.Rel(root, path)
				out = append(out, r+" ("+m+")")
			}
		}
		return nil
	})
	return out
}

// freePort reserves an ephemeral port for an application the probe runs.
func freePort(tb testing.TB) int {
	tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// writeExecutable writes a script plugin. The plugins the probes run are
// shell scripts: the envelope is the contract, and a script is the smallest
// program that speaks it.
func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	must(t, os.WriteFile(path, []byte(body), 0o755))
}

func requireShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the plugin probes run shell-script plugins; Windows is measured where the plugin host is")
	}
}

func must(tb testing.TB, err error) {
	tb.Helper()
	if err != nil {
		tb.Fatal(err)
	}
}
