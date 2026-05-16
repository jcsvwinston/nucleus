package nucleus

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// TestNew_DefaultsConfig confirms the builder is seeded with the
// canonical defaults from pkg/app and starts with an empty Modules map.
func TestNew_DefaultsConfig(t *testing.T) {
	b := New()
	if b == nil {
		t.Fatal("New() returned nil")
	}
	if b.app.Modules == nil {
		t.Fatal("New() did not allocate the Modules map")
	}
	if b.app.Config.LogLevel == "" {
		t.Fatal("New() did not inherit app.DefaultConfig LogLevel")
	}
}

// TestMount_KeysByName confirms Mount stores modules keyed by Name().
func TestMount_KeysByName(t *testing.T) {
	mod := Module[struct{}]{Name: "articles", Routes: func(r Router, _ struct{}) {}}
	b := New().Mount(mod.Build())
	if got, ok := b.app.Modules["articles"]; !ok || got == nil {
		t.Fatalf("Mount did not store the module under its Name; got %v", b.app.Modules)
	}
}

// TestMount_DuplicateNameRejected confirms duplicate names short-circuit
// the chain and surface via Build/Start, not via a mid-chain panic.
func TestMount_DuplicateNameRejected(t *testing.T) {
	mod := Module[struct{}]{Name: "articles", Routes: func(r Router, _ struct{}) {}}
	_, err := New().Mount(mod.Build()).Mount(mod.Build()).Build()
	if err == nil {
		t.Fatal("expected an error for duplicate module Name, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate module name") {
		t.Fatalf("expected duplicate-name error, got %v", err)
	}
}

// TestMount_NilRejected confirms a nil ModuleSpec is rejected at the
// chain step.
func TestMount_NilRejected(t *testing.T) {
	_, err := New().Mount(nil).Build()
	if err == nil {
		t.Fatal("expected an error for nil ModuleSpec, got nil")
	}
	if !strings.Contains(err.Error(), "nil ModuleSpec") {
		t.Fatalf("expected nil ModuleSpec error, got %v", err)
	}
}

// TestFromConfigFile_NoArgsRejected confirms an empty path slice is
// reported as a clear error, not a panic.
func TestFromConfigFile_NoArgsRejected(t *testing.T) {
	_, err := New().FromConfigFile().Build()
	if err == nil {
		t.Fatal("expected an error for empty paths, got nil")
	}
	if !strings.Contains(err.Error(), "at least one path") {
		t.Fatalf("expected at-least-one-path error, got %v", err)
	}
}

// TestFromConfigFile_MultiFileIsPhase2 confirms multi-file calls fail
// loud in Phase 1.
func TestFromConfigFile_MultiFileIsPhase2(t *testing.T) {
	_, err := New().FromConfigFile("a.yaml", "b.yaml").Build()
	if err == nil {
		t.Fatal("expected a Phase-2 sentinel error for multi-file load, got nil")
	}
	if !strings.Contains(err.Error(), "Phase 2") {
		t.Fatalf("expected the error to reference Phase 2, got %v", err)
	}
}

// TestFromStruct_PreservesModuleMap confirms FromStruct never drops
// to a nil Modules map.
func TestFromStruct_PreservesModuleMap(t *testing.T) {
	b := New().FromStruct(App{Config: app.DefaultConfig()})
	if b.app.Modules == nil {
		t.Fatal("FromStruct dropped the Modules map allocation")
	}
}

// TestModule_BuildRequiresName confirms Module[C].Build panics when
// Name is empty.
func TestModule_BuildRequiresName(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected Module.Build to panic on empty Name")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "Name is required") {
			t.Fatalf("unexpected panic value: %v", r)
		}
	}()
	_ = Module[struct{}]{}.Build()
}

// TestModuleAdapter_NilCallbacksAreNoOp confirms a Module[C] with nil
// callbacks does not panic when the framework invokes them.
func TestModuleAdapter_NilCallbacksAreNoOp(t *testing.T) {
	mod := Module[struct{}]{Name: "empty"}.Build()
	mod.Routes(nil) // nil Routes must short-circuit
	if err := mod.OnStart(context.Background(), nil); err != nil {
		t.Fatalf("nil OnStart should be a no-op, got %v", err)
	}
	if err := mod.OnShutdown(context.Background(), nil); err != nil {
		t.Fatalf("nil OnShutdown should be a no-op, got %v", err)
	}
}

