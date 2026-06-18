package nucleus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// testWidget is a minimal registry-compatible model for exercising the
// Module.Models -> registry wiring (sub-slice 1). It embeds model.BaseModel
// for the standard id/timestamps, mirroring how real module models are shaped.
type testWidget struct {
	model.BaseModel

	Name string `db:"required" json:"name"`
}

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

// TestRegisterModuleModelsCatalogsModels verifies sub-slice 1: a module's
// declared Models() are registered with the application model registry by Run's
// registerModuleModels helper, so the admin panel and generic CRUD can see them.
func TestRegisterModuleModelsCatalogsModels(t *testing.T) {
	core := newTestApp(t)
	before := core.Models.Count()

	spec := Module[struct{}]{
		Name:   "widgets",
		Models: []any{testWidget{}},
	}.Build()

	if err := registerModuleModels(core, []ModuleSpec{spec}); err != nil {
		t.Fatalf("registerModuleModels: %v", err)
	}
	if got := core.Models.Count(); got != before+1 {
		t.Fatalf("registry Count = %d, want %d (one model registered)", got, before+1)
	}
}

// TestRegisterModuleModelsEmptyIsNoop confirms a module with no Models() leaves
// the registry untouched and returns no error.
func TestRegisterModuleModelsEmptyIsNoop(t *testing.T) {
	core := newTestApp(t)
	before := core.Models.Count()

	spec := Module[struct{}]{Name: "empty"}.Build()
	if err := registerModuleModels(core, []ModuleSpec{spec}); err != nil {
		t.Fatalf("registerModuleModels: %v", err)
	}
	if got := core.Models.Count(); got != before {
		t.Fatalf("registry Count = %d, want unchanged %d", got, before)
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
	if _, err := rt.DBForRequest(httptest.NewRequest(http.MethodGet, "/", nil)); err == nil {
		t.Fatal("unbacked Runtime.DBForRequest() should return an error, not nil")
	}
}

// newMultiTenantTestApp builds an app with header-resolved multi-tenancy and
// one isolated tenant database, mirroring a real `multitenant.*` deployment.
func newMultiTenantTestApp(t *testing.T) *app.App {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.Databases = map[string]app.DatabaseConfig{
		"default":     {URL: "sqlite://:memory:"},
		"tenant_acme": {URL: "sqlite://:memory:"},
	}
	cfg.MultiTenant = app.MultiTenantConfig{
		Enabled:               true,
		Resolver:              "header",
		Header:                "X-Tenant-ID",
		RequireIsolatedDB:     true,
		DatabaseAliasTemplate: "tenant_%s",
		Tenants: map[string]app.TenantConfig{
			"acme": {Database: "tenant_acme"},
		},
	}
	core, err := app.New(&cfg, app.WithoutDefaults())
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = core.Shutdown(t.Context()) })
	return core
}

// TestRuntimeDBForRequestResolvesTenant is the regression guard for the
// fluent-API multi-tenant gap (fleetdesk finding #6): a module handler must
// be able to reach the request's tenant database through the Runtime façade.
func TestRuntimeDBForRequestResolvesTenant(t *testing.T) {
	core := newMultiTenantTestApp(t)
	rt := newRuntime(core, "")

	req := httptest.NewRequest(http.MethodGet, "/api/fleets", nil)
	req.Header.Set("X-Tenant-ID", "acme")
	got, err := rt.DBForRequest(req)
	if err != nil {
		t.Fatalf("DBForRequest(tenant=acme): %v", err)
	}
	tenantDB, err := core.Database("tenant_acme")
	if err != nil {
		t.Fatalf("core.Database(tenant_acme): %v", err)
	}
	want, err := tenantDB.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}
	if got != want {
		t.Fatalf("DBForRequest returned %p, want the tenant_acme managed handle %p", got, want)
	}
}

// TestRuntimeDBForRequestIsolationViolation pins that an unknown tenant under
// require_isolated_db surfaces as an error instead of silently falling back
// to a shared database.
func TestRuntimeDBForRequestIsolationViolation(t *testing.T) {
	core := newMultiTenantTestApp(t)
	rt := newRuntime(core, "")

	req := httptest.NewRequest(http.MethodGet, "/api/fleets", nil)
	req.Header.Set("X-Tenant-ID", "ghost")
	if _, err := rt.DBForRequest(req); err == nil {
		t.Fatal("DBForRequest(unknown tenant) should error under require_isolated_db, not fall back to a shared DB")
	}
}

// TestRuntimeDBForRequestDefaultScope: without multi-tenant config the
// resolver yields the application default database.
func TestRuntimeDBForRequestDefaultScope(t *testing.T) {
	core := newTestApp(t)
	rt := newRuntime(core, "")

	got, err := rt.DBForRequest(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("DBForRequest(default scope): %v", err)
	}
	if want := core.DefaultDB(); got != want {
		t.Fatalf("DBForRequest = %p, want default managed handle %p", got, want)
	}
}

