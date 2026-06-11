package nucleus

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
)

// Runtime is the handle a module receives in its OnStart and OnShutdown
// lifecycle hooks. It is a thin, stable façade over the running
// application container, exposing the managed resources a module needs —
// the shared database pool, schema migration, and the structured logger —
// without leaking the full `*app.App` surface onto the module contract.
//
// Modules MUST use `rt.DB()` instead of opening their own `*sql.DB`. The
// returned handle is owned by the framework: it draws from the configured
// connection pool, participates in the framework's startup/shutdown
// lifecycle (the framework closes it — a module must NOT), and honours the
// application's database configuration. The handle is bound to the
// module's declared `DefaultDB` alias; a module that leaves `DefaultDB`
// empty receives the application's default database.
//
// Runtime is implemented by the framework, not by users. New methods may
// therefore be added in future minor versions without breaking module
// authors (who only ever consume the interface).
type Runtime interface {
	// DB returns the managed `*sql.DB` for the module's database alias
	// (the module's `DefaultDB`, or the application default when unset).
	// It returns nil only when no database is configured for that alias —
	// a misconfiguration the module should surface as an OnStart error.
	DB() *sql.DB

	// DBForRequest resolves the managed `*sql.DB` for the request's resolved
	// scope: the tenant's isolated database when multi-tenant resolution is
	// active (`multitenant.*`), the site's database under multi-site, and the
	// application default otherwise. It mirrors
	// `(*app.App).DatabaseForRequest` semantics — including the
	// tenant-isolation-violation error when an unresolvable tenant would
	// otherwise fall through to a shared database under
	// `multitenant.require_isolated_db` — so module handlers in multi-tenant
	// applications should prefer it over DB(), which is bound to one static
	// alias for the whole module lifetime.
	DBForRequest(r *http.Request) (*sql.DB, error)

	// AutoMigrate synchronises the schema for the given models. NOTE: unlike
	// DB(), it does NOT scope to the module's bound DefaultDB alias — each
	// model is migrated against the database alias declared in its own
	// metadata (defaulting to the application default). It is a development
	// convenience; production deployments should prefer explicit SQL
	// migrations (`nucleus migrate up`), consistent with the SPEC's
	// SQL-first stance.
	AutoMigrate(models ...any) error

	// Logger returns the application's structured logger. It is never nil;
	// callers always receive at least `slog.Default()`.
	Logger() *slog.Logger

	// Session returns the application's session manager — the same
	// instance whose LoadAndSave middleware the framework mounts on every
	// request, so handlers can already read and write session values
	// through the request context. Modules need the manager itself for the
	// operations that go beyond get/put: `RenewToken` after a successful
	// login (session-fixation defence), `Destroy`/`Invalidate` on logout,
	// and flash messaging. The manager is constructed unconditionally by
	// `app.New`; Session returns nil only on an unbacked runtime.
	Session() *auth.SessionManager

	// Authorizer returns the application's RBAC enforcer (ADR-004) — the
	// same instance behind the framework's default-deny middleware and the
	// admin panel. Modules use it to mount `RequireRole` middleware on
	// their routes, manage role groupings (`AddRole`/`RemoveRole`), or
	// audit live policy through the read-only forwarders (`GetPolicy`,
	// `GetGroupingPolicy`, `GetAllRoles`). Returns nil on an unbacked
	// runtime AND when the RBAC subsystem was not attached (an app built
	// with `app.WithoutDefaults()`) — guard accordingly.
	//
	// Mutations (`AddPolicy`/`Deny`/`AddRole`) act on the live in-memory
	// ruleset only: the policy file is read once at startup and runtime
	// changes do not persist across restarts.
	Authorizer() *authz.Enforcer
}

// runtime is the unexported Runtime implementation backing the module
// lifecycle hooks. It wraps the live `*app.App` and the module's resolved
// database alias. A zero/empty alias means "use the application default".
//
// It is deliberately a small, copyable value type (a pointer plus a string)
// used with value receivers: callers store it in a `Runtime` interface and
// it is constructed once per module lifetime. Do NOT add a mutex or other
// non-copy-safe field without switching to a pointer receiver throughout.
type runtime struct {
	core  *app.App
	alias string
}

// newRuntime binds a Runtime to a specific module's default-database alias.
// An empty alias resolves to the application's default database in DB().
func newRuntime(core *app.App, alias string) runtime {
	return runtime{core: core, alias: alias}
}

// DB resolves the managed *sql.DB for the module's bound alias. An empty
// alias uses the application's primary connection. Any resolution failure
// (no app, unknown alias, sql-handle error) yields nil rather than
// panicking, so a misconfigured module degrades to a clean OnStart error
// at the call site rather than a startup panic.
func (rt runtime) DB() *sql.DB {
	if rt.core == nil {
		return nil
	}
	if rt.alias == "" {
		return rt.core.DefaultDB()
	}
	dbConn, err := rt.core.Database(rt.alias)
	if err != nil || dbConn == nil {
		return nil
	}
	sdb, err := dbConn.SqlDB()
	if err != nil {
		return nil
	}
	return sdb
}

// DBForRequest resolves the request's scoped database (tenant/site-aware)
// through the application container and unwraps it to the managed *sql.DB.
// Resolution errors — unknown app, tenant-isolation violations, unknown
// alias — surface as errors so a handler can return a clean 4xx/5xx rather
// than silently querying the wrong database.
func (rt runtime) DBForRequest(r *http.Request) (*sql.DB, error) {
	if rt.core == nil {
		return nil, errors.New("nucleus: Runtime.DBForRequest called on an unbacked runtime")
	}
	dbConn, err := rt.core.DatabaseForRequest(r)
	if err != nil {
		return nil, err
	}
	if dbConn == nil {
		return nil, errors.New("nucleus: no database resolved for request scope")
	}
	return dbConn.SqlDB()
}

// AutoMigrate delegates to the application container's migrator, which
// resolves each model's database alias from its metadata (NOT rt.alias —
// see the interface godoc). Returns a clear error (rather than a nil-deref)
// if the runtime is unbacked.
func (rt runtime) AutoMigrate(models ...any) error {
	if rt.core == nil {
		return errors.New("nucleus: Runtime.AutoMigrate called on an unbacked runtime")
	}
	return rt.core.AutoMigrate(models...)
}

// Logger returns the application's structured logger, falling back to
// slog.Default() so the return is never nil.
func (rt runtime) Logger() *slog.Logger {
	if rt.core == nil || rt.core.Logger == nil {
		return slog.Default()
	}
	return rt.core.Logger
}

// Session returns the application's session manager, or nil on an
// unbacked runtime — mirroring DB()'s degrade-to-nil contract so a
// misconfigured module fails at its own call site, not with a panic
// inside the framework.
func (rt runtime) Session() *auth.SessionManager {
	if rt.core == nil {
		return nil
	}
	return rt.core.Session
}

// Authorizer returns the application's RBAC enforcer. Nil on an unbacked
// runtime and also on apps built with app.WithoutDefaults(), where the
// RBAC subsystem is never attached — same degrade-to-nil posture as DB().
func (rt runtime) Authorizer() *authz.Enforcer {
	if rt.core == nil {
		return nil
	}
	return rt.core.Authorizer
}
