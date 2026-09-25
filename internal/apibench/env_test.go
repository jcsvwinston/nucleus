// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package apibench

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// env carries what several probes share: one booted application, started at
// most once per run and torn down by the parent test.
//
// The application is a default one plus ONE module, "bench", whose routes are
// the fixtures the HTTP probes need: a handler that binds JSON, one that
// returns a domain error, one that lists. The module is the smallest thing an
// application author writes; what the probes ask of it is what that author
// gets for free. Probes that need another shape of application boot their own
// and say so.
type env struct {
	tb   testing.TB
	once sync.Once
	srv  *nucleustest.Server
}

func newEnv(tb testing.TB) *env { return &env{tb: tb} }

func (e *env) server() *nucleustest.Server {
	e.once.Do(func() {
		e.srv = startWith(e.tb, benchModule())
	})
	return e.srv
}

// startWith boots an application with the given modules mounted, the
// authorizer open (the probes measure the HTTP surface, not the policies) and
// the bench configuration.
func startWith(tb testing.TB, modules ...nucleus.ModuleSpec) *nucleustest.Server {
	tb.Helper()
	a := buildWith(tb, nil, modules...)
	return nucleustest.StartApp(tb, a)
}

// buildWith builds (does not run) an application: open authorizer, the bench
// configuration, the modules given, and whatever the caller adds to the
// builder first.
func buildWith(tb testing.TB, extra func(*nucleus.AppBuilder), modules ...nucleus.ModuleSpec) nucleus.App {
	tb.Helper()
	b := nucleus.New().WithOpenAuthz()
	if extra != nil {
		extra(b)
	}
	if len(modules) > 0 {
		b = b.Mount(modules...)
	}
	a, err := b.Build()
	if err != nil {
		tb.Fatalf("build application: %v", err)
	}
	a.Config = benchConfig(tb)
	return a
}

func benchConfig(tb testing.TB) app.Config {
	tb.Helper()
	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.JWTSecret = strings.Repeat("apibench-probe-secret", 2)
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(tb.TempDir(), "apibench.db") + "?_pragma=busy_timeout(10000)"},
	}
	return cfg
}

// echoInput is the request body the binding probes send. One required field
// and one typed one: enough to see whether binding validates and how a
// validation failure is reported.
type echoInput struct {
	Name string `json:"name" validate:"required"`
	Age  int    `json:"age"`
}

// benchModule is the fixture module.
func benchModule() nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name:       "bench",
		Prefix:     "/bench",
		CSRFExempt: []string{"/echo"},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Post("/echo", func(c *nucleus.Context) error {
				var in echoInput
				if err := c.BindJSON(&in); err != nil {
					return err
				}
				return c.JSON(http.StatusOK, in)
			})
			r.Get("/notfound", func(c *nucleus.Context) error {
				return gferrors.NotFound("thing", "1")
			})
			r.Get("/things", func(c *nucleus.Context) error {
				return c.JSON(http.StatusOK, []map[string]any{{"id": 1, "name": "one"}})
			})
		},
	}.Build()
}

// do sends a request to the shared server and returns the response and its
// body. A non-nil body is JSON-encoded.
func (e *env) do(t *testing.T, method, path string, body any, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	return doOn(t, e.server(), method, path, body, headers)
}

func doOn(t *testing.T, srv *nucleustest.Server, method, path string, body any, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, srv.URL(path), rd)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

// jsonKeys returns the sorted top-level keys of a JSON object, or nil when
// the body is not one.
func jsonKeys(raw []byte) []string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---- reflection over the public API ----------------------------------------

// methodNames lists the exported methods of v's type (pointer receivers
// included when v is a pointer).
func methodNames(v any) []string {
	t := reflect.TypeOf(v)
	names := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		names = append(names, t.Method(i).Name)
	}
	sort.Strings(names)
	return names
}

// anyMethod returns the first candidate that is one of the names — the way a
// probe asks "does the kit offer X under any reasonable name" without
// deciding the name for the author.
func anyMethod(names []string, candidates ...string) (string, bool) {
	for _, c := range candidates {
		for _, n := range names {
			if strings.EqualFold(n, c) {
				return n, true
			}
		}
	}
	return "", false
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
// the alternative, a probe that compiles against a name the author has not
// chosen, cannot be written.
func sourceMatches(tb testing.TB, rel string, re *regexp.Regexp) []string {
	tb.Helper()
	root := filepath.Join(repoRoot(tb), rel)
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err == nil && re.Match(b) {
			r, _ := filepath.Rel(repoRoot(tb), path)
			out = append(out, r)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// freePort reserves an ephemeral port for an application the probe runs
// itself (outside the kit).
func freePort(tb testing.TB) int {
	tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}
