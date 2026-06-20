package nucleus

import (
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/db"
)

// Runtime.Models returns the app's shared registry, and a registration made
// through it is visible via the accessor (the contract orbit's Data Studio
// relies on: the framework registers every module's Models() into this same
// instance).
func TestRuntimeModelsReturnsSharedRegistry(t *testing.T) {
	core := newTestApp(t)
	rt := newRuntime(core, "")

	reg := rt.Models()
	if reg == nil {
		t.Fatal("Runtime.Models() returned nil; want the app's registry")
	}
	if reg != core.Models {
		t.Fatalf("Runtime.Models() must return the app's shared registry instance (%p), got %p", core.Models, reg)
	}

	before := reg.Count()
	if err := reg.Register(&testWidget{}); err != nil {
		t.Fatalf("registry.Register: %v", err)
	}
	if got := rt.Models().Count(); got != before+1 {
		t.Fatalf("Runtime.Models() did not reflect a new registration: count %d, want %d", got, before+1)
	}
}

func TestRuntimeModelsUnbackedIsNil(t *testing.T) {
	if (runtime{}).Models() != nil {
		t.Fatal("Runtime.Models() on an unbacked runtime should be nil, not a panic")
	}
}

// Runtime.Databases exposes every managed handle by alias, returns the managed
// pool (not a fresh connection), and hands back a snapshot copy.
func TestRuntimeDatabasesSnapshot(t *testing.T) {
	core := newTestApp(t)
	rt := newRuntime(core, "")

	dbs := rt.Databases()
	if dbs == nil {
		t.Fatal("Runtime.Databases() returned nil for a backed app")
	}
	if len(dbs) != 1 {
		t.Fatalf("Runtime.Databases() = %d handle(s), want exactly 1 (the configured \"default\" alias)", len(dbs))
	}
	got, ok := dbs["default"]
	if !ok || got == nil {
		t.Fatalf("Runtime.Databases() missing the \"default\" handle: %d handle(s) returned", len(dbs))
	}
	// Same managed pool as DB()/DefaultDB(), not a fresh handle.
	if want := core.DefaultDB(); got != want {
		t.Fatalf("Databases()[\"default\"] = %p, want the managed handle %p", got, want)
	}
	if err := got.Ping(); err != nil {
		t.Fatalf("handle from Databases() is not usable: %v", err)
	}

	// Snapshot copy: mutating the returned map must not leak into the framework.
	delete(dbs, "default")
	if _, ok := rt.Databases()["default"]; !ok {
		t.Fatal("Runtime.Databases() must return a snapshot copy; a caller's mutation leaked into the framework registry")
	}
}

func TestRuntimeDatabasesUnbackedIsNil(t *testing.T) {
	if (runtime{}).Databases() != nil {
		t.Fatal("Runtime.Databases() on an unbacked runtime should be nil, not a panic")
	}
}

// A handle that cannot be unwrapped to *sql.DB (here a zero-value *db.DB with no
// live pool) is omitted from the snapshot rather than surfaced as a nil entry,
// so callers never have to nil-check the values.
func TestRuntimeDatabasesOmitsUnusableHandles(t *testing.T) {
	rt := runtime{core: &app.App{DBs: map[string]*db.DB{"broken": {}}}}
	got := rt.Databases()
	if _, ok := got["broken"]; ok {
		t.Fatal("Runtime.Databases() must omit an alias whose handle cannot be unwrapped to *sql.DB")
	}
	if len(got) != 0 {
		t.Fatalf("expected an empty snapshot (the only handle was unusable), got %d", len(got))
	}
}
