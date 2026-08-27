package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/health"
	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observability"
	"github.com/jcsvwinston/nucleus/pkg/observability/hooks"
	"github.com/jcsvwinston/nucleus/pkg/observe"
	"github.com/jcsvwinston/nucleus/pkg/outbox"
	"github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// App is the main Nucleus application container. It wires the minimum runtime
// dependencies (config, logger, router, DB, and model registry).
//
// By default, app.New(cfg) initializes all subsystems (storage, mail, authz).
// Use app.WithoutDefaults() to initialize only core, then add extensions explicitly.
type App struct {
	Config     *Config
	Logger     *slog.Logger
	Router     *router.Router
	DB         *db.DB
	DBs        map[string]*db.DB
	Mailer     mail.Sender
	Session    *auth.SessionManager
	JWT        *auth.JWTManager
	Models     *model.Registry
	Authorizer *authz.Enforcer
	Storage    storage.Store
	// AuthChain is the ordered authentication chain built from
	// auth_backends. Nil when the list is empty — an application with
	// nothing to authenticate against says so by leaving it unset rather
	// than carrying an empty chain that always rejects.
	AuthChain *auth.Chain
	Outbox    *outbox.ManagedOutbox
	Templates *template.Template

	// Observability is the in-process event bus for HTTP, SQL, session and
	// custom events. It is always non-nil after app.New returns.
	// Subscribers (such as the orbit admin module) attach to it directly.
	// See pkg/observability for the full ownership model.
	Observability *observability.Bus

	// SessionRecorder produces session-change events on the Observability
	// bus. It is used by the session manager middleware below.
	SessionRecorder *hooks.SessionRecorder

	databaseDefaultAlias string
	scopeResolver        *requestScopeResolver
	extensions           []Extension
	openAuthz            bool
	// extraHealthProbes are caller-owned /healthz checks added via
	// RegisterHealthProbe (e.g. one per ServiceRegistration.Health).
	extraHealthProbes []health.Prober

	mu             sync.Mutex
	server         *http.Server
	shutdownFns    []func(context.Context) error
	openAPIRoutes  map[string]struct{}
	storageCleaner *storage.Cleaner
}

// AutoMigrate synchronizes the database schema with the provided model
// definitions. Supported dialects: SQLite, PostgreSQL, MySQL, MSSQL,
// and Oracle. Unknown engines return ErrAutoMigrate — use explicit SQL
// migration files plus `nucleus migrate` instead (see
// `website/docs/getting-started/quickstart.md` for the multi-driver
// path). It extracts metadata from models and executes dialect-aware
// `CREATE TABLE` statements, idempotent where the engine supports it
// (`CREATE TABLE IF NOT EXISTS` on sqlite/postgres/mysql, `IF
// OBJECT_ID … IS NULL` on MSSQL, PL/SQL ORA-00955 swallow on Oracle).
// Existing tables are not modified by AutoMigrate — schema evolution
// still requires migrations.
func (a *App) AutoMigrate(models ...any) error {
	a.Logger.Info("starting auto-migration", "count", len(models))

	for _, m := range models {
		meta, err := model.ExtractMeta(m)
		if err != nil {
			return fmt.Errorf("automigrate: failed to extract meta for %T: %w", m, err)
		}

		dbAlias := meta.DatabaseAlias
		if dbAlias == "" {
			dbAlias = "default"
		}

		dbConn, ok := a.DBs[dbAlias]
		if !ok {
			return fmt.Errorf("automigrate: database alias %q not found", dbAlias)
		}

		sqlDB, err := dbConn.SqlDB()
		if err != nil {
			return fmt.Errorf("automigrate: failed to get sql handle for %q: %w", dbAlias, err)
		}

		up, err := buildAutoMigrateScaffold(dbConn.System(), meta)
		if err != nil {
			return fmt.Errorf("automigrate: %w", err)
		}

		// ExecScript splits multi-statement scaffolds per dialect — Oracle
		// emits several `/`-separated PL/SQL blocks (one per CREATE TABLE /
		// INDEX) and go-ora runs only one block per Exec. Non-Oracle dialects
		// pass straight through to a single Exec, unchanged.
		if err := db.ExecScript(sqlDB, dbConn.System(), up); err != nil {
			return fmt.Errorf("automigrate: failed to execute migration for %s: %w", meta.Name, err)
		}

		a.Logger.Debug("migrated model", "model", meta.Name, "table", meta.Table, "system", dbConn.System())
	}

	return nil
}

// buildAutoMigrateScaffold dispatches to a dialect-specific scaffold
// builder based on the resolved SQL system. Returning ErrAutoMigrate
// for unknown engines preserves the contract previously documented by
// the package-level comment on `AutoMigrate`, so callers can
// `errors.Is` against the same sentinel.
func buildAutoMigrateScaffold(system string, meta *model.ModelMeta) (string, error) {
	switch system {
	case "sqlite":
		up, _, err := model.BuildSQLiteMigrationScaffold(meta)
		if err != nil {
			return "", fmt.Errorf("failed to build sqlite scaffold for %s: %w", meta.Name, err)
		}
		return up, nil
	case "postgresql":
		up, _, err := model.BuildPostgresMigrationScaffold(meta)
		if err != nil {
			return "", fmt.Errorf("failed to build postgres scaffold for %s: %w", meta.Name, err)
		}
		return up, nil
	case "mysql":
		up, _, err := model.BuildMySQLMigrationScaffold(meta)
		if err != nil {
			return "", fmt.Errorf("failed to build mysql scaffold for %s: %w", meta.Name, err)
		}
		return up, nil
	case "mssql":
		up, _, err := model.BuildMSSQLMigrationScaffold(meta)
		if err != nil {
			return "", fmt.Errorf("failed to build mssql scaffold for %s: %w", meta.Name, err)
		}
		return up, nil
	case "oracle":
		up, _, err := model.BuildOracleMigrationScaffold(meta)
		if err != nil {
			return "", fmt.Errorf("failed to build oracle scaffold for %s: %w", meta.Name, err)
		}
		return up, nil
	default:
		// Unknown engine — AutoMigrate has no dialect-aware builder.
		return "", fmt.Errorf("%w (system=%q)", db.ErrAutoMigrate, system)
	}
}

// DefaultDB returns the primary database connection.
func (a *App) DefaultDB() *sql.DB {
	if a.DB == nil {
		return nil
	}
	sdb, _ := a.DB.SqlDB()
	return sdb
}

