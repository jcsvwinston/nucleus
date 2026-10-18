package nucleus

import (
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/db"
)

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

// DatabaseHandle/DatabaseHandles return the framework's engine-aware *db.DB
// wrappers (what migrations + dialect detection need), as shared instances +
// a snapshot map.
func TestRuntimeDatabaseHandles(t *testing.T) {
	core := newTestApp(t)
	rt := newRuntime(core, "")

	if got := rt.DatabaseHandle(); got == nil || got != core.DB {
		t.Fatalf("DatabaseHandle() = %p, want the app's default *db.DB %p", got, core.DB)
	}
	// Engine-aware: the reason *db.DB is exposed (vs the *sql.DB from DB()).
	if eng := rt.DatabaseHandle().Engine(); eng == "" {
		t.Error("DatabaseHandle().Engine() is empty; the wrapper should carry the engine")
	}

	hs := rt.DatabaseHandles()
	if got, ok := hs["default"]; !ok || got != core.DBs["default"] {
		t.Fatalf("DatabaseHandles()[\"default\"] = %v, want the app's wrapper %p", got, core.DBs["default"])
	}
	// Snapshot copy: mutating the result must not affect the framework.
	delete(hs, "default")
	if _, ok := rt.DatabaseHandles()["default"]; !ok {
		t.Error("DatabaseHandles() must return a snapshot copy; a caller's mutation leaked")
	}
}

func TestRuntimeDatabaseHandlesUnbackedIsNil(t *testing.T) {
	if (runtime{}).DatabaseHandle() != nil {
		t.Fatal("DatabaseHandle() on an unbacked runtime should be nil")
	}
	if (runtime{}).DatabaseHandles() != nil {
		t.Fatal("DatabaseHandles() on an unbacked runtime should be nil")
	}
	// Backed runtime but no default DB configured (e.g. WithoutDefaults + no db):
	// nil/empty, not a panic.
	backed := runtime{core: &app.App{}}
	if backed.DatabaseHandle() != nil {
		t.Error("DatabaseHandle() with a nil App.DB should be nil")
	}
	if len(backed.DatabaseHandles()) != 0 {
		t.Error("DatabaseHandles() with no App.DBs should be empty")
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