// TestRuntimeSessionAndAuthorizerExposeAppInstances pins the façade
// contract for the auth surface (fleetdesk finding #21): Session() and
// Authorizer() hand back the App's OWN instances — the ones already
// wired into the global middleware chain — never fresh constructions.
// A fresh scs manager could not read the session data loaded by the
// mounted middleware (scs context keys are per-instance), and a fresh
// enforcer would not see the loaded policy.
//
// newTestApp builds with WithoutDefaults, which skips the RBAC subsystem
// and leaves core.Authorizer nil — a nil-nil identity check would be
// vacuous. The session manager is constructed unconditionally, so its
// identity is asserted against the real instance; for the enforcer the
// exported field is populated explicitly to exercise the pass-through
// with a non-nil pointer.
func TestRuntimeSessionAndAuthorizerExposeAppInstances(t *testing.T) {
	core := newTestApp(t)
	rt := newRuntime(core, "")

	if core.Session == nil {
		t.Fatal("precondition: app.New must construct the session manager unconditionally")
	}
	if got, want := rt.Session(), core.Session; got != want {
		t.Fatalf("Runtime.Session() = %p, want the app's instance %p", got, want)
	}

	// WithoutDefaults leaves Authorizer nil — the documented degrade case.
	if rt.Authorizer() != nil {
		t.Fatalf("Runtime.Authorizer() = %p on a WithoutDefaults app, want nil", rt.Authorizer())
	}

	enf, err := authz.New(core.Logger, "")
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}
	core.Authorizer = enf
	if got := rt.Authorizer(); got != enf {
		t.Fatalf("Runtime.Authorizer() = %p, want the app's instance %p", got, enf)
	}
}

// TestRuntimeSessionAuthorizerUnbackedAreNil extends the unbacked-runtime
// degrade contract (no panics, nil returns) to the auth accessors.
func TestRuntimeSessionAuthorizerUnbackedAreNil(t *testing.T) {
	rt := runtime{}
	if rt.Session() != nil {
		t.Fatal("unbacked Runtime.Session() must be nil")
	}
	if rt.Authorizer() != nil {
		t.Fatal("unbacked Runtime.Authorizer() must be nil")
	}
}

// TestRuntimeMailerStorageExposeAppInstances pins the service-surface
// accessors: Mailer() and Storage() hand back the App's own instances (the
// ones the framework built from config and wrapped with health/breaker/cleaner
// lifecycle), so a module reaches those rather than constructing duplicates.
// newTestApp uses WithoutDefaults, so both fields start nil (the documented
// degrade case); they are populated explicitly to exercise the pass-through.
func TestRuntimeMailerStorageExposeAppInstances(t *testing.T) {
	core := newTestApp(t)
	rt := newRuntime(core, "")

	// WithoutDefaults leaves both nil.
	if rt.Mailer() != nil {
		t.Fatalf("Runtime.Mailer() = %v on a WithoutDefaults app, want nil", rt.Mailer())
	}
	if rt.Storage() != nil {
		t.Fatalf("Runtime.Storage() = %v on a WithoutDefaults app, want nil", rt.Storage())
	}

	// A pointer-typed sender, not the value-type noop: two noopSender{}
	// values compare equal, which would make this pass-through assertion
	// vacuous (a broken Mailer() fabricating a fresh noop would still pass).
	sender := &sentinelSender{}
	core.Mailer = sender
	if got := rt.Mailer(); got != sender {
		t.Fatalf("Runtime.Mailer() = %v, want the app's instance %v", got, sender)
	}

	store, err := storage.New(storage.Config{
		Provider: storage.ProviderLocal,
		Local:    storage.LocalConfig{Path: t.TempDir()},
	}, core.Logger)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	core.Storage = store
	if got := rt.Storage(); got != store {
		t.Fatalf("Runtime.Storage() = %p, want the app's instance %p", got, store)
	}
}

// sentinelSender is a pointer-typed mail.Sender used only for identity
// assertions: unlike the value-type noop sender, two instances are never
// equal, so a pass-through check actually proves the accessor returns the
// app's own instance.
type sentinelSender struct{}

func (*sentinelSender) Send(context.Context, mail.Message) error { return nil }

// TestRuntimeServiceAccessorsUnbackedAreNil extends the unbacked-runtime
// degrade contract to the Mailer/Storage/JWT service accessors.
func TestRuntimeServiceAccessorsUnbackedAreNil(t *testing.T) {
	rt := runtime{}
	if rt.Mailer() != nil {
		t.Fatal("unbacked Runtime.Mailer() must be nil")
	}
	if rt.Storage() != nil {
		t.Fatal("unbacked Runtime.Storage() must be nil")
	}
	if rt.JWT() != nil {
		t.Fatal("unbacked Runtime.JWT() must be nil")
	}
}

// TestRuntimeJWTExposesAppInstance pins the JWT service accessor: JWT() hands
// back the App's own *auth.JWTManager (the one the framework built from
// jwt_secret/jwt_keys), so a module mints and verifies tokens through the same
// manager rather than constructing a duplicate from a copied secret. newTestApp
// configures no signing material, so App.JWT starts nil (the documented degrade
// case); it is set explicitly to exercise the pass-through with a non-nil
// pointer.
func TestRuntimeJWTExposesAppInstance(t *testing.T) {
	core := newTestApp(t)
	rt := newRuntime(core, "")

	// No jwt_secret / jwt_keys configured → App.JWT is nil.
	if got := rt.JWT(); got != nil {
		t.Fatalf("Runtime.JWT() = %p with no signing material, want nil", got)
	}

	// A non-empty HS256 secret yields a usable manager; the value is a fixed
	// test fixture (entropy is irrelevant — this only exercises the pass-through).
	mgr := auth.NewJWTManager("test-secret-test-secret-test-secret", time.Hour)
	core.JWT = mgr
	if got := rt.JWT(); got != mgr {
		t.Fatalf("Runtime.JWT() = %p, want the app's instance %p", got, mgr)
	}
}
