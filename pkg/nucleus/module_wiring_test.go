// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Module wiring (A10 S9): the start order follows DependsOn, shutdown runs it
// in reverse, a failed OnStart shuts down what already started (NU-44), and
// a module provides a value another resolves typed.
package nucleus

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func depModule(name string, deps ...string) ModuleSpec {
	return Module[struct{}]{Name: name, DependsOn: deps}.Build()
}

func moduleMap(specs ...ModuleSpec) map[string]ModuleSpec {
	out := map[string]ModuleSpec{}
	for _, s := range specs {
		out[s.Name()] = s
	}
	return out
}

func TestModuleStartOrder(t *testing.T) {
	cases := []struct {
		name    string
		modules map[string]ModuleSpec
		want    []string
	}{
		{"no declarations keep name order",
			moduleMap(depModule("c"), depModule("a"), depModule("b")),
			[]string{"a", "b", "c"}},
		{"a dependency starts first",
			moduleMap(depModule("a-consumer", "z-provider"), depModule("z-provider")),
			[]string{"z-provider", "a-consumer"}},
		{"ties broken by name once dependencies are met",
			moduleMap(depModule("d", "a"), depModule("c", "a"), depModule("b"), depModule("a", "e"), depModule("e")),
			[]string{"b", "e", "a", "c", "d"}},
		{"duplicate and blank entries are ignored",
			moduleMap(depModule("a", "b", " ", "b"), depModule("b")),
			[]string{"b", "a"}},
		{"a chain",
			moduleMap(depModule("a", "b"), depModule("b", "c"), depModule("c")),
			[]string{"c", "b", "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := moduleStartOrder(tc.modules)
			if err != nil {
				t.Fatalf("moduleStartOrder: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("order = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestModuleStartOrder_UnknownDependencyNamesBoth(t *testing.T) {
	_, err := moduleStartOrder(moduleMap(depModule("billing", "ledgr"), depModule("ledger")))
	if !errors.Is(err, ErrModuleDependency) {
		t.Fatalf("err = %v, want ErrModuleDependency", err)
	}
	for _, want := range []string{`"billing"`, `"ledgr"`, "not mounted", "ledger"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestModuleStartOrder_CycleNamesTheModules(t *testing.T) {
	cases := []struct {
		name    string
		modules map[string]ModuleSpec
		want    string
	}{
		{"two", moduleMap(depModule("a", "b"), depModule("b", "a"), depModule("c")), `"a" -> "b" -> "a"`},
		{"three", moduleMap(depModule("a", "b"), depModule("b", "c"), depModule("c", "a")), `"a" -> "b" -> "c" -> "a"`},
		{"self", moduleMap(depModule("a", "a")), `"a" -> "a"`},
		{"behind a cycle", moduleMap(depModule("a", "b"), depModule("b", "c"), depModule("c", "b")), `"b" -> "c" -> "b"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := moduleStartOrder(tc.modules)
			if !errors.Is(err, ErrModuleDependency) {
				t.Fatalf("err = %v, want ErrModuleDependency", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name the cycle %s", err, tc.want)
			}
		})
	}
}

// recorder keeps the order of lifecycle events across modules.
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(e string) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func wiringConfig(t *testing.T) app.Config {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = freeLocalPort(t)
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "wiring.db")},
	}
	return cfg
}

// runUntilStarted runs the application and stops it as soon as every module
// has started (a service runs only after that), returning Run's error.
func runUntilStarted(t *testing.T, a App) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a.Services = append(a.Services, ServiceRegistration{Name: "stop-when-up", Run: func(context.Context) error {
		cancel()
		return nil
	}})
	return RunContext(ctx, a)
}

func lifecycleModule(name string, rec *recorder, startErr, stopErr error, deps ...string) ModuleSpec {
	return Module[struct{}]{
		Name:      name,
		DependsOn: deps,
		OnStart: func(context.Context, Runtime, struct{}) error {
			rec.add("start:" + name)
			return startErr
		},
		OnShutdown: func(context.Context, Runtime, struct{}) error {
			rec.add("stop:" + name)
			return stopErr
		},
	}.Build()
}

func TestRun_StartsInDependencyOrderAndStopsInReverse(t *testing.T) {
	rec := &recorder{}
	err := runUntilStarted(t, App{
		Config:  wiringConfig(t),
		Options: []app.Option{app.WithoutDefaults()},
		Modules: moduleMap(
			lifecycleModule("a-api", rec, nil, nil, "m-cache"),
			lifecycleModule("m-cache", rec, nil, nil, "z-store"),
			lifecycleModule("z-store", rec, nil, nil),
			lifecycleModule("b-free", rec, nil, nil),
		),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := []string{
		"start:b-free", "start:z-store", "start:m-cache", "start:a-api",
		"stop:a-api", "stop:m-cache", "stop:z-store", "stop:b-free",
	}
	if got := rec.list(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v\nwant     %v", got, want)
	}
}

func TestRun_UnknownDependencyFailsBootBeforeAnyModuleStarts(t *testing.T) {
	rec := &recorder{}
	err := runUntilStarted(t, App{
		Config:  wiringConfig(t),
		Options: []app.Option{app.WithoutDefaults()},
		Modules: moduleMap(lifecycleModule("a", rec, nil, nil, "missing")),
	})
	if !errors.Is(err, ErrModuleDependency) {
		t.Fatalf("err = %v, want ErrModuleDependency", err)
	}
	if got := rec.list(); len(got) != 0 {
		t.Fatalf("modules ran although boot failed: %v", got)
	}
}

// NU-44: a failed OnStart shuts down, in reverse order, every module that
// already started; the failing module gets no OnShutdown; the app-level
// Lifecycle.OnShutdown runs last; the errors are joined.
func TestRun_FailedOnStartShutsDownWhatStarted(t *testing.T) {
	rec := &recorder{}
	refuse := errors.New("refusing to start")
	stopFails := errors.New("flush failed")
	err := runUntilStarted(t, App{
		Config:  wiringConfig(t),
		Options: []app.Option{app.WithoutDefaults()},
		Lifecycle: LifecycleHooks{
			OnStart:    func(context.Context) error { rec.add("start:app"); return nil },
			OnShutdown: func(context.Context) error { rec.add("stop:app"); return nil },
		},
		Modules: moduleMap(
			lifecycleModule("a-first", rec, nil, nil),
			lifecycleModule("b-second", rec, nil, stopFails),
			lifecycleModule("c-fails", rec, refuse, nil),
			lifecycleModule("d-never", rec, nil, nil),
		),
	})
	if !errors.Is(err, refuse) {
		t.Fatalf("err = %v, want it to wrap the OnStart error", err)
	}
	if !errors.Is(err, stopFails) {
		t.Fatalf("err = %v, want it to join the OnShutdown error", err)
	}
	for _, want := range []string{`module "c-fails" OnStart`, `module "b-second" OnShutdown`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %s", err, want)
		}
	}
	want := []string{
		"start:app", "start:a-first", "start:b-second", "start:c-fails",
		"stop:b-second", "stop:a-first", "stop:app",
	}
	if got := rec.list(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v\nwant     %v", got, want)
	}
}

// A failed Lifecycle.OnStart runs no module and no Lifecycle.OnShutdown, and
// still returns its own error unchanged.
func TestRun_FailedLifecycleOnStartRunsNoShutdownHook(t *testing.T) {
	rec := &recorder{}
	refuse := errors.New("not ready")
	err := runUntilStarted(t, App{
		Config:  wiringConfig(t),
		Options: []app.Option{app.WithoutDefaults()},
		Lifecycle: LifecycleHooks{
			OnStart:    func(context.Context) error { return refuse },
			OnShutdown: func(context.Context) error { rec.add("stop:app"); return nil },
		},
		Modules: moduleMap(lifecycleModule("a", rec, nil, nil)),
	})
	if !errors.Is(err, refuse) {
		t.Fatalf("err = %v", err)
	}
	if got := rec.list(); len(got) != 0 {
		t.Fatalf("hooks ran: %v", got)
	}
}

type clock interface{ Now() time.Time }

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type greeting string

func TestProvideResolve_AcrossModules(t *testing.T) {
	at := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	var seen time.Time
	var seenGreeting greeting
	provider := Module[struct{}]{
		Name: "z-clock",
		OnStart: func(_ context.Context, rt Runtime, _ struct{}) error {
			if err := Provide[clock](rt, fixedClock{at: at}); err != nil {
				return err
			}
			return Provide(rt, greeting("hola"))
		},
	}.Build()
	consumer := Module[struct{}]{
		Name:      "a-report",
		DependsOn: []string{"z-clock"},
		OnStart: func(_ context.Context, rt Runtime, _ struct{}) error {
			c, err := Resolve[clock](rt)
			if err != nil {
				return err
			}
			seen = c.Now()
			seenGreeting, err = Resolve[greeting](rt)
			return err
		},
	}.Build()
	if err := runUntilStarted(t, App{
		Config:  wiringConfig(t),
		Options: []app.Option{app.WithoutDefaults()},
		Modules: moduleMap(provider, consumer),
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !seen.Equal(at) || seenGreeting != "hola" {
		t.Fatalf("consumer saw %v / %q", seen, seenGreeting)
	}
}

func TestResolve_WithoutDeclaredDependencyNamesTheFix(t *testing.T) {
	// "a-report" starts before "z-clock" by name and declares nothing.
	provider := Module[struct{}]{
		Name: "z-clock",
		OnStart: func(_ context.Context, rt Runtime, _ struct{}) error {
			return Provide[clock](rt, fixedClock{})
		},
	}.Build()
	consumer := Module[struct{}]{
		Name: "a-report",
		OnStart: func(_ context.Context, rt Runtime, _ struct{}) error {
			_, err := Resolve[clock](rt)
			return err
		},
	}.Build()
	err := runUntilStarted(t, App{
		Config:  wiringConfig(t),
		Options: []app.Option{app.WithoutDefaults()},
		Modules: moduleMap(provider, consumer),
	})
	if !errors.Is(err, ErrNotProvided) {
		t.Fatalf("err = %v, want ErrNotProvided", err)
	}
	for _, want := range []string{`module "a-report"`, "nucleus.clock", "DependsOn"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %s", err, want)
		}
	}
}

func TestProvide_TwiceIsAnErrorNamingBothModules(t *testing.T) {
	mk := func(name string) ModuleSpec {
		return Module[struct{}]{
			Name: name,
			OnStart: func(_ context.Context, rt Runtime, _ struct{}) error {
				return Provide(rt, greeting(name))
			},
		}.Build()
	}
	err := runUntilStarted(t, App{
		Config:  wiringConfig(t),
		Options: []app.Option{app.WithoutDefaults()},
		Modules: moduleMap(mk("a"), mk("b")),
	})
	if !errors.Is(err, ErrAlreadyProvided) {
		t.Fatalf("err = %v, want ErrAlreadyProvided", err)
	}
	if !strings.Contains(err.Error(), `module "b" provides nucleus.greeting, which module "a" already provides`) {
		t.Fatalf("error %q does not name both modules", err)
	}
}

func newTestRegistryRuntime(module string, reg *serviceRegistry) runtime {
	rt := newRuntime(nil, "")
	rt.moduleName = module
	rt.services = reg
	return rt
}

func TestResolve_ProvidedUnderTheConcreteTypeSaysSo(t *testing.T) {
	reg := newServiceRegistry(nil)
	if err := Provide(newTestRegistryRuntime("z-clock", reg), fixedClock{}); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve[clock](newTestRegistryRuntime("a-report", reg))
	if !errors.Is(err, ErrNotProvided) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "nucleus.Provide[nucleus.clock]") {
		t.Fatalf("error %q does not say how to provide it", err)
	}
}

func TestProvide_RefusesNilAfterStartAndForeignRuntimes(t *testing.T) {
	reg := newServiceRegistry(nil)
	rt := newTestRegistryRuntime("m", reg)
	if err := Provide[clock](rt, nil); err == nil || !strings.Contains(err.Error(), "nil value") {
		t.Fatalf("nil interface: %v", err)
	}
	var p *fixedClock
	if err := Provide(rt, p); err == nil {
		t.Fatal("nil pointer accepted")
	}
	reg.seal()
	if err := Provide(rt, greeting("late")); err == nil || !strings.Contains(err.Error(), "after every module has started") {
		t.Fatalf("after seal: %v", err)
	}
	if _, err := Resolve[greeting](rt); !errors.Is(err, ErrNotProvided) || !strings.Contains(err.Error(), "every module has started") {
		t.Fatalf("resolve after seal: %v", err)
	}
	if err := Provide(newRuntime(nil, ""), greeting("x")); err == nil || !strings.Contains(err.Error(), "not handed out by a running application") {
		t.Fatalf("bare runtime: %v", err)
	}
	if _, err := Resolve[greeting](nil); err == nil {
		t.Fatal("nil runtime accepted")
	}
}

func TestResolve_UndeclaredDependencyIsDetected(t *testing.T) {
	reg := newServiceRegistry(map[string][]string{"a": {"b"}, "b": {"c"}})
	if err := Provide(newTestRegistryRuntime("c", reg), greeting("x")); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	transitive := reg.dependsOnLocked("a", "c")
	direct := reg.dependsOnLocked("b", "c")
	none := reg.dependsOnLocked("c", "a")
	reg.mu.Unlock()
	if !transitive || !direct || none {
		t.Fatalf("dependsOn: a->c %v, b->c %v, c->a %v", transitive, direct, none)
	}
	// Resolving from a module that declares nothing logs once and still
	// returns the value.
	if v, err := Resolve[greeting](newTestRegistryRuntime("d", reg)); err != nil || v != "x" {
		t.Fatalf("resolve: %v %v", v, err)
	}
	if !reg.warned["d\x00nucleus.greeting"] {
		t.Fatal("no warning recorded for an undeclared dependency")
	}
	if _, err := Resolve[greeting](newTestRegistryRuntime("a", reg)); err != nil {
		t.Fatal(err)
	}
	if reg.warned["a\x00nucleus.greeting"] {
		t.Fatal("a declares c through b, and was warned anyway")
	}
}