// New creates an application container with default wiring.
//
// When called without options, New initializes all subsystems (storage,
// mail, authz) — identical to pre-extension behavior.
//
// Use WithoutDefaults() for a lightweight core-only app:
//
//	a, err := app.New(cfg, app.WithoutDefaults())
//
// Use WithExtensions() to selectively add subsystems:
//
//	a, err := app.New(cfg,
//	    app.WithoutDefaults(),
//	    app.WithExtensions(myExtension()),
//	)
func New(cfg *Config, opts ...Option) (*App, error) {
	if cfg == nil {
		return nil, wrapOp("New", ErrNilConfig)
	}

	effective := mergeDefaults(cfg)
	if err := validateMultiTenantIsolation(effective); err != nil {
		return nil, wrapOp("New validate multitenant", err)
	}

	// Process options.
	var o appOptions
	for _, opt := range opts {
		opt(&o)
	}

	// Secret redaction is on by default (ADR-007). log_redact_extra_keys
	// extends the built-in denylist with app-specific sensitive fields;
	// there is deliberately no config key to disable redaction.
	logger := observe.NewLoggerWithRedaction(effective.LogLevel, effective.LogFormat, observe.RedactionConfig{
		ExtraKeys: effective.LogRedactExtraKeys,
	})

	telemetryShutdown, metricsHandler, err := observe.SetupOpenTelemetry(context.Background(), observe.TelemetryConfig{
		ServiceName:       "nucleus-app",
		OTLPEndpoint:      effective.OTLPEndpoint,
		PrometheusEnabled: strings.TrimSpace(effective.MetricsPath) != "",
	}, logger)
	if err != nil {
		return nil, wrapOp("New telemetry", err)
	}

	// Driver-level SQL instrumentation (opt-in, sql_driver_instrumentation).
	// Databases are opened before the observability SQL observer exists, so
	// the driver-level StatementObserver forwards through this late-bound
	// atomic sink, which is populated once the observer is constructed below.
	// Off by default → stmtObserver stays nil → db.New does not wrap the
	// driver at all.
	var driverSQLSink atomic.Pointer[model.SQLQueryObserver]
	var stmtObserver db.StatementObserver
	if effective.SQLDriverInstrumentation {
		stmtObserver = func(ctx context.Context, info db.StatementInfo) {
			obs := driverSQLSink.Load()
			if obs == nil {
				return
			}
			// Bridge to the same model-layer SQL observer CRUD feeds, so
			// direct statements reuse its sanitize + correlation + emit
			// (and its HasSubscribers gate — the expensive work runs only
			// when something is watching). No ModelName: the driver layer
			// cannot know it.
			(*obs)(ctx, model.SQLQueryEvent{
				Operation:    info.Operation,
				Query:        info.Query,
				Args:         info.Args,
				Duration:     info.Duration,
				Error:        info.Err,
				RowsAffected: info.RowsAffected,
			})
		}
	}

	defaultAlias, dbs, err := openDatabases(effective, logger, stmtObserver)
	if err != nil {
		_ = telemetryShutdown(context.Background())
		return nil, wrapOp("New db", err)
	}
	dbConn := dbs[defaultAlias]
	if dbConn == nil {
		_ = closeDatabases(dbs)
		_ = telemetryShutdown(context.Background())
		return nil, wrapOp("New db", fmt.Errorf("database alias %q not initialized", defaultAlias))
	}

	sessionManager, sessionStoreShutdown, err := buildSessionManager(effective, dbConn)
	if err != nil {
		_ = closeDatabases(dbs)
		_ = telemetryShutdown(context.Background())
		return nil, wrapOp("New session", err)
	}

	routerOpts := []router.Option{
		router.WithTimeout(toTimeoutSeconds(effective.ReadTimeout)),
		// Emit HSTS unconditionally in production (typically behind a
		// TLS-terminating proxy, where r.TLS is nil); over a direct TLS
		// connection the middleware emits it regardless. Off in development
		// so plain-HTTP local runs are not pinned to HTTPS (H-N5).
		router.WithHSTS(effective.IsProd()),
		// Honor X-Forwarded-For / X-Real-IP only from these upstream proxies;
		// empty (the default) ignores forwarding headers and uses the immediate
		// peer as the client IP, preventing spoofed rate-limit evasion (H-N3).
		router.WithTrustedProxies(effective.TrustedProxies...),
	}
	if effective.RateLimitRequests > 0 {
		routerOpts = append(routerOpts, router.WithRateLimitPolicy(router.RateLimitPolicy{
			Requests: effective.RateLimitRequests,
			Window:   effective.RateLimitWindow,
			Burst:    effective.RateLimitBurst,
			ByRoute:  effective.RateLimitByRoute,
			ByRole:   effective.RateLimitByRole,
		}))
	}
	// CORS (ADR-013 R4, completed at v1.0.0 via DEP-2026-007, + SEC-1): an
	// empty cors_origins (the default) DENIES cross-origin requests — no CORS
	// headers are emitted. A non-empty list restricts the response to exactly
	// those origins; the historical allow-all is the explicit opt-in
	// `cors_origins: ["*"]`. Credentials are OFF by default (SEC-1) and are
	// emitted only when an explicit allow-list is set AND
	// cors_allow_credentials is true — reflecting every Origin with
	// credentials would let any site read authenticated cross-origin
	// responses. cors_allow_credentials without cors_origins is a
	// misconfiguration (warned below), never silently widened.
	if len(effective.CORSOrigins) > 0 {
		routerOpts = append(routerOpts,
			router.WithCORSOrigins(effective.CORSOrigins...),
			router.WithCORSCredentials(effective.CORSAllowCredentials),
		)
	} else if effective.CORSAllowCredentials {
		logger.Warn("cors_allow_credentials set without cors_origins (SEC-1); cross-origin requests are denied by default and credentials are NOT emitted",
			"remedy", "set an explicit cors_origins allow-list to enable credentialed CORS")
	}
	// CSRF is opt-in via `csrf_enabled` (the mvc scaffold turns it on):
	// origin verification via Sec-Fetch-Site with the double-submit token
	// as fallback. Bearer-only subtrees are excluded with
	// `csrf_exempt_paths` — a token API authenticating by header is not
	// CSRF-forgeable, and exempting it keeps non-browser clients working.
	if effective.CSRFEnabled {
		routerOpts = append(routerOpts, router.WithCSRF(effective.CSRFExemptPaths...))
		if effective.CSRFInsecureCookie {
			routerOpts = append(routerOpts, router.WithCSRFInsecureCookie(true))
		}
	}
	r := router.New(logger, routerOpts...)
	// QCD-FW-8: hand the session manager to the router tree, so the Context
	// session helpers (SessionPutString & co.) work in handlers — including
	// inside Route/Group/With children, which inherit it at creation. The
	// manager existed and its middleware was mounted, but nothing ever
	// called SetSessionManager, so c.session stayed nil everywhere.
	r.SetSessionManager(sessionManager)
	scopeResolver := newRequestScopeResolver(effective)
	r.Use(scopeResolver.Middleware())
	r.Use(sessionManager.Middleware())
	sessionRuntimeIdentity := auth.DetectSessionRuntimeIdentity()
	r.Use(auth.RuntimeMetadataMiddleware(sessionManager, sessionRuntimeIdentity, 30*time.Second))

	// --- Observability bus ---
	//
	// The bus is constructed unconditionally and is always non-nil. Hooks
	// gate event construction on HasSubscribers, so when nobody is
	// subscribed the cost is one atomic load per request. Subscribers (such
	// as the orbit admin module) attach to the same bus directly.
	observBus := observability.NewBus(logger)
	nodeIDForObserv := strings.TrimSpace(sessionRuntimeIdentity.Instance)
	if nodeIDForObserv == "" {
		nodeIDForObserv = strings.TrimSpace(sessionRuntimeIdentity.Host)
	}
	r.Use(hooks.NewHTTPMiddleware(hooks.HTTPMiddlewareConfig{
		Bus:    observBus,
		NodeID: nodeIDForObserv,
	}))
	// Process-wide default SQL observer. Feeds the observability bus, which
	// carries every model.CRUD query across the application — so any
	// subscriber (such as the orbit admin module's live SQL view) sees the
	// whole application's query stream (ADR-018).
	sqlObserver := hooks.NewSQLObserver(hooks.SQLObserverConfig{
		Bus:    observBus,
		NodeID: nodeIDForObserv,
	})
	model.SetDefaultSQLObserver(sqlObserver)
	// Late-bind the same observer into the driver-level sink so direct
	// (non-CRUD) statements reach the feed too (ADR-021). Populated only when
	// the opt-in is on; before this point stmtObserver's sink is nil and
	// early-setup queries (none run yet) would be no-ops.
	if effective.SQLDriverInstrumentation {
		driverSQLSink.Store(&sqlObserver)
	}
	sessionRecorder := hooks.NewSessionRecorder(hooks.SessionRecorderConfig{
		Bus:    observBus,
		NodeID: nodeIDForObserv,
	})

	a := &App{
		Config:               effective,
		Logger:               logger,
		Router:               r,
		DB:                   dbConn,
		DBs:                  dbs,
		Session:              sessionManager,
		Models:               model.NewRegistry(),
		Observability:        observBus,
		SessionRecorder:      sessionRecorder,
		databaseDefaultAlias: defaultAlias,
		scopeResolver:        scopeResolver,
		openAPIRoutes:        make(map[string]struct{}),
		openAuthz:            o.openAuthz,
	}

	// Initialize template engine if configured. Only parse when at least one
	// template actually exists: template.ParseGlob errors ("pattern matches
	// no files") on a present-but-empty dir, and the previous template.Must
	// panicked app startup on it — a real bug for any app whose TemplatesDir
	// exists but has no .html (e.g. a freshly scaffolded skeleton, where
	// TemplatesDir defaults to internal/web/templates). A genuine parse error
	// is surfaced rather than panicked.
	// Templates (QCD-FW-7/9): recursive load from templates_dir on top of an
	// optional prebuilt base (WithTemplates), with registered functions
	// (WithTemplateFuncs) applied BEFORE any parse. Naming rule (render
	// guide): each file registers under its path relative to templates_dir
	// with forward slashes; root files keep their flat name; {{define}}
	// blocks keep their declared names.
	{
		templatesDirExists := false
		if effective.TemplatesDir != "" {
			if _, err := os.Stat(effective.TemplatesDir); err == nil {
				templatesDirExists = true
			}
		}
		// Load order fixes the collision rule (last parse wins): the
		// WithTemplates base first, then every WithTemplatesFS source in
		// registration order, then templates_dir — so the host's on-disk
		// files always override an embedded source's. Functions apply once,
		// before any parse.
		root := o.templateBase
		if root == nil {
			root = template.New("")
		}
		if len(o.templateFuncs) > 0 {
			root = root.Funcs(o.templateFuncs)
		}
		fsCount := 0
		for _, src := range o.templateFS {
			var n int
			var err error
			root, n, err = loadTemplatesFromFS(src.fsys, src.prefix, root)
			if err != nil {
				return nil, wrapOp("New templates", err)
			}
			fsCount += n
		}
		count := 0
		if templatesDirExists {
			var err error
			root, count, err = loadTemplatesRecursive(effective.TemplatesDir, root, nil)
			if err != nil {
				return nil, wrapOp("New templates", err)
			}
		}
		baseHasTemplates := o.templateBase != nil && len(o.templateBase.Templates()) > 0
		switch {
		case count > 0 || fsCount > 0 || baseHasTemplates:
			a.Templates = root
			a.Router.SetHTMLTemplates(a.Templates)
			a.Logger.Info("templates loaded",
				"dir", effective.TemplatesDir, "count", count, "fs_count", fsCount)
		case templatesDirExists:
			// Loud, not silent (QCD-FW-7): the dir is configured and present
			// but holds no .html — any c.HTML will fail. Say so at startup
			// instead of at the first request.
			a.Logger.Warn("templates_dir exists but contains no .html templates — the template engine is NOT configured and c.HTML will return an error",
				"dir", effective.TemplatesDir)
		}
	}

	// Register the core /healthz handler. Wired here so it is available
	// regardless of whether default subsystems are attached (i.e. it works
	// under app.WithoutDefaults()). The handler reads a.DBs and any future
	// state lazily on each request, so subsystems attached after this point
	// still surface through the probe.
	a.Router.Get("/healthz", a.handleHealthz)

	// Mount the Prometheus /metrics endpoint when telemetry returned a
	// non-nil handler (i.e. the operator opted in via Config.MetricsPath).
	// The handler streams OTel SDK metrics in OpenMetrics format; the
	// MeterProvider also continues to feed any configured OTLP exporter,
	// so OTel push and Prometheus pull can coexist.
	if metricsHandler != nil {
		metricsPath := strings.TrimSpace(effective.MetricsPath)
		if metricsPath != "" {
			a.Router.Get(metricsPath, router.FromHTTP(metricsHandler.ServeHTTP))
		}
	}

	// Build the JWT manager from config and mount the JWKS handler when
	// at least one asymmetric (RS256+) key is configured. The bootstrap
	// allow-list (ADR-004) reserves /.well-known/jwks.json for this
	// route so the framework default-deny middleware does not gate it.
	//
	// buildJWTManager returns (nil, nil) when neither `jwt_keys` nor
	// `jwt_secret` is configured. We do NOT default to a phantom
	// manager — an empty HMAC secret would forge globally-known
	// signatures. App.JWT stays nil and consumers must surface a clear
	// error when they need it.
	jwtMgr, err := buildJWTManager(context.Background(), effective)
	if err != nil {
		return nil, wrapOp("New jwt", err)
	}
	if jwtMgr != nil {
		a.JWT = jwtMgr
		if hasAsymmetricKey(jwtMgr) {
			a.Router.Get(
				"/.well-known/jwks.json",
				router.FromHTTP(jwtMgr.JWKSHandler().ServeHTTP),
			)
		}
	} else {
		logger.Warn(
			"jwt: no signing material configured (jwt_keys empty and jwt_secret unset); " +
				"App.JWT is nil — set jwt_secret or jwt_keys[] before issuing tokens. " +
				"This is safe for read-only services that consume JWTs minted by an external IdP.",
		)
	}

	// DB close should always happen on app shutdown.
	a.OnShutdown(func(context.Context) error {
		return closeDatabases(a.DBs)
	})
	a.OnShutdown(func(ctx context.Context) error {
		return telemetryShutdown(ctx)
	})
	if sessionStoreShutdown != nil {
		a.OnShutdown(sessionStoreShutdown)
	}

	// When no options are provided or WithoutDefaults is not set,
	// initialize all default subsystems for full backward compatibility.
	if !o.skipDefaults {
		if err := attachDefaultSubsystems(a, effective); err != nil {
			_ = a.Shutdown(context.Background())
			return nil, err
		}
	}

	// Initialize outbox if enabled in configuration
	if effective.Outbox.Enabled {
		if err := attachOutbox(a, effective, dbConn); err != nil {
			_ = a.Shutdown(context.Background())
			return nil, wrapOp("New outbox", err)
		}
	}

	// Attach user-provided extensions.
	for _, ext := range o.extensions {
		if err := ext.Attach(a); err != nil {
			_ = a.Shutdown(context.Background())
			return nil, wrapOp("New extension "+ext.Name(), err)
		}
		a.extensions = append(a.extensions, ext)
		a.OnShutdown(ext.Shutdown)
	}

	// QCD-FW-10: start the outbox dispatcher only AFTER every extension has
	// attached — Attach is the supported way to register bridges, and the
	// dispatcher's first pass is immediate. Starting earlier opened a window
	// where a durable pending message was leased with an empty route
	// registry and failed ("no bridge route matched"), consuming a retry.
	// Durability semantics are unchanged (missing_route_policy default is
	// still "error"); only the start point moved.
	if effective.Outbox.Enabled && a.Outbox != nil {
		if err := a.Outbox.Start(context.Background()); err != nil {
			_ = a.Shutdown(context.Background())
			return nil, wrapOp("New outbox start", err)
		}
		a.OnShutdown(func(ctx context.Context) error {
			return a.Outbox.Stop(ctx)
		})
	}

	if err := a.buildAuthChain(o, effective); err != nil {
		return nil, err
	}

	return a, nil
}

