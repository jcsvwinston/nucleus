package nucleus

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// TestThreeSurfaceEquivalence verifies that the three coexisting
// surfaces described in ADR-010 §1 — fluent builder, direct struct,
// and bootstrap pattern — produce equal `App{}` values when
// given equivalent inputs. The normalisation rules of ADR-010 §79-§85
// apply:
//
//   - Modules map: sorted by key before comparison.
//   - Function fields: compared by reference identity (same function
//     literal across surfaces — the test reuses one literal per
//     callback so identity is preserved).
//   - time.Time fields zeroed (none today; future-proofs the rule).
func TestThreeSurfaceEquivalence(t *testing.T) {
	t.Parallel()

	// Shared inputs across the three surfaces. Function literals are
	// declared once and reused so reference-identity comparison passes.
	startHook := func(ctx context.Context) error { return nil }
	shutdownHook := func(ctx context.Context) error { return nil }
	serviceRun := func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}

	routesFn := func(r Router, _ struct{}) {
		r.Get("/ping", func(c *Context) error { return c.String(200, "pong") })
	}

	articles := Module[struct{}]{
		Name:      "articles",
		Prefix:    "/articles",
		DefaultDB: "default",
		Routes:    routesFn,
	}.Build()

	desiredPort := 8765

	// Surface 1: fluent builder
	fluentApp, err := New().
		Use( /* no middleware in this test */ ).
		Mount(articles).
		Build()
	if err != nil {
		t.Fatalf("fluent Build: %v", err)
	}
	fluentApp.Config.Port = desiredPort
	fluentApp.Lifecycle = LifecycleHooks{
		OnStart: startHook, OnShutdown: shutdownHook,
	}
	fluentApp.Services = []ServiceRegistration{
		{Name: "worker", Run: serviceRun},
	}

	// Surface 2: direct struct
	directApp := App{
		Config: app.DefaultConfig(),
		Modules: map[string]ModuleSpec{
			"articles": articles,
		},
		Lifecycle: LifecycleHooks{
			OnStart: startHook, OnShutdown: shutdownHook,
		},
		Services: []ServiceRegistration{
			{Name: "worker", Run: serviceRun},
		},
	}
	directApp.Config.Port = desiredPort

	// Surface 3: bootstrap pattern (user-space convention)
	bootstrapApp := newBootstrapApp(articles, desiredPort, startHook, shutdownHook, serviceRun)

	// Compare. Equality is field-by-field after applying the
	// normalisation rules of ADR-010 §79-§85.
	assertAppsEqual(t, "fluent vs direct", fluentApp, directApp)
	assertAppsEqual(t, "fluent vs bootstrap", fluentApp, bootstrapApp)
	assertAppsEqual(t, "direct vs bootstrap", directApp, bootstrapApp)
}

// newBootstrapApp models the user-space bootstrap convention: a
// project-owned function (typically `internal/bootstrap/bootstrap.go`
// in a host application) that returns a fully populated `App`.
// `pkg/nucleus` does not ship a bootstrap sub-package; this function
// lives in the test as the reference implementation of the pattern.
func newBootstrapApp(
	articles ModuleSpec,
	port int,
	onStart func(context.Context) error,
	onShutdown func(context.Context) error,
	serviceRun func(context.Context) error,
) App {
	a := App{
		Config: app.DefaultConfig(),
		Modules: map[string]ModuleSpec{
			articles.Name(): articles,
		},
		Lifecycle: LifecycleHooks{
			OnStart: onStart, OnShutdown: onShutdown,
		},
		Services: []ServiceRegistration{
			{Name: "worker", Run: serviceRun},
		},
	}
	a.Config.Port = port
	return a
}

