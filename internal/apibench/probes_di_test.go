// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package apibench

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
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

// DI-02: a module can PROVIDE a service that another module consumes typed.
func probeModuleProvidesService(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`func (\([^)]*\) )?(Provide|Resolve|Inject|MustResolve)\b|Provides\(\)`)
	if files := sourceMatches(t, "pkg/nucleus", re); len(files) > 0 {
		t.Logf("provision API in %v", files)
		return present
	}
	t.Log("ServiceRegistration is a background runner (Run, Health); nothing lets module A hand module B a typed value — B reaches for a package-level variable or a string key")
	return absent
}

// DI-03: request-scoped values are typed.
func probeRequestScopedTyped(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`func (Get|Value|MustGet|Lookup)\[`)
	if files := sourceMatches(t, "pkg/nucleus", re); len(files) > 0 {
		t.Logf("typed request values in %v", files)
		return present
	}
	if _, ok := anyMethod(contextMethods(), "Get", "Set"); ok {
		t.Log("Context.Set(key string, v interface{}) / Get(key) interface{}: a keyed store with a type assertion at every read")
		return partial
	}
	return absent
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

func recordingModule(name string, rec *orderRecorder, startErr error) nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name:   name,
		Prefix: "/" + name,
		OnStart: func(context.Context, nucleus.Runtime, struct{}) error {
			rec.add("start:" + name)
			return startErr
		},
		OnShutdown: func(context.Context, nucleus.Runtime, struct{}) error {
			rec.add("stop:" + name)
			return nil
		},
	}.Build()
}

// DI-04: start order follows DECLARED dependencies between modules.
func probeStartOrderDeclared(t *testing.T, _ *env) verdict {
	rec := &orderRecorder{}
	// "a-consumer" should come up AFTER "z-provider"; without a declaration,
	// the only order available is the name.
	srv := startWith(t, recordingModule("a-consumer", rec, nil), recordingModule("z-provider", rec, nil))
	_ = srv
	order := rec.list()
	t.Logf("start order: %v", order)
	decl := sourceMatches(t, "pkg/nucleus", regexp.MustCompile(`(?m)^\s*(DependsOn|After|Before|RequiresModules)\s`))
	if len(decl) > 0 {
		t.Logf("dependency declaration in %v", decl)
		return present
	}
	t.Log("Module.Requires names DATABASE aliases, not modules; modules start in name order (sortedModuleSpecs), so a module that needs another's OnStart to have run renames itself or hopes")
	return absent
}

// DI-05: when a module's OnStart fails, the modules already started are shut
// down (NU-44).
func probeShutdownOnStartFailure(t *testing.T, _ *env) verdict {
	rec := &orderRecorder{}
	a := buildWith(t, nil,
		recordingModule("a-first", rec, nil),
		recordingModule("b-fails", rec, fmt.Errorf("bench: refusing to start")))
	a.Config.Port = freePort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := nucleus.RunContext(ctx, a)
	order := rec.list()
	t.Logf("run: %v; order: %v", err, order)
	if err == nil {
		t.Log("the application started although a module refused to")
		return absent
	}
	for _, s := range order {
		if s == "stop:a-first" {
			return present
		}
	}
	t.Log("a-first started, b-fails refused, and a-first's OnShutdown never ran: whatever a-first opened stays open")
	return absent
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
func probeConstructorsDontPanic(t *testing.T, _ *env) verdict {
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				t.Logf("auth.NewJWTManager(\"\", 0) panics: %v", r)
			}
		}()
		_ = auth.NewJWTManager("", 0)
	}()
	if _, err := auth.NewJWTManagerFromKeys(nil, "", 0); err == nil {
		t.Log("NewJWTManagerFromKeys accepts no keys without an error")
	}
	if panicked {
		t.Log("two constructors for one type, one panics and the other returns an error: the style guide NU-41 asks for is not written")
		return absent
	}
	return present
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