// TestModuleAdapter_DefensiveCopy confirms the adapter returns
// independent slices, not aliases into the source Module[C].
func TestModuleAdapter_DefensiveCopy(t *testing.T) {
	mod := Module[struct{}]{
		Name:     "needs-default",
		Requires: []string{"default"},
		Models:   []any{"m1"},
	}.Build()
	got := mod.Requires()
	got[0] = "mutated"
	again := mod.Requires()
	if again[0] != "default" {
		t.Fatalf("Requires() returned an aliased slice; mutation leaked: %v", again)
	}
	gotMods := mod.Models()
	gotMods[0] = "mutated"
	againMods := mod.Models()
	if againMods[0].(string) != "m1" {
		t.Fatalf("Models() returned an aliased slice; mutation leaked: %v", againMods)
	}
}

// TestValidateModules_RequiresMissingAlias confirms the boot-time
// guard against a module declaring Requires() for an unconfigured
// database alias.
func TestValidateModules_RequiresMissingAlias(t *testing.T) {
	mod := Module[struct{}]{
		Name:     "needs-analytics",
		Requires: []string{"analytics"},
	}.Build()
	a := App{Config: app.DefaultConfig(), Modules: map[string]ModuleSpec{"needs-analytics": mod}}
	err := validateModules(&a)
	if !errors.Is(err, ErrModuleRequiresMissingDB) {
		t.Fatalf("expected ErrModuleRequiresMissingDB, got %v", err)
	}
	if !strings.Contains(err.Error(), "analytics") {
		t.Fatalf("error should name the missing alias, got %v", err)
	}
}

// TestValidateModules_RequiresPresent confirms validateModules returns
// nil when every Requires() alias is configured.
func TestValidateModules_RequiresPresent(t *testing.T) {
	mod := Module[struct{}]{
		Name:     "needs-default",
		Requires: []string{"default"},
	}.Build()
	cfg := app.DefaultConfig()
	if cfg.Databases == nil {
		cfg.Databases = make(map[string]app.DatabaseConfig)
	}
	cfg.Databases["default"] = app.DatabaseConfig{URL: "sqlite://:memory:"}
	a := App{Config: cfg, Modules: map[string]ModuleSpec{"needs-default": mod}}
	if err := validateModules(&a); err != nil {
		t.Fatalf("validateModules: %v", err)
	}
}

// TestSortedModuleKeys_DeterministicOrder confirms the helper produces
// a deterministic ordering — a precondition for the three-surface
// equivalence test.
func TestSortedModuleKeys_DeterministicOrder(t *testing.T) {
	m := map[string]ModuleSpec{
		"z": Module[struct{}]{Name: "z"}.Build(),
		"a": Module[struct{}]{Name: "a"}.Build(),
		"m": Module[struct{}]{Name: "m"}.Build(),
	}
	got := sortedModuleKeys(m)
	want := []string{"a", "m", "z"}
	if len(got) != len(want) {
		t.Fatalf("unexpected length: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected order at %d: got %v want %v", i, got, want)
		}
	}
}

// TestErrorShortCircuits confirms that an error stashed early in the
// chain prevents subsequent fluent steps from doing work — the
// canonical bufio.Scanner pattern.
func TestErrorShortCircuits(t *testing.T) {
	mod := Module[struct{}]{Name: "articles"}.Build()
	noopMW := func(next http.Handler) http.Handler { return next }
	b := New().
		FromConfigFile(). // stashes "at least one path required"
		Mount(mod).
		Use(noopMW)
	if b.err == nil {
		t.Fatal("expected the early-chain error to persist; subsequent steps should not clear it")
	}
	if !strings.Contains(b.err.Error(), "at least one path") {
		t.Fatalf("error mutated by later chain steps: %v", b.err)
	}
}

// TestUse_AppendsGlobalMiddleware confirms Use registers global
// middleware on the App's Middleware slice in order.
func TestUse_AppendsGlobalMiddleware(t *testing.T) {
	mw1 := func(next http.Handler) http.Handler { return next }
	mw2 := func(next http.Handler) http.Handler { return next }
	b := New().Use(mw1).Use(mw2)
	if len(b.app.Middleware) != 2 {
		t.Fatalf("expected 2 middleware entries, got %d", len(b.app.Middleware))
	}
}
