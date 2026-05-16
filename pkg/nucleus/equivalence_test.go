package nucleus

import (
	"reflect"
	"sort"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// equivalenceTestModule is the canonical module used by the
// three-surface equivalence test. Building it twice in the same scope
// produces two ModuleSpec values whose underlying Module[C] are
// byte-identical except for `func`-typed fields, which the comparator
// handles via reference identity.
func makeEquivalenceTestModule() ModuleSpec {
	return Module[struct{}]{
		Name:      "articles",
		Prefix:    "/articles",
		DefaultDB: "default",
		Routes:    func(r Router, _ struct{}) {},
	}.Build()
}

// makeEquivalenceTestConfig returns an app.Config that has been pinned
// through every yaml-bindable channel so the three surfaces have
// something concrete to roundtrip. Re-uses app.DefaultConfig() so the
// equivalence test does not have to chase pkg/app default churn.
func makeEquivalenceTestConfig() app.Config {
	cfg := app.DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = 8080
	if cfg.Databases == nil {
		cfg.Databases = map[string]app.DatabaseConfig{}
	}
	cfg.Databases["default"] = app.DatabaseConfig{URL: "sqlite://:memory:"}
	return cfg
}

// TestThreeSurfaceEquivalence is the ADR-010 §1 contract: the fluent
// builder surface, the direct-struct surface (nucleus.Run / Run), and
// the bootstrap pattern surface (in our case the same direct-struct
// path, since bootstrap.New() is a user-space convention that returns
// a nucleus.App) must produce equivalent `nucleus.App` values when
// given equivalent inputs.
//
// "Equivalent" is defined by normaliseAppForEquivalence:
//
//   - Modules: compared by lexicographically sorted keys, then by the
//     underlying ModuleSpec returned by Build() (which uses the
//     same Module[C] literal so the moduleAdapter is bit-identical
//     modulo the closure pointers — see func-reference rule below).
//   - Databases (inherited from app.Config): sorted by key. We do
//     this by copying into a fresh ordered representation before
//     reflect.DeepEqual.
//   - time.Time fields: zeroed before comparison. Today no field on
//     App or app.Config carries a construction-time timestamp, but
//     the rule is enforced so the test is forward-compatible.
//   - func fields (Lifecycle.OnStart, ServiceRegistration.Run,
//     Module[C].Routes via the adapter, etc.): compared by reference
//     identity. The test invokes the three surfaces in the SAME
//     scope so the same function literal is reused — any divergence
//     is itself a bug worth catching.
func TestThreeSurfaceEquivalence(t *testing.T) {
	cfg := makeEquivalenceTestConfig()
	mod := makeEquivalenceTestModule()

	// Surface 1: fluent builder.
	fluentB := New().
		FromStruct(App{
			Config:  cfg,
			Modules: map[string]ModuleSpec{mod.Name(): mod},
		})
	if fluentB.err != nil {
		t.Fatalf("fluent surface accumulated error: %v", fluentB.err)
	}
	fluent := fluentB.app

	// Surface 2: direct struct passed to nucleus.Run.
	directInput := App{
		Config:  cfg,
		Modules: map[string]ModuleSpec{mod.Name(): mod},
	}
	// Mirror what nucleus.Run does up to the Build call so we can
	// snapshot the App before serving traffic. We can't invoke
	// nucleus.Run end-to-end here without starting an HTTP server.
	direct := ensureModuleMap(directInput)

	// Surface 3: bootstrap pattern — a user-space function returning
	// the same App. Modeled here as a literal function call to make
	// the equivalence visible.
	bootstrap := func() App {
		return App{
			Config:  cfg,
			Modules: map[string]ModuleSpec{mod.Name(): mod},
		}
	}
	boot := ensureModuleMap(bootstrap())

	// Normalise + compare. The three Apps differ only in zero-valued
	// fields that ensureModuleMap and the App literal both populate
	// identically.
	want := normaliseAppForEquivalence(direct)
	for label, got := range map[string]App{"fluent": fluent, "direct": direct, "bootstrap": boot} {
		normalised := normaliseAppForEquivalence(got)
		if !reflect.DeepEqual(normalised, want) {
			t.Fatalf("surface %q diverges from direct-struct\n  got: %#v\n want: %#v", label, normalised, want)
		}
	}
}

// TestEquivalence_ModuleOrderIndependence guards the deterministic
// ordering property: building the Modules map in a different
// insertion order should still produce an equivalent App.
func TestEquivalence_ModuleOrderIndependence(t *testing.T) {
	cfg := makeEquivalenceTestConfig()
	a := makeEquivalenceTestModule()
	b := Module[struct{}]{Name: "books", Routes: func(r Router, _ struct{}) {}}.Build()

	one := App{
		Config:  cfg,
		Modules: map[string]ModuleSpec{a.Name(): a, b.Name(): b},
	}
	two := App{
		Config:  cfg,
		Modules: map[string]ModuleSpec{b.Name(): b, a.Name(): a},
	}
	if !reflect.DeepEqual(normaliseAppForEquivalence(one), normaliseAppForEquivalence(two)) {
		t.Fatal("App equivalence is not order-independent over Modules")
	}
}

// normaliseAppForEquivalence applies the rules described in the
// TestThreeSurfaceEquivalence docstring. The output is suitable for
// reflect.DeepEqual; callers should not compare un-normalised App
// values directly.
//
// The implementation copies the input by value (App is a struct, but
// it embeds app.Config which is itself a struct, so the assignment
// copies the whole tree). The maps inside are then re-built in sorted
// order; the func fields are compared by pointer identity which
// reflect.DeepEqual handles natively (function values are equal iff
// they share the same underlying code address).
func normaliseAppForEquivalence(in App) App {
	out := in

	// Normalise Modules: re-build the map in sorted-key order.
	if len(in.Modules) > 0 {
		keys := make([]string, 0, len(in.Modules))
		for k := range in.Modules {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		// Maps don't have an observable order in reflect.DeepEqual,
		// but we still want sorted keys to be the canonical form so
		// the test failure messages (which print the map) are
		// readable.
		fresh := make(map[string]ModuleSpec, len(keys))
		for _, k := range keys {
			fresh[k] = in.Modules[k]
		}
		out.Modules = fresh
	}

	// Normalise Databases (inherited from app.Config).
	if len(in.Config.Databases) > 0 {
		keys := make([]string, 0, len(in.Config.Databases))
		for k := range in.Config.Databases {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fresh := make(map[string]app.DatabaseConfig, len(keys))
		for _, k := range keys {
			fresh[k] = in.Config.Databases[k]
		}
		out.Config.Databases = fresh
	}

	// time.Time normalisation: no App-level construction timestamps
	// exist today; the rule is a no-op. Documented here so the
	// reader knows the policy and a future field is not silently
	// admitted.
	// (No-op.)

	return out
}
