package nucleus

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/storage"
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

	// Mailer returns the application's outbound mail sender — the same
	// instance the framework built from the `mail_*` config and wrapped
	// with the health check and circuit breaker. Modules send through it
	// (e.g. from a signal handler or a task) rather than constructing their
	// own sender, which would bypass that lifecycle. Returns nil on an
	// unbacked runtime AND when the mail subsystem was not attached
	// (`app.WithoutDefaults()`) — guard accordingly. When attached, the
	// default driver is a no-op sender, so a non-nil Mailer is safe to call
	// even when no SMTP is configured.
	Mailer() mail.Sender

	// Storage returns the application's object store — the same instance the
	// framework built from the `storage_*` config, with its background
	// cleaner and circuit breaker. Modules Put/Get/SignedURL through it for
	// uploads and generated artifacts (report exports, etc.) instead of
	// opening their own store. Returns nil on an unbacked runtime AND when
	// the storage subsystem was not attached (`app.WithoutDefaults()`).
	Storage() storage.Store

	// JWT returns the application's JWT manager — the same instance the
	// framework built from `jwt_secret` / `jwt_keys[]` (and whose JWKS the
	// framework auto-mounts for asymmetric keys). Modules mint bearer tokens
	// (`Generate`) and validate them (`Validate`, or mount `Middleware` /
	// `OptionalJWTMiddleware`) through it instead of constructing their own
	// manager from a duplicated secret.
	//
	// Returns nil on an unbacked runtime AND when no signing material is
	// configured (`App.JWT` is nil) — a read-only service that only consumes
	// JWTs minted by an external IdP may legitimately leave it unset, so guard
	// accordingly.
	//
	// Treat the returned manager as read-only from request handlers: its
	// `RotateKey` / `RemoveKey` methods mutate the shared keyset for every
	// concurrent caller (and the framework's JWKS endpoint), so key lifecycle
	// is an operator concern, not a per-request module call — the same posture
	// as `Authorizer()`'s in-memory policy mutations.
	JWT() *auth.JWTManager

	// Models returns the application's model registry — the same instance the
	// framework registers every mounted module's `Models()` into. A module that
	// hosts a generic data UI (enumerate models, run CRUD over arbitrary models)
	// reads it; ordinary modules that only use their own typed models do not need
	// it. Returns nil on an unbacked runtime. The registry is process-wide and
	// shared — treat schema mutations (`UpdateFieldMeta`) as an operator concern,
	// not a per-request call.
	Models() *model.Registry

	// Databases returns a snapshot of every configured managed database handle,
	// keyed by alias (the application default included), unwrapped to `*sql.DB`.
	// Unlike `DB()` — bound to the module's single alias — this exposes all
	// handles, for a module that browses across databases (a data console over a
	// multi-database or multi-tenant topology). The returned map is a copy:
	// mutating it does not affect the framework's registry, and the handles
	// remain framework-owned (a module must NOT close them). Returns nil on an
	// unbacked runtime; an alias whose handle cannot be unwrapped is omitted.
	//
	// It is NOT scoped to the module's own alias or the request's tenant — every
	// configured handle is returned, so the caller owns any tenant-isolation
	// policy. Intended for a trusted, first-party admin module (orbit).
	Databases() map[string]*sql.DB

	// DatabaseHandle returns the framework's managed *db.DB wrapper for the
	// default database. It is the engine-aware handle a deep module needs for
	// operations the raw *sql.DB cannot do — `db.NewMigrator`'s dialect-aware DDL,
	// and `Engine()`/`System()` for dialect detection. Most modules want DB() (the
	// *sql.DB) instead. Returns nil on an unbacked runtime or when no default
	// database is configured (e.g. app.WithoutDefaults()). The handle is
	// framework-owned — a module must NOT Close it.
	DatabaseHandle() *db.DB

	// DatabaseHandles returns a snapshot of every managed *db.DB wrapper keyed by
	// alias — the engine-aware counterpart to Databases() (which returns *sql.DB)
	// — for a module that needs per-database dialect/migration capability across a
	// multi-database topology (e.g. orbit's admin). The map is freshly allocated;
	// the handles remain framework-owned (do NOT Close them). Nil on an unbacked
	// runtime.
	DatabaseHandles() map[string]*db.DB

	// Observability returns a first-party view of the framework's in-process
	// event bus, for a module that renders a live activity feed (orbit's live
	// SQL/HTTP view). It emits nucleus-owned event values through EventBus, so
	// the module never touches the lower-level pkg/observability surface and is
	// freed from its pooled-event Release discipline. Returns nil on an unbacked
	// runtime or when no bus is attached.
	Observability() EventBus
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

