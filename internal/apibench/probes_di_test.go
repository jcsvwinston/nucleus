// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package apibench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// The di family asks how an application's parts find each other and in what
// order they come up and go down — the listón is Spring's constructor
// injection and Elixir's supervision trees, scaled down: a module receives
// what it needs typed, can offer what it provides, starts after what it
// depends on, and a failed start tears down what already started. The plan
// names "light dependency injection instead of the service locator"; QADR-0010
// says the locator stays until the major, so what is measured here is whether
// the typed form EXISTS beside it, not whether the old one is gone.

// DI-01: modules receive a typed runtime — services by method, not by key.
func probeTypedRuntime(t *testing.T, _ *env) verdict {
	rt := reflect.TypeOf((*nucleus.Runtime)(nil)).Elem()
	var names []string
	for i := 0; i < rt.NumMethod(); i++ {
		names = append(names, rt.Method(i).Name)
	}
	t.Logf("Runtime: %v", names)
	if rt.NumMethod() >= 10 {
		return present
	}
	return partial
}

// benchQuotes is what the provider module of DI-02 offers: an interface, so
// the probe also measures that a value is found by the type the consumer
// asks for, not by its concrete type.
type benchQuotes interface{ Quote() string }

type fixedQuotes string

func (q fixedQuotes) Quote() string { return string(q) }

// DI-02: a module can PROVIDE a service that another module consumes typed.
//
// Boots an application with two modules: "z-quotes" provides a benchQuotes in
// its OnStart; "a-report" declares it in DependsOn, resolves it typed in its
// own OnStart and serves it from a route. Then the two errors an author
// meets: resolving what nobody provides, and two modules providing one type.
func probeModuleProvidesService(t *testing.T, _ *env) verdict {
	provider := nucleus.Module[struct{}]{
		Name: "z-quotes",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			return nucleus.Provide[benchQuotes](rt, fixedQuotes("typed, not keyed"))
		},
	}.Build()
	// The consumer keeps the resolution's outcome instead of failing its
	// OnStart, so a missing value reads as an answer from the route rather
	// than as a boot failure the kit would stop the probe on.
	var quotes benchQuotes
	var resolveErr error
	consumer := nucleus.Module[struct{}]{
		Name:      "a-report",
		Prefix:    "/a-report",
		DependsOn: []string{"z-quotes"},
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			quotes, resolveErr = nucleus.Resolve[benchQuotes](rt)
			return nil
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/quote", func(c *nucleus.Context) error {
				if resolveErr != nil {
					return c.JSON(http.StatusInternalServerError, map[string]string{"error": resolveErr.Error()})
				}
				return c.JSON(http.StatusOK, map[string]string{"quote": quotes.Quote()})
			})
		},
	}.Build()
	srv := startWith(t, provider, consumer)
	resp, raw := doOn(t, srv, http.MethodGet, "/a-report/quote", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), "typed, not keyed") {
		t.Logf("the consumer's route answered %d %.200s", resp.StatusCode, raw)
		return absent
	}

	// Nobody provides it: the boot error names the module, the type and the
	// fix.
	orphan := nucleus.Module[struct{}]{
		Name: "orphan",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			_, err := nucleus.Resolve[benchQuotes](rt)
			return err
		},
	}.Build()
	err := runToBoot(t, orphan)
	t.Logf("resolve with no provider: %v", err)
	if !errors.Is(err, nucleus.ErrNotProvided) || !strings.Contains(err.Error(), `"orphan"`) || !strings.Contains(err.Error(), "benchQuotes") {
		t.Log("resolving what no module provides does not fail with an error naming the module and the type")
		return partial
	}

	// Two providers of one type: an error naming both.
	twice := func(name string) nucleus.ModuleSpec {
		return nucleus.Module[struct{}]{
			Name: name,
			OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
				return nucleus.Provide[benchQuotes](rt, fixedQuotes(name))
			},
		}.Build()
	}
	err = runToBoot(t, twice("p-one"), twice("p-two"))
	t.Logf("two providers: %v", err)
	if !errors.Is(err, nucleus.ErrAlreadyProvided) || !strings.Contains(err.Error(), `"p-one"`) || !strings.Contains(err.Error(), `"p-two"`) {
		t.Log("two modules providing one type is not an error naming both")
		return partial
	}
	return present
}

// runToBoot runs an application with the given modules until every module
// has started (a service runs only then and stops it) and returns what
// RunContext returned: the boot error, or nil.
func runToBoot(t *testing.T, modules ...nucleus.ModuleSpec) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a := buildWith(t, nil, modules...)
	a.Config.Port = freePort(t)
	a.Services = append(a.Services, nucleus.ServiceRegistration{Name: "stop-when-up", Run: func(context.Context) error {
		cancel()
		return nil
	}})
	return nucleus.RunContext(ctx, a)
}