// attachOutbox initializes the outbox when enabled in configuration.
func attachOutbox(a *App, cfg *Config, dbConn *db.DB) error {
	sqlDB, err := dbConn.SqlDB()
	if err != nil {
		return fmt.Errorf("outbox: get sql db: %w", err)
	}

	flavor, err := outboxFlavorForConfig(cfg)
	if err != nil {
		return err
	}

	// QCD-FW-5: the lease owner identifies THIS instance. Every process used
	// to share the literal "nucleus-app", which made lease rows untraceable
	// and let any co-tenant process lease (and fail) messages another
	// instance could deliver.
	leaseOwner := strings.TrimSpace(cfg.Outbox.LeaseOwner)
	if leaseOwner == "" {
		leaseOwner = defaultOutboxLeaseOwner()
	}

	var missingRoutePolicy outbox.MissingRoutePolicy
	switch strings.ToLower(strings.TrimSpace(cfg.Outbox.MissingRoutePolicy)) {
	case "", "error":
		missingRoutePolicy = outbox.MissingRouteError
	case "ignore":
		missingRoutePolicy = outbox.MissingRouteIgnore
	default:
		return fmt.Errorf("outbox: invalid missing_route_policy %q (expected \"error\" or \"ignore\")", cfg.Outbox.MissingRoutePolicy)
	}

	managedOutbox, err := outbox.NewManagedOutbox(outbox.ManagedConfig{
		DB:                 sqlDB,
		TableName:          cfg.Outbox.TableName,
		Flavor:             flavor,
		LeaseOwner:         leaseOwner,
		LeaseDuration:      cfg.Outbox.LeaseDuration,
		PollInterval:       time.Second,
		BatchSize:          10,
		MaxAttempts:        cfg.Outbox.MaxRetries,
		BaseDelay:          cfg.Outbox.RetryBackoff,
		MaxDelay:           time.Minute,
		Logger:             a.Logger,
		MissingRoutePolicy: missingRoutePolicy,
	})
	if err != nil {
		return fmt.Errorf("outbox: create managed outbox: %w", err)
	}
	a.Logger.Info("outbox dispatcher configured", "lease_owner", leaseOwner, "missing_route_policy", string(missingRoutePolicy))

	// Configure bridges from configuration
	for _, bridgeCfg := range cfg.Outbox.Bridges {
		switch strings.ToLower(bridgeCfg.Type) {
		case "webhook":
			webhookCfg := outbox.WebhookConfig{
				Name:            bridgeCfg.Name,
				URL:             getConfigString(bridgeCfg.Config, "url"),
				Headers:         getConfigStringMap(bridgeCfg.Config, "headers"),
				Secret:          getConfigString(bridgeCfg.Config, "secret"),
				PayloadEncoding: getConfigString(bridgeCfg.Config, "payload_encoding"),
			}
			// Mirror of the module-webhook boot WARN: delivering to an
			// endpoint without body signing is legitimate only when the
			// consumer authenticates the caller by other means, and that
			// choice should be visible in the boot log — once per bridge.
			if webhookCfg.Secret == "" {
				a.Logger.Warn("outbox: webhook bridge configured without a signing secret; its consumer must authenticate deliveries itself",
					"bridge", bridgeCfg.Name)
			}
			bridge, err := outbox.NewWebhookBridge(webhookCfg)
			if err != nil {
				return fmt.Errorf("outbox: create webhook bridge %q: %w", bridgeCfg.Name, err)
			}
			if err := managedOutbox.RegisterBridge(bridge); err != nil {
				return fmt.Errorf("outbox: register webhook bridge %q: %w", bridgeCfg.Name, err)
			}
			// Add default route for all topics if no pattern specified
			pattern := getConfigString(bridgeCfg.Config, "pattern")
			if pattern == "" {
				pattern = "*"
			}
			managedOutbox.AddRoute(pattern, bridgeCfg.Name)
		case "kafka":
			return fmt.Errorf("outbox: kafka bridge %q is experimental and disabled; configure webhook bridges or implement a real Kafka bridge before enabling this route", bridgeCfg.Name)
		default:
			a.Logger.Warn("outbox: unknown bridge type", "type", bridgeCfg.Type, "name", bridgeCfg.Name)
		}
	}

	a.Outbox = managedOutbox

	// QCD-FW-10: the dispatcher is NOT started here. Its Run performs an
	// immediate initial dispatch pass, and Extension.Attach — which runs
	// after this function — is the supported way to register bridges: a
	// message already durable in the table would be leased with an empty
	// route registry and fail, consuming a retry. New starts the dispatcher
	// after the extensions loop.

	a.Logger.Info("outbox initialized", "table", cfg.Outbox.TableName, "bridges", len(cfg.Outbox.Bridges))
	return nil
}