func assertAppsEqual(t *testing.T, label string, a, b App) {
	t.Helper()

	if a.Config.Port != b.Config.Port {
		t.Errorf("%s: Port differs: %d vs %d", label, a.Config.Port, b.Config.Port)
	}
	if a.Config.Host != b.Config.Host {
		t.Errorf("%s: Host differs: %q vs %q", label, a.Config.Host, b.Config.Host)
	}

	if got, want := sortedNames(a.Modules), sortedNames(b.Modules); !reflect.DeepEqual(got, want) {
		t.Errorf("%s: module names differ: %v vs %v", label, got, want)
	}

	if len(a.Modules) != len(b.Modules) {
		t.Errorf("%s: module count differs: %d vs %d", label, len(a.Modules), len(b.Modules))
	}
	for name, mA := range a.Modules {
		mB, ok := b.Modules[name]
		if !ok {
			t.Errorf("%s: module %q missing on right side", label, name)
			continue
		}
		if mA.Name() != mB.Name() {
			t.Errorf("%s: module %q Name() differs: %q vs %q", label, name, mA.Name(), mB.Name())
		}
		if mA.Prefix() != mB.Prefix() {
			t.Errorf("%s: module %q Prefix() differs: %q vs %q", label, name, mA.Prefix(), mB.Prefix())
		}
		if mA.DefaultDB() != mB.DefaultDB() {
			t.Errorf("%s: module %q DefaultDB() differs: %q vs %q", label, name, mA.DefaultDB(), mB.DefaultDB())
		}
		// Function-field reference-identity per ADR-010 §84: when the
		// three surfaces are given the same `Module[C]` value, the
		// `Routes` callback captured inside the type-erased
		// `ModuleSpec` must point at the same function literal across
		// surfaces. The cast below relies on the fact that the test
		// constructs `articles` via `Module[struct{}]{...}.Build()`
		// once and reuses the resulting spec; if any surface
		// substituted a different wrapping, this assertion catches it.
		specA, okA := mA.(moduleSpec[struct{}])
		specB, okB := mB.(moduleSpec[struct{}])
		if !okA || !okB {
			t.Errorf("%s: module %q underlying type is not moduleSpec[struct{}] (okA=%v, okB=%v)", label, name, okA, okB)
			continue
		}
		if !sameFunc(specA.m.Routes, specB.m.Routes) {
			t.Errorf("%s: module %q Routes function reference identity differs across surfaces", label, name)
		}
	}

	if len(a.Services) != len(b.Services) {
		t.Errorf("%s: service count differs: %d vs %d", label, len(a.Services), len(b.Services))
	} else {
		for i := range a.Services {
			if a.Services[i].Name != b.Services[i].Name {
				t.Errorf("%s: service[%d].Name differs: %q vs %q", label, i, a.Services[i].Name, b.Services[i].Name)
			}
			if !sameFunc(a.Services[i].Run, b.Services[i].Run) {
				t.Errorf("%s: service[%d].Run reference identity differs", label, i)
			}
		}
	}

	if !sameFunc(a.Lifecycle.OnStart, b.Lifecycle.OnStart) {
		t.Errorf("%s: Lifecycle.OnStart reference identity differs", label)
	}
	if !sameFunc(a.Lifecycle.OnShutdown, b.Lifecycle.OnShutdown) {
		t.Errorf("%s: Lifecycle.OnShutdown reference identity differs", label)
	}

	// App.OpenAPI (ADR-010 Phase 4, Slice 2) must agree across surfaces. The
	// spec is immutable post-construction and cloneApp shares the pointer, so
	// pointer identity is the right comparison; when both are nil (the common
	// case — no OpenAPI configured) they are trivially equal.
	if a.OpenAPI != b.OpenAPI {
		t.Errorf("%s: App.OpenAPI differs (%v vs %v)", label, a.OpenAPI, b.OpenAPI)
	}
}

func sortedNames(modules map[string]ModuleSpec) []string {
	out := make([]string, 0, len(modules))
	for n := range modules {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// sameFunc compares two function values by reference identity. Both
// values are passed through reflect.Value.Pointer to expose the
// underlying function pointer; two `nil` functions are considered
// equal.
func sameFunc(a, b any) bool {
	va := reflect.ValueOf(a)
	vb := reflect.ValueOf(b)
	if !va.IsValid() && !vb.IsValid() {
		return true
	}
	if va.Kind() == reflect.Func && vb.Kind() == reflect.Func {
		return va.Pointer() == vb.Pointer()
	}
	return false
}

// TestFromConfigFile_NoPaths verifies the empty-paths guard surfaces a
// clean error rather than a panic or a silent no-op.
func TestFromConfigFile_NoPaths(t *testing.T) {
	t.Parallel()

	b := New().FromConfigFile()
	if b.Err() == nil {
		t.Fatal("expected FromConfigFile() with no paths to record an error")
	}
}

// TestFromConfigFile_MultiPathIsPhase2b verifies that passing multiple
// paths today fails loud, deferring to Phase 2b for the merge engine.
func TestFromConfigFile_MultiPathIsPhase2b(t *testing.T) {
	t.Parallel()

	b := New().FromConfigFile("a.yaml", "b.yaml")
	if b.Err() == nil {
		t.Fatal("expected multi-path FromConfigFile to fail in Phase 2a")
	}
}

// TestMount_DuplicateName ensures the builder records an error when
// two modules are mounted under the same name.
func TestMount_DuplicateName(t *testing.T) {
	t.Parallel()

	mod := Module[struct{}]{Name: "dup", Prefix: "/x"}.Build()
	b := New().Mount(mod).Mount(mod)
	if b.Err() == nil {
		t.Fatal("expected duplicate module name to surface an error")
	}
}

// TestMount_EmptyName ensures a module with an empty Name field is
// rejected at Mount time rather than at Start time.
func TestMount_EmptyName(t *testing.T) {
	t.Parallel()

	b := New().Mount(Module[struct{}]{Prefix: "/x"}.Build())
	if b.Err() == nil {
		t.Fatal("expected empty module name to surface an error")
	}
}

// TestMethods_HasSemantics covers the small surface around MethodSet
// to guard against accidental regressions when Phase 2 extends the
// Router with additional verbs.
func TestMethods_HasSemantics(t *testing.T) {
	t.Parallel()

	ms := Methods(Index, Show)
	if !ms.Has(Index) {
		t.Error("Index not present in {Index, Show}")
	}
	if !ms.Has(Show) {
		t.Error("Show not present in {Index, Show}")
	}
	if ms.Has(Create) {
		t.Error("Create unexpectedly present in {Index, Show}")
	}
}