// benchTenantKey and benchStepKey are the typed request-value keys DI-03
// sets in a middleware and in a handler.
var (
	benchTenantKey = nucleus.NewKey[benchTenant]("tenant")
	benchStepKey   = nucleus.NewKey[int]("step")
)

type benchTenant struct{ ID string }

// DI-03: request-scoped values are typed.
//
// Boots a module whose http middleware stores a struct under a typed key and
// whose route chain stores an int in a first handler and reads both, typed,
// in the last — the two places an author sets a request value.
func probeRequestScopedTyped(t *testing.T, _ *env) verdict {
	m := nucleus.Module[struct{}]{
		Name:   "scoped",
		Prefix: "/scoped",
		Middleware: []nucleus.Middleware{func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t := benchTenant{ID: r.Header.Get("X-Bench-Tenant")}
				next.ServeHTTP(w, r.WithContext(benchTenantKey.WithValue(r.Context(), t)))
			})
		}},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/who",
				func(c *nucleus.Context) error {
					nucleus.SetValue(c, benchStepKey, 2)
					return c.Next()
				},
				func(c *nucleus.Context) error {
					tenant, okT := nucleus.Value(c, benchTenantKey)
					step, okS := nucleus.Value(c, benchStepKey)
					return c.JSON(http.StatusOK, map[string]any{"tenant": tenant.ID, "step": step, "found": okT && okS})
				})
		},
	}.Build()
	srv := startWith(t, m)
	resp, raw := doOn(t, srv, http.MethodGet, "/scoped/who", nil, map[string]string{"X-Bench-Tenant": "acme"})
	t.Logf("%d %.200s", resp.StatusCode, raw)
	var got struct {
		Tenant string `json:"tenant"`
		Step   int    `json:"step"`
		Found  bool   `json:"found"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &got) != nil {
		return absent
	}
	if !got.Found || got.Tenant != "acme" || got.Step != 2 {
		t.Log("a typed key did not carry its value from the middleware or the first handler to the last")
		return partial
	}
	return present
}

type orderRecorder struct {
	mu    sync.Mutex
	order []string
}

func (r *orderRecorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, s)
}

func (r *orderRecorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...)
}

func recordingModule(name string, rec *orderRecorder, startErr error, deps ...string) nucleus.ModuleSpec {
	return recordingModuleStopErr(name, rec, startErr, nil, deps...)
}

func recordingModuleStopErr(name string, rec *orderRecorder, startErr, stopErr error, deps ...string) nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name:      name,
		Prefix:    "/" + name,
		DependsOn: deps,
		OnStart: func(context.Context, nucleus.Runtime, struct{}) error {
			rec.add("start:" + name)
			return startErr
		},
		OnShutdown: func(context.Context, nucleus.Runtime, struct{}) error {
			rec.add("stop:" + name)
			return stopErr
		},
	}.Build()
}

// DI-04: start order follows DECLARED dependencies between modules.
//
// "a-consumer" declares "z-provider", which by name would start last; "m-free"
// declares nothing. The probe records the start order, stops the application
// and records the stop order, then boots a cycle.
func probeStartOrderDeclared(t *testing.T, _ *env) verdict {
	rec := &orderRecorder{}
	srv := startWith(t,
		recordingModule("a-consumer", rec, nil, "z-provider"),
		recordingModule("m-free", rec, nil),
		recordingModule("z-provider", rec, nil))
	srv.Stop()
	order := rec.list()
	t.Logf("order: %v", order)
	want := []string{"start:m-free", "start:z-provider", "start:a-consumer", "stop:a-consumer", "stop:z-provider", "stop:m-free"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		if len(order) > 0 && order[0] == "start:a-consumer" {
			t.Log("the modules started in name order: the declaration was not honoured")
			return absent
		}
		t.Logf("want %v", want)
		return partial
	}

	// A cycle is a boot error naming the modules on it.
	cyc := &orderRecorder{}
	err := runToBoot(t, recordingModule("c-one", cyc, nil, "c-two"), recordingModule("c-two", cyc, nil, "c-one"))
	t.Logf("cycle: %v (ran: %v)", err, cyc.list())
	if !errors.Is(err, nucleus.ErrModuleDependency) || !strings.Contains(err.Error(), `"c-one" -> "c-two" -> "c-one"`) || len(cyc.list()) != 0 {
		t.Log("a dependency cycle does not stop boot with an error naming the cycle")
		return partial
	}
	return present
}

// DI-05: when a module's OnStart fails, the modules already started are shut
// down (NU-44).
//
// Three modules start in name order; the third refuses. The first two must
// be shut down in reverse order, the third must not be, and the error must
// carry both the start failure and the second module's shutdown failure.
func probeShutdownOnStartFailure(t *testing.T, _ *env) verdict {
	rec := &orderRecorder{}
	refuse := fmt.Errorf("bench: refusing to start")
	flush := fmt.Errorf("bench: flush failed")
	err := runToBoot(t,
		recordingModule("a-first", rec, nil),
		recordingModuleStopErr("b-second", rec, nil, flush),
		recordingModule("c-fails", rec, refuse))
	order := rec.list()
	t.Logf("run: %v; order: %v", err, order)
	if err == nil {
		t.Log("the application started although a module refused to")
		return absent
	}
	stopped := map[string]int{}
	for i, s := range order {
		stopped[s] = i + 1
	}
	if stopped["stop:a-first"] == 0 && stopped["stop:b-second"] == 0 {
		t.Log("a-first and b-second started, c-fails refused, and neither OnShutdown ran: whatever they opened stays open")
		return absent
	}
	want := []string{"start:a-first", "start:b-second", "start:c-fails", "stop:b-second", "stop:a-first"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Logf("want %v", want)
		return partial
	}
	if !errors.Is(err, refuse) || !errors.Is(err, flush) {
		t.Log("the error does not carry both the start failure and the shutdown failure")
		return partial
	}
	return present
}

// DI-06: application-level hooks run around the modules' hooks, in order.
func probeAppHooksAroundModules(t *testing.T, _ *env) verdict {
	rec := &orderRecorder{}
	a := buildWith(t, nil, recordingModule("m-one", rec, nil))
	a.Lifecycle = nucleus.LifecycleHooks{
		OnStart:    func(context.Context) error { rec.add("start:app"); return nil },
		OnShutdown: func(context.Context) error { rec.add("stop:app"); return nil },
	}
	srv := nucleustest.StartApp(t, a)
	srv.Stop()
	order := rec.list()
	t.Logf("order: %v", order)
	if len(order) < 4 || order[0] != "start:app" || order[1] != "start:m-one" {
		return partial
	}
	return present
}

// DI-07: constructors report bad input as an error, never a panic (NU-41).
//
// Two halves. The source: every exported top-level function under pkg/ that
// calls panic directly either is a Must* wrapper beside an error-returning
// function of the same name without the prefix (the regexp.MustCompile
// convention, for init-time registration), or is deprecated by a paragraph
// that names an exported function of its package whose last result is an
// error. The behaviour: the error forms, called with the input the
// deprecated forms panic on, return an error and do not panic.
func probeConstructorsDontPanic(t *testing.T, _ *env) verdict {
	offenders := panickingExportedFuncs(t, filepath.Join(repoRoot(t), "pkg"))
	for _, o := range offenders {
		t.Logf("panics with no error-returning form declared: %s", o)
	}

	type errorForm struct {
		name string
		call func() error
	}
	forms := []errorForm{
		{"auth.NewJWTManagerFromSecret with a short secret", func() error {
			_, err := auth.NewJWTManagerFromSecret("short", time.Hour)
			return err
		}},
		{"db.NewMigratorFromConfig with a module name holding '/'", func() error {
			_, err := db.NewMigratorFromConfig(db.MigratorConfig{FS: fstest.MapFS{}, Module: "a/b"}, nil)
			return err
		}},
		{"db.NewMigratorFromConfig with no source", func() error {
			_, err := db.NewMigratorFromConfig(db.MigratorConfig{Module: "shop"}, nil)
			return err
		}},
		{"router.NewCSRFMiddleware with an XSRF cookie and no key", func() error {
			_, err := router.NewCSRFMiddleware(router.CSRFOptions{EnableXSRFCookie: true})
			return err
		}},
	}
	failing := 0
	for _, f := range forms {
		err, panicked := callRecovering(f.call)
		switch {
		case panicked != nil:
			t.Logf("%s panics: %v", f.name, panicked)
			failing++
		case err == nil:
			t.Logf("%s returns no error", f.name)
			failing++
		default:
			t.Logf("%s: %v", f.name, err)
		}
	}
	switch {
	case len(offenders) == 0 && failing == 0:
		return present
	case failing == len(forms):
		return absent
	default:
		return partial
	}
}

func callRecovering(fn func() error) (err error, panicked any) {
	defer func() { panicked = recover() }()
	return fn(), nil
}

// panickingExportedFuncs lists the exported top-level functions under root
// (test files and testdata excluded) whose body calls panic directly and
// that declare no error-returning form — see probeConstructorsDontPanic for
// the two accepted declarations.
func panickingExportedFuncs(t *testing.T, root string) []string {
	t.Helper()
	type pkgFuncs struct {
		errorFuncs map[string]bool          // exported funcs whose last result is error
		decls      map[string]*ast.FuncDecl // exported funcs that call panic
		files      map[string]string
	}
	pkgs := map[string]*pkgFuncs{}
	fset := token.NewFileSet()
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return nil
		}
		dir := filepath.Dir(path)
		p := pkgs[dir]
		if p == nil {
			p = &pkgFuncs{errorFuncs: map[string]bool{}, decls: map[string]*ast.FuncDecl{}, files: map[string]string{}}
			pkgs[dir] = p
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || !fd.Name.IsExported() {
				continue
			}
			if res := fd.Type.Results; res != nil && len(res.List) > 0 {
				if id, ok := res.List[len(res.List)-1].Type.(*ast.Ident); ok && id.Name == "error" {
					p.errorFuncs[fd.Name.Name] = true
				}
			}
			if fd.Body != nil && callsPanic(fd.Body) {
				p.decls[fd.Name.Name] = fd
				rel, _ := filepath.Rel(root, path)
				p.files[fd.Name.Name] = rel
			}
		}
		return nil
	})
	var out []string
	named := regexp.MustCompile(`\b[A-Z][A-Za-z0-9]*\b`)
	for _, p := range pkgs {
		for name, fd := range p.decls {
			if strings.HasPrefix(name, "Must") && p.errorFuncs[strings.TrimPrefix(name, "Must")] {
				continue
			}
			doc := ""
			if fd.Doc != nil {
				doc = fd.Doc.Text()
			}
			ok := false
			if i := strings.Index(doc, "Deprecated:"); i >= 0 {
				for _, cand := range named.FindAllString(doc[i+len("Deprecated:"):], -1) {
					if cand != name && p.errorFuncs[cand] {
						ok = true
						break
					}
				}
			}
			if !ok {
				out = append(out, p.files[name]+": "+name)
			}
		}
	}
	sort.Strings(out)
	return out
}

func callsPanic(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false // a panic inside a returned closure is not the constructor's
		}
		if ce, ok := n.(*ast.CallExpr); ok {
			if id, ok := ce.Fun.(*ast.Ident); ok && id.Name == "panic" {
				found = true
			}
		}
		return true
	})
	return found
}

type benchCfg struct {
	Greeting string `koanf:"greeting"`
}

func greetingModule(defaults benchCfg) nucleus.ModuleSpec {
	return nucleus.Module[benchCfg]{
		Name:   "benchcfg",
		Prefix: "/benchcfg",
		Config: defaults,
		Routes: func(r nucleus.Router, cfg benchCfg) {
			r.Get("/greeting", func(c *nucleus.Context) error {
				return c.JSON(http.StatusOK, map[string]string{"greeting": cfg.Greeting})
			})
		},
	}.Build()
}

// DI-08: a module declares a typed configuration and receives it typed.
func probeTypedModuleConfig(t *testing.T, _ *env) verdict {
	srv := startWith(t, greetingModule(benchCfg{Greeting: "default"}))
	resp, raw := doOn(t, srv, http.MethodGet, "/benchcfg/greeting", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"default"`) {
		t.Logf("%d %.200s", resp.StatusCode, raw)
		return absent
	}
	return present
}

// DI-09: that configuration is BOUND from the application's config file
// under modules.<name>.
func probeModuleConfigBound(t *testing.T, _ *env) verdict {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yaml")
	yaml := fmt.Sprintf(`env: development
host: 127.0.0.1
port: %d
jwt_secret: %q
databases:
  default:
    url: %q
modules:
  benchcfg:
    greeting: hola
`, freePort(t), strings.Repeat("apibench-probe-secret", 2), "sqlite://"+filepath.Join(dir, "cfg.db"))
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := nucleus.New().FromConfigFile(cfgPath).WithOpenAuthz().Mount(greetingModule(benchCfg{Greeting: "default"})).Build()
	if err != nil {
		t.Logf("build from file: %v", err)
		return absent
	}
	srv := nucleustest.StartApp(t, a)
	resp, raw := doOn(t, srv, http.MethodGet, "/benchcfg/greeting", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Logf("%d %.200s", resp.StatusCode, raw)
		return absent
	}
	if !strings.Contains(string(raw), `"hola"`) {
		t.Logf("the module saw %.200s, not the file's value", raw)
		return partial
	}
	return present
}