func outboxFlavorForConfig(cfg *Config) (outbox.Flavor, error) {
	if cfg == nil {
		return outbox.FlavorSQLite, nil
	}
	dbCfg, ok := cfg.DatabaseByAlias(cfg.DefaultDatabaseAlias())
	if !ok {
		return outbox.FlavorSQLite, nil
	}
	return outboxFlavorForDatabaseURL(dbCfg.URL)
}

// outboxFlavorForDatabaseURL maps the default database URL to the outbox
// flavor. It must not paper over unsupported dialects: mapping mssql or
// oracle to sqlite here would hand outbox.NewStore an explicit (valid)
// flavor and bypass its construction-time fail-fast (NU6-3), so those
// URLs return an error instead.
func outboxFlavorForDatabaseURL(raw string) (outbox.Flavor, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(normalized, "postgres://"), strings.HasPrefix(normalized, "postgresql://"):
		return outbox.FlavorPostgres, nil
	case strings.HasPrefix(normalized, "mysql://"):
		return outbox.FlavorMySQL, nil
	case strings.HasPrefix(normalized, "mssql://"), strings.HasPrefix(normalized, "sqlserver://"):
		return "", fmt.Errorf("outbox: store supports sqlite/postgres/mysql; got mssql")
	case strings.HasPrefix(normalized, "oracle://"):
		return "", fmt.Errorf("outbox: store supports sqlite/postgres/mysql; got oracle")
	default:
		return outbox.FlavorSQLite, nil
	}
}