// Mailer returns the application's mail sender, or nil on an unbacked
// runtime / an app built with app.WithoutDefaults() (no mail subsystem).
func (rt runtime) Mailer() mail.Sender {
	if rt.core == nil {
		return nil
	}
	return rt.core.Mailer
}

// Storage returns the application's object store, or nil on an unbacked
// runtime / an app built with app.WithoutDefaults() (no storage subsystem).
func (rt runtime) Storage() storage.Store {
	if rt.core == nil {
		return nil
	}
	return rt.core.Storage
}

// JWT returns the application's JWT manager, or nil on an unbacked runtime /
// when no signing material is configured (App.JWT is nil).
func (rt runtime) JWT() *auth.JWTManager {
	if rt.core == nil {
		return nil
	}
	return rt.core.JWT
}

// Models returns the application's model registry (App.Models), or nil on an
// unbacked runtime — the same degrade-to-nil contract as DB().
func (rt runtime) Models() *model.Registry {
	if rt.core == nil {
		return nil
	}
	return rt.core.Models
}

// Databases returns a snapshot copy of every managed database handle keyed by
// alias, unwrapped to *sql.DB. Nil on an unbacked runtime. A handle that is nil
// or fails to unwrap is omitted rather than surfaced as a nil map entry, so the
// caller never has to nil-check the values. The returned map is freshly
// allocated; mutating it does not affect the framework's internal registry.
func (rt runtime) Databases() map[string]*sql.DB {
	if rt.core == nil {
		return nil
	}
	// rt.core.DBs is populated once at app.New and never mutated afterward (no
	// runtime add/remove path), so this read needs no lock. If a dynamic
	// "add database" path is ever introduced, guard this iteration with the
	// app's mutex.
	out := make(map[string]*sql.DB, len(rt.core.DBs))
	for alias, conn := range rt.core.DBs {
		if conn == nil {
			continue
		}
		sdb, err := conn.SqlDB()
		if err != nil || sdb == nil {
			continue
		}
		out[alias] = sdb
	}
	return out
}

// DatabaseHandle returns the framework's default *db.DB wrapper (App.DB), or nil
// on an unbacked runtime. The framework owns its lifecycle — a module must NOT
// Close it.
func (rt runtime) DatabaseHandle() *db.DB {
	if rt.core == nil {
		return nil
	}
	return rt.core.DB
}

// DatabaseHandles returns a snapshot copy of every managed *db.DB wrapper keyed
// by alias (App.DBs). Nil on an unbacked runtime; nil entries are omitted. The
// map is freshly allocated; the handles remain framework-owned (do NOT Close).
func (rt runtime) DatabaseHandles() map[string]*db.DB {
	if rt.core == nil {
		return nil
	}
	// Lock-free for the same reason as Databases(): rt.core.DBs is written once
	// at app.New and never mutated afterward.
	out := make(map[string]*db.DB, len(rt.core.DBs))
	for alias, h := range rt.core.DBs {
		if h != nil {
			out[alias] = h
		}
	}
	return out
}

// Observability returns a first-party EventBus over the application's event bus
// (App.Observability), or nil on an unbacked runtime / when no bus is attached.
func (rt runtime) Observability() EventBus {
	if rt.core == nil || rt.core.Observability == nil {
		return nil
	}
	return busAdapter{bus: rt.core.Observability}
}
