package nucleus

import (
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// newTestApp builds a real *app.App backed by an in-memory SQLite database
// and core-only wiring (WithoutDefaults), so the Runtime façade can be
// exercised against a live managed connection pool without standing up the
// admin/storage/mail/authz extensions.
func newTestApp(t *testing.T) *app.App {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://:memory:"},
	}
	core, err := app.New(&cfg, app.WithoutDefaults())
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = core.Shutdown(t.Context()) })
	return core
}

func TestRuntimeDBReturnsManagedPool(t *testing.T) {
	core := newTestApp(t)

	// Empty alias resolves to the application default database.
	rt := newRuntime(core, "")
	got := rt.DB()
	if got == nil {
		t.Fatal("Runtime.DB() returned nil for the default alias; want the managed *sql.DB")
	}
	// Pointer identity is the right assertion here: rt.DB() and core.DefaultDB()
	// must return the SAME managed *sql.DB instance (the shared pool), not a
	// fresh handle. The Ping below independently proves the handle is live.
	if want := core.DefaultDB(); got != want {
		t.Fatalf("Runtime.DB() = %p, want the app's managed handle %p", got, want)
	}
	if err := got.Ping(); err != nil {
		t.Fatalf("managed handle is not usable: %v", err)
	}
}

// TestRuntimeDBNamedAliasResolves exercises the non-empty-alias branch of
// runtime.DB() (core.Database(alias) → SqlDB()), distinct from the
// empty-alias core.DefaultDB() path covered above.
func TestRuntimeDBNamedAliasResolves(t *testing.T) {
	core := newTestApp(t)
	rt := newRuntime(core, "default") // "default" is the configured alias in newTestApp
	got := rt.DB()
	if got == nil {
		t.Fatal("Runtime.DB() returned nil for the configured \"default\" alias")
	}
	if want := core.DefaultDB(); got != want {
		t.Fatalf("named-alias Runtime.DB() = %p, want the app's managed handle %p", got, want)
	}
}

func TestRuntimeDBUnknownAliasIsNil(t *testing.T) {
	core := newTestApp(t)
	rt := newRuntime(core, "does-not-exist")
	if rt.DB() != nil {
		t.Fatal("Runtime.DB() for an unknown alias should be nil, not a panic or a wrong handle")
	}
}

func TestRuntimeLoggerNeverNil(t *testing.T) {
	core := newTestApp(t)
	if newRuntime(core, "").Logger() == nil {
		t.Fatal("Runtime.Logger() returned nil; want at least the app logger")
	}
	// An unbacked runtime must still yield a usable logger (slog.Default()).
	if (runtime{}).Logger() == nil {
		t.Fatal("Runtime.Logger() on an unbacked runtime returned nil; want slog.Default()")
	}
}

func TestRuntimeUnbackedDegradesSafely(t *testing.T) {
	// A zero-value runtime (no *app.App) must not panic.
	rt := runtime{}
	if rt.DB() != nil {
		t.Fatal("unbacked Runtime.DB() should be nil")
	}
	if err := rt.AutoMigrate(); err == nil {
		t.Fatal("unbacked Runtime.AutoMigrate() should return an error, not nil")
	}
}