func getConfigString(cfg map[string]interface{}, key string) string {
	if cfg == nil {
		return ""
	}
	val, ok := cfg[key]
	if !ok {
		return ""
	}
	if str, ok := val.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", val)
}

func getConfigStringMap(cfg map[string]interface{}, key string) map[string]string {
	if cfg == nil {
		return nil
	}
	val, ok := cfg[key]
	if !ok {
		return nil
	}
	if m, ok := val.(map[string]interface{}); ok {
		result := make(map[string]string)
		for k, v := range m {
			if str, ok := v.(string); ok {
				result[k] = str
			} else {
				result[k] = fmt.Sprintf("%v", v)
			}
		}
		return result
	}
	return nil
}

func getConfigStringSlice(cfg map[string]interface{}, key string) []string {
	if cfg == nil {
		return nil
	}
	val, ok := cfg[key]
	if !ok {
		return nil
	}
	if slice, ok := val.([]interface{}); ok {
		result := make([]string, 0, len(slice))
		for _, v := range slice {
			if str, ok := v.(string); ok {
				result = append(result, str)
			} else {
				result = append(result, fmt.Sprintf("%v", v))
			}
		}
		return result
	}
	return nil
}

// attachDefaultSubsystems initializes mail, storage, and authz when
// app.New is called without WithoutDefaults(). This preserves full backward
// compatibility with existing code.
func attachDefaultSubsystems(
	a *App,
	effective *Config,
) error {
	// --- Mail ---
	mailer, err := mail.NewSender(mail.Config{
		Driver:   effective.MailDriver,
		Timeout:  effective.WriteTimeout,
		SMTPHost: effective.SMTPHost,
		SMTPPort: effective.SMTPPort,
		SMTPUser: effective.SMTPUser,
		SMTPPass: effective.SMTPPass,
		CircuitBreaker: mail.CircuitBreakerConfig{
			Enabled:               effective.MailCircuitBreaker.Enabled,
			FailureThreshold:      effective.MailCircuitBreaker.FailureThreshold,
			Cooldown:              effective.MailCircuitBreaker.Cooldown,
			HalfOpenMaxConcurrent: effective.MailCircuitBreaker.HalfOpenMaxConcurrent,
		},
	})
	if err != nil {
		return wrapOp("New mail", err)
	}
	a.Mailer = mailer
	if effective.MailCircuitBreaker.Enabled {
		// Match mail.NewSender's normalisation: empty driver maps to
		// "noop", which is never wrapped, so the log line is silent
		// for both forms.
		normalizedDriver := strings.ToLower(strings.TrimSpace(effective.MailDriver))
		if normalizedDriver == "" {
			normalizedDriver = "noop"
		}
		if normalizedDriver != "noop" {
			a.Logger.Info(
				"mail circuit breaker enabled",
				"driver", normalizedDriver,
				"failure_threshold", effective.MailCircuitBreaker.FailureThreshold,
				"cooldown", effective.MailCircuitBreaker.Cooldown,
			)
		}
	}

	// --- RBAC ---
	//
	// ADR-004: construct the enforcer unconditionally, seed the framework-
	// owned bootstrap allow-list, and (unless WithOpenAuthz was passed)
	// mount the default-deny middleware on the router.
	rbacPath, rbacPathErr := rbacPolicyPath(effective)
	if rbacPathErr != nil {
		// DX-2 corollary: an explicit rbac_policy_file that does not exist
		// used to boot the app into total default-deny with a WARN telling
		// the operator to set the key they had already set. A filename typo
		// fails startup naming the path instead.
		return wrapOp("New RBAC policy file", rbacPathErr)
	}
	rbacEnforcer, err := authz.New(a.Logger, rbacPath)
	if err != nil {
		return wrapOp("New RBAC enforcer", err)
	}
	// `metrics_public: false` keeps the Prometheus endpoint out of the
	// anonymous bootstrap allow-list, so it falls under default-deny and
	// requires an explicit policy grant (or WithOpenAuthz). Default true —
	// the historical scrape-friendly posture, documented in Config.
	var seedSkip []string
	if !effective.MetricsPublic {
		seedSkip = append(seedSkip, "/metrics")
	}
	if err := rbacEnforcer.SeedBootstrapAllowListExcluding(seedSkip...); err != nil {
		return wrapOp("New RBAC bootstrap allow-list", err)
	}
	a.Authorizer = rbacEnforcer

	if rbacPath == "" {
		a.Logger.Warn(
			"authz: no user policies loaded; only bootstrap routes will respond — " +
				"set rbac_policy_file or call App.Authorizer.AddPolicy programmatically, " +
				"or pass app.WithOpenAuthz() to skip enforcement entirely (see ADR-004)",
		)
	} else {
		a.Logger.Info("RBAC enforcer initialized", "policy_path", rbacPath)
	}

	if a.openAuthz {
		a.Logger.Warn(
			"authz: WithOpenAuthz() in effect — no authorization checks will run on user routes. " +
				"This is unsafe outside development (see ADR-004).",
		)
	} else {
		// Decode the bearer BEFORE global enforcement (QCD-FW-1): without
		// this, no middleware populated claims ahead of the default-deny
		// layer, every subject resolved to `anonymous`, and the role-based
		// policies AUTH_GUIDE documents were unreachable globally. Optional:
		// requests without (or with invalid) tokens proceed claimless and
		// still resolve to `anonymous`.
		if a.JWT != nil {
			a.Router.Use(a.JWT.OptionalJWTMiddleware())
		}
		a.Router.Use(buildDefaultAuthzMiddleware(rbacEnforcer, a.Logger))
	}

	// --- Storage ---
	storCfg := effective.toStorageConfig()
	baseStore, err := storage.New(storCfg, a.Logger)
	if err != nil {
		return wrapOp("New storage", err)
	}
	store := storage.NewWithTenant(baseStore, func(ctx context.Context) string {
		scope, ok := RequestScopeFromContext(ctx)
		if !ok || scope.Tenant == "" {
			return ""
		}
		return scope.Tenant
	})
	cleaner, err := storage.NewCleaner(baseStore, storCfg.Cleanup, a.Logger)
	if err == nil && cleaner != nil {
		cleaner.Start()
	}
	publicMapper := storage.NewPublicMapperForConfig(baseStore, storCfg)
	a.Storage = store
	a.storageCleaner = cleaner

	if publicMapper != nil {
		publicMapper.MountAll(a.Router)
	}

	a.OnShutdown(func(ctx context.Context) error {
		if cleaner != nil {
			cleaner.Stop()
		}
		return baseStore.Close()
	})

	return nil
}

// RegisterModel registers a model in the shared model registry.
func (a *App) RegisterModel(m interface{}, cfg ...model.ModelConfig) error {
	if a == nil {
		return wrapOp("RegisterModel", ErrNilApp)
	}
	if a.Models == nil {
		return wrapOp("RegisterModel", ErrModelsRegistryNotInitialized)
	}
	return a.Models.Register(m, cfg...)
}

// MountOpenAPIHandler mounts a JSON OpenAPI document endpoint exactly once
// per path, served by any stdlib http.Handler — typically
// `openapi.Handler(provider)` for a generated document factory. This is the
// stdlib-first replacement for MountOpenAPI (DEP-2026-008).
func (a *App) MountOpenAPIHandler(pattern string, handler http.Handler) error {
	if a == nil {
		return wrapOp("MountOpenAPIHandler", ErrNilApp)
	}
	if a.Router == nil {
		return wrapOp("MountOpenAPIHandler", ErrNotInitialized)
	}
	if handler == nil {
		return wrapOp("MountOpenAPIHandler", errors.New("openapi handler is nil"))
	}

	path := strings.TrimSpace(pattern)
	if path == "" {
		path = "/openapi.json"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.openAPIRoutes == nil {
		a.openAPIRoutes = map[string]struct{}{}
	}
	if _, ok := a.openAPIRoutes[path]; ok {
		return nil
	}

	a.Router.Get(path, router.FromHTTP(handler.ServeHTTP))
	a.openAPIRoutes[path] = struct{}{}
	return nil
}

// OnShutdown registers a callback executed during shutdown in reverse order.
func (a *App) OnShutdown(fn func(context.Context) error) {
	if a == nil || fn == nil {
		return
	}
	a.mu.Lock()
	a.shutdownFns = append(a.shutdownFns, fn)
	a.mu.Unlock()
}

// warnUnknownDBTags emits one WARN per registered-model field whose `db:` tag
// carries directives parseDBTag does not recognize. Unknown directives are
// deliberately not an error (they were always ignored, and failing would break
// running apps); the WARN makes the gap visible at startup instead of leaving
// the developer trusting a constraint that does not exist.
func warnUnknownDBTags(a *App) {
	if a == nil || a.Models == nil || a.Logger == nil {
		return
	}
	for _, meta := range a.Models.All() {
		for _, f := range meta.Fields {
			if len(f.UnknownDBTokens) == 0 {
				continue
			}
			a.Logger.Warn(
				"model field has unrecognized db tag directives; they have no effect (supported: column:<name>, pk, fk:<table.column>, fk:<k=v,…>, index[:name], unique[:name], not null, required, readonly, tenant, or \"-\" to exclude the field)",
				"model", meta.Name, "field", f.Name, "unrecognized", strings.Join(f.UnknownDBTokens, ", "),
			)
		}
	}
}

// Run starts the HTTP server and blocks until context cancellation or SIGINT/SIGTERM.
func (a *App) Run(ctx context.Context) error {
	if a == nil {
		return wrapOp("Run", ErrNilApp)
	}
	if a.Config == nil || a.Router == nil {
		return wrapOp("Run", ErrNotInitialized)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Boot-time diagnostics: a `db:` tag directive the parser does not
	// recognize is applied as... nothing. Silently. The developer believes a
	// constraint exists (an fk, an exclusion) that was never created — that
	// is how a documented-but-phantom tag syntax went unnoticed through four
	// audits. Same "loud, not fatal" channel as the module-readiness WARNs.
	warnUnknownDBTags(a)

	srv := &http.Server{
		Addr:         a.Config.Addr(),
		Handler:      a.Router,
		ReadTimeout:  a.Config.ReadTimeout,
		WriteTimeout: a.Config.WriteTimeout,
		IdleTimeout:  a.Config.IdleTimeout,
	}

	a.mu.Lock()
	if a.server != nil {
		a.mu.Unlock()
		return wrapOp("Run", ErrServerAlreadyRunning)
	}
	a.server = srv
	a.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		var err error
		if a.Config.TLSCertFile != "" && a.Config.TLSKeyFile != "" {
			err = srv.ListenAndServeTLS(a.Config.TLSCertFile, a.Config.TLSKeyFile)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := withTimeoutFromConfig(a.Config)
		defer cancel()
		return a.Shutdown(shutdownCtx)
	case <-sigCh:
		shutdownCtx, cancel := withTimeoutFromConfig(a.Config)
		defer cancel()
		return a.Shutdown(shutdownCtx)
	case err := <-errCh:
		shutdownCtx, cancel := withTimeoutFromConfig(a.Config)
		defer cancel()
		_ = a.Shutdown(shutdownCtx)
		return wrapOp("Run serve", err)
	}
}

// Shutdown gracefully stops the HTTP server (if started) and runs shutdown hooks.
func (a *App) Shutdown(ctx context.Context) error {
	if a == nil {
		return wrapOp("Shutdown", ErrNilApp)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	a.mu.Lock()
	srv := a.server
	a.server = nil
	hooks := make([]func(context.Context) error, len(a.shutdownFns))
	copy(hooks, a.shutdownFns)
	a.shutdownFns = nil
	a.mu.Unlock()

	var errs []error

	if srv != nil {
		if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, wrapOp("Shutdown server", err))
		}
	}

	for i := len(hooks) - 1; i >= 0; i-- {
		if err := hooks[i](ctx); err != nil {
			errs = append(errs, wrapOp(fmt.Sprintf("Shutdown hook[%d]", i), err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func withTimeoutFromConfig(cfg *Config) (context.Context, context.CancelFunc) {
	if cfg == nil {
		return context.WithTimeout(context.Background(), 10*time.Second)
	}
	timeout := cfg.WriteTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return context.WithTimeout(context.Background(), timeout)
}

func toTimeoutSeconds(d time.Duration) int {
	if d <= 0 {
		return 30
	}
	if d < time.Second {
		return 1
	}
	return int(d.Seconds())
}

func mergeDefaults(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}

	base := defaults()
	merged := *cfg

	if merged.Host == "" {
		merged.Host = base.Host
	}
	if merged.ReadTimeout == 0 {
		merged.ReadTimeout = base.ReadTimeout
	}
	if merged.WriteTimeout == 0 {
		merged.WriteTimeout = base.WriteTimeout
	}
	if merged.IdleTimeout == 0 {
		merged.IdleTimeout = base.IdleTimeout
	}
	if merged.SessionLifetime == 0 {
		merged.SessionLifetime = base.SessionLifetime
	}
	if merged.SessionStore == "" {
		merged.SessionStore = base.SessionStore
	}
	if merged.SessionTable == "" {
		merged.SessionTable = base.SessionTable
	}
	if merged.SessionCookieName == "" {
		merged.SessionCookieName = base.SessionCookieName
	}
	if merged.SessionCookiePath == "" {
		merged.SessionCookiePath = base.SessionCookiePath
	}
	if merged.SessionCookieSameSite == "" {
		merged.SessionCookieSameSite = base.SessionCookieSameSite
	}
	if merged.SessionRedisPrefix == "" {
		merged.SessionRedisPrefix = base.SessionRedisPrefix
	}
	if merged.MailDriver == "" {
		merged.MailDriver = base.MailDriver
	}
	if merged.SMTPPort == 0 {
		merged.SMTPPort = base.SMTPPort
	}
	if merged.MailFrom == "" {
		merged.MailFrom = base.MailFrom
	}
	if merged.LogLevel == "" {
		merged.LogLevel = base.LogLevel
	}
	if merged.LogFormat == "" {
		merged.LogFormat = base.LogFormat
	}
	if len(merged.LogRedactExtraKeys) == 0 {
		merged.LogRedactExtraKeys = base.LogRedactExtraKeys
	}
	if merged.Env == "" {
		merged.Env = base.Env
	}
	if merged.RateLimitWindow == 0 {
		merged.RateLimitWindow = base.RateLimitWindow
	}
	if merged.DatabaseDefault == "" {
		merged.DatabaseDefault = base.DatabaseDefault
	}
	if merged.Databases == nil {
		merged.Databases = map[string]DatabaseConfig{}
	}
	if merged.MultiSite.DefaultSite == "" {
		merged.MultiSite.DefaultSite = base.MultiSite.DefaultSite
	}
	if merged.MultiSite.Sites == nil {
		merged.MultiSite.Sites = map[string]SiteConfig{}
	}
	if merged.MultiTenant.Resolver == "" {
		merged.MultiTenant.Resolver = base.MultiTenant.Resolver
	}
	if merged.MultiTenant.Header == "" {
		merged.MultiTenant.Header = base.MultiTenant.Header
	}
	if merged.MultiTenant.DatabaseAliasTemplate == "" {
		merged.MultiTenant.DatabaseAliasTemplate = base.MultiTenant.DatabaseAliasTemplate
	}
	if !merged.MultiTenant.RequireIsolatedDB {
		merged.MultiTenant.RequireIsolatedDB = base.MultiTenant.RequireIsolatedDB
	}
	if merged.MultiTenant.Tenants == nil {
		merged.MultiTenant.Tenants = map[string]TenantConfig{}
	}
	if merged.JobsProvider == "" {
		merged.JobsProvider = base.JobsProvider
	}
	if merged.JobsConcurrency == 0 {
		merged.JobsConcurrency = base.JobsConcurrency
	}
	if merged.WebhooksPrefix == "" {
		merged.WebhooksPrefix = base.WebhooksPrefix
	}
	normalizeRuntimeConfig(&merged)

	return &merged
}

// DefaultDatabaseAlias returns the active default database alias.
func (a *App) DefaultDatabaseAlias() string {
	if a == nil {
		return "default"
	}
	alias := normalizeAlias(a.databaseDefaultAlias)
	if alias != "" {
		return alias
	}
	if a.Config != nil {
		return a.Config.DefaultDatabaseAlias()
	}
	return "default"
}

// Database resolves a database handle by alias. Empty alias means default.
func (a *App) Database(alias string) (*db.DB, error) {
	if a == nil {
		return nil, wrapOp("Database", ErrNilApp)
	}

	key := normalizeAlias(alias)
	if key == "" {
		key = a.DefaultDatabaseAlias()
	}

	if len(a.DBs) == 0 {
		if a.DB == nil {
			return nil, wrapOp("Database", ErrNotInitialized)
		}
		if key == a.DefaultDatabaseAlias() {
			return a.DB, nil
		}
		return nil, wrapOp("Database", fmt.Errorf("%w: %s", ErrDatabaseAliasNotFound, key))
	}

	handle := a.DBs[key]
	if handle == nil {
		return nil, wrapOp("Database", fmt.Errorf("%w: %s", ErrDatabaseAliasNotFound, key))
	}
	return handle, nil
}

// DatabaseForRequest returns the DB selected for the current request scope.
// If no request scope is available, the default DB alias is used.
func (a *App) DatabaseForRequest(r *http.Request) (*db.DB, error) {
	if a == nil {
		return nil, wrapOp("DatabaseForRequest", ErrNilApp)
	}
	if r == nil {
		return a.Database("")
	}

	scope, ok := RequestScopeFromContext(r.Context())
	if ok {
		if scope.DatabaseAlias == tenantIsolationViolationAlias {
			return nil, wrapOp("DatabaseForRequest", ErrTenantIsolationViolation)
		}
		return a.Database(scope.DatabaseAlias)
	}
	if a.scopeResolver != nil {
		scope = a.scopeResolver.Resolve(r)
		if scope.DatabaseAlias == tenantIsolationViolationAlias {
			return nil, wrapOp("DatabaseForRequest", ErrTenantIsolationViolation)
		}
		return a.Database(scope.DatabaseAlias)
	}
	return a.Database("")
}

func openDatabases(cfg *Config, logger *slog.Logger, stmtObserver db.StatementObserver) (string, map[string]*db.DB, error) {
	if cfg == nil {
		return "", nil, fmt.Errorf("nil config")
	}

	aliases := cfg.DatabaseAliases()
	if len(aliases) == 0 {
		return "", nil, fmt.Errorf("no databases configured")
	}

	dbs := make(map[string]*db.DB, len(aliases))
	for _, alias := range aliases {
		dbCfg, ok := cfg.DatabaseByAlias(alias)
		if !ok {
			continue
		}

		handle, err := db.New(db.Config{
			Engine:              db.EngineSQL,
			DatabaseURL:         dbCfg.URL,
			DatabaseMaxOpen:     dbCfg.MaxOpen,
			DatabaseMaxIdle:     dbCfg.MaxIdle,
			DatabaseMaxLifetime: dbCfg.MaxLifetime,
			StatementObserver:   stmtObserver,
		}, logger)
		if err != nil {
			_ = closeDatabases(dbs)
			return "", nil, fmt.Errorf("open database alias %q: %w", alias, err)
		}
		dbs[alias] = handle
	}

	defaultAlias := cfg.DefaultDatabaseAlias()
	if dbs[defaultAlias] == nil {
		_ = closeDatabases(dbs)
		return "", nil, fmt.Errorf("default database alias %q is not configured", defaultAlias)
	}
	return defaultAlias, dbs, nil
}

func closeDatabases(dbs map[string]*db.DB) error {
	if len(dbs) == 0 {
		return nil
	}

	aliases := make([]string, 0, len(dbs))
	for alias := range dbs {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)

	var errs []error
	for _, alias := range aliases {
		handle := dbs[alias]
		if handle == nil {
			continue
		}
		if err := handle.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close database alias %q: %w", alias, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func cloneDatabaseConfigMap(in map[string]DatabaseConfig) map[string]DatabaseConfig {
	if len(in) == 0 {
		return map[string]DatabaseConfig{}
	}
	out := make(map[string]DatabaseConfig, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneSiteConfigMap(in map[string]SiteConfig) map[string]SiteConfig {
	if len(in) == 0 {
		return map[string]SiteConfig{}
	}
	out := make(map[string]SiteConfig, len(in))
	for k, v := range in {
		hosts := make([]string, len(v.Hosts))
		copy(hosts, v.Hosts)
		v.Hosts = hosts
		out[k] = v
	}
	return out
}

func cloneTenantConfigMap(in map[string]TenantConfig) map[string]TenantConfig {
	if len(in) == 0 {
		return map[string]TenantConfig{}
	}
	out := make(map[string]TenantConfig, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func buildSessionManager(cfg *Config, database *db.DB) (*auth.SessionManager, func(context.Context) error, error) {
	if cfg == nil {
		return nil, nil, fmt.Errorf("nil config")
	}

	// FW-4: SameSite=None requires Secure, or every modern browser silently
	// drops the cookie (the session never sticks). Fail startup loudly here
	// rather than ship a config that looks fine but never logs anyone in.
	// auth.NewSessionManager also self-corrects this combo as defence in
	// depth, but the framework's production posture is to reject it outright
	// so the operator fixes the config instead of running on a coerced value.
	if strings.EqualFold(strings.TrimSpace(cfg.SessionCookieSameSite), "none") && !cfg.SessionCookieSecure {
		return nil, nil, fmt.Errorf(
			"session_cookie_samesite=none requires session_cookie_secure=true " +
				"(browsers reject SameSite=None cookies without the Secure attribute)")
	}

	// Cookie-prefix support (same posture as FW-4 above: reject the
	// misconfiguration at startup, because a prefixed cookie that violates
	// its preconditions is silently dropped by every browser and the
	// session simply never sticks — with no server-side signal at all).
	//   __Host-…   requires Secure, Path=/ and NO Domain attribute.
	//   __Secure-… requires Secure.
	cookieName := strings.TrimSpace(cfg.SessionCookieName)
	if strings.HasPrefix(cookieName, "__Host-") {
		switch {
		case !cfg.SessionCookieSecure:
			return nil, nil, fmt.Errorf("session_cookie_name %q uses the __Host- prefix, which requires session_cookie_secure=true", cookieName)
		case strings.TrimSpace(cfg.SessionCookieDomain) != "":
			return nil, nil, fmt.Errorf("session_cookie_name %q uses the __Host- prefix, which forbids setting session_cookie_domain", cookieName)
		case strings.TrimSpace(cfg.SessionCookiePath) != "" && cfg.SessionCookiePath != "/":
			return nil, nil, fmt.Errorf("session_cookie_name %q uses the __Host- prefix, which requires session_cookie_path=/ (got %q)", cookieName, cfg.SessionCookiePath)
		}
	} else if strings.HasPrefix(cookieName, "__Secure-") && !cfg.SessionCookieSecure {
		return nil, nil, fmt.Errorf("session_cookie_name %q uses the __Secure- prefix, which requires session_cookie_secure=true", cookieName)
	}

	sessionManager := auth.NewSessionManager(auth.SessionConfig{
		Lifetime:    cfg.SessionLifetime,
		IdleTimeout: cfg.SessionIdleTimeout,
		Secure:      cfg.SessionCookieSecure,
		Path:        cfg.SessionCookiePath,
		Domain:      cfg.SessionCookieDomain,
		CookieName:  cfg.SessionCookieName,
		SameSite:    cfg.SessionCookieSameSite,
	})

	store := strings.ToLower(strings.TrimSpace(cfg.SessionStore))
	if store == "" {
		store = "memory"
	}

	// Resolved through the registry rather than a switch, so a session
	// backend this framework has never heard of — DynamoDB, an internal
	// store — is selectable by name without patching it. The built-ins
	// register through the same public call (auth.RegisterSessionStore).
	params := auth.SessionStoreParams{
		TableName: cfg.SessionTable,
		KeyPrefix: cfg.SessionRedisPrefix,
		RedisURL:  strings.TrimSpace(cfg.SessionRedisURL),
	}
	if params.RedisURL == "" {
		params.RedisURL = strings.TrimSpace(cfg.RedisURL)
	}
	if database != nil {
		// A store that does not need SQL must not be denied one because
		// the handle failed to open, so the error is only fatal for the
		// factories that actually ask for DB.
		if sqlDB, err := database.SqlDB(); err == nil {
			params.DB = sqlDB
			params.DatabaseURL = cfg.DefaultDatabase().URL
		}
	}

	backing, shutdown, err := auth.BuildSessionStore(store, params)
	if err != nil {
		return nil, nil, err
	}
	// A nil store means "keep the manager's in-memory default" — that is
	// how the memory backend is expressed, so there is one code path
	// instead of two.
	if backing != nil {
		sessionManager.SetSessionStore(backing)
	}
	return sessionManager, shutdown, nil
}

// resolveRBACPolicyFile returns the configured RBAC policy file path from the
// canonical rbac_policy_file key. The deprecated admin_rbac_policy_file alias
// was removed in v0.12.0 (DEP-2026-004).
func resolveRBACPolicyFile(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.RBACPolicyFile)
}

// rbacPolicyPath returns the RBAC policy file path if it exists. It reads the
// rbac_policy_file key, then probes the default scaffold locations.
func rbacPolicyPath(cfg *Config) (string, error) {
	if cfg == nil {
		return "", nil
	}
	path := resolveRBACPolicyFile(cfg)
	if path == "" {
		// Check default locations. Both the legacy admin_rbac.csv name and the
		// rbac_policy.csv name emitted by the mvc scaffold are probed (R5 /
		// ADR-013) so an app that relies on auto-discovery finds the
		// scaffolded policy without setting rbac_policy_file.
		for _, p := range []string{
			"admin_rbac.csv", "config/admin_rbac.csv", "rbac/admin_rbac.csv",
			"rbac_policy.csv", "config/rbac_policy.csv", "rbac/rbac_policy.csv",
		} {
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
		return "", nil
	}
	// An EXPLICITLY configured path is a promise (same contract as
	// LoadConfig's --config): if it is missing, silently degrading to "no
	// policies loaded" turned a filename typo into production-wide 403s.
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("rbac_policy_file %s does not exist: %w", path, err)
	}
	return path, nil
}

// buildAuthChain assembles the ordered authentication chain from
// auth_backends.
//
// The application's own user table joins the registry here, under the name
// WithUserProvider gave it, so it is nameable in auth_backends alongside a
// directory. Registration is per-process and idempotent from the caller's
// point of view: registering the same name twice is an error in the
// registry (import order would otherwise decide the winner), so a second
// App in the same process reuses what is already there.
func (a *App) buildAuthChain(o appOptions, cfg *Config) error {
	if o.userProvider != nil {
		name := strings.ToLower(strings.TrimSpace(o.userProviderName))
		if name == "" {
			name = "local"
		}
		backend, err := auth.NewUserProviderBackend(name, o.userProvider)
		if err != nil {
			return wrapOp("New auth backend", err)
		}
		// A duplicate registration means this name already exists in the
		// process — a second App, or an application that registered its
		// own. Not an error to the caller: the chain below resolves the
		// name either way, and failing here would make a second App
		// impossible to construct in one process (tests do exactly that).
		_ = auth.RegisterBackend(name, func(auth.BackendConfig) (auth.Backend, error) { return backend, nil })
	}

	if len(cfg.AuthBackends) == 0 {
		// Nothing declared: no chain. An application with no user provider
		// and no directory has nothing to authenticate against, and an
		// empty chain that always rejects would be a worse answer than
		// none at all.
		return nil
	}

	chain, err := auth.NewChainFrom(auth.ChainConfig{
		Backends:       cfg.AuthBackends,
		ProviderConfig: cfg.AuthBackendConfig,
	})
	if err != nil {
		return wrapOp("New auth chain", err)
	}
	a.AuthChain = chain
	a.Logger.Info("nucleus: authentication chain ready (backends are consulted in this order; one that cannot reach its source is skipped, not treated as a rejection)",
		"backends", strings.Join(chain.Names(), " "))
	return nil
}
