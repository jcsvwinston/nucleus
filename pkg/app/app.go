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
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
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
	Outbox     *outbox.ManagedOutbox
	Templates  *template.Template

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
	}
	r := router.New(logger, routerOpts...)
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
	if effective.TemplatesDir != "" {
		if _, err := os.Stat(effective.TemplatesDir); err == nil {
			pattern := filepath.Join(effective.TemplatesDir, "*.html")
			// filepath.Glob errors only on a malformed pattern, which "*.html"
			// never is — a zero-length match (empty dir) is the case we guard.
			if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
				tmpl, err := template.ParseGlob(pattern)
				if err != nil {
					return nil, wrapOp("New templates", err)
				}
				a.Templates = tmpl
				a.Router.SetHTMLTemplates(a.Templates)
			}
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

	managedOutbox, err := outbox.NewManagedOutbox(outbox.ManagedConfig{
		DB:            sqlDB,
		TableName:     cfg.Outbox.TableName,
		Flavor:        flavor,
		LeaseOwner:    "nucleus-app",
		LeaseDuration: cfg.Outbox.LeaseDuration,
		PollInterval:  time.Second,
		BatchSize:     10,
		MaxAttempts:   cfg.Outbox.MaxRetries,
		BaseDelay:     cfg.Outbox.RetryBackoff,
		MaxDelay:      time.Minute,
		Logger:        a.Logger,
	})
	if err != nil {
		return fmt.Errorf("outbox: create managed outbox: %w", err)
	}

	// Configure bridges from configuration
	for _, bridgeCfg := range cfg.Outbox.Bridges {
		switch strings.ToLower(bridgeCfg.Type) {
		case "webhook":
			webhookCfg := outbox.WebhookConfig{
				Name:    bridgeCfg.Name,
				URL:     getConfigString(bridgeCfg.Config, "url"),
				Headers: getConfigStringMap(bridgeCfg.Config, "headers"),
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

	// Start the dispatcher in background
	if err := a.Outbox.Start(context.Background()); err != nil {
		return fmt.Errorf("outbox: start dispatcher: %w", err)
	}

	// Register shutdown hook
	a.OnShutdown(func(ctx context.Context) error {
		return a.Outbox.Stop(ctx)
	})

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
	rbacPath := rbacPolicyPath(effective)
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
		a.Router.Use(buildDefaultAuthzMiddleware(rbacEnforcer))
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

	switch store {
	case "memory":
		return sessionManager, nil, nil
	case "sql":
		if database == nil {
			return nil, nil, fmt.Errorf("session_store=sql requires database")
		}
		sqlDB, err := database.SqlDB()
		if err != nil {
			return nil, nil, fmt.Errorf("session_store=sql open sql db: %w", err)
		}
		sqlStore, err := auth.NewSQLSessionStore(sqlDB, auth.SQLSessionStoreConfig{
			DatabaseURL: cfg.DefaultDatabase().URL,
			TableName:   cfg.SessionTable,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("session_store=sql initialize store: %w", err)
		}
		sessionManager.SetStore(sqlStore)
		return sessionManager, nil, nil
	case "redis":
		redisURL := strings.TrimSpace(cfg.SessionRedisURL)
		if redisURL == "" {
			redisURL = strings.TrimSpace(cfg.RedisURL)
		}
		if redisURL == "" {
			return nil, nil, fmt.Errorf("session_store=redis requires session_redis_url or redis_url")
		}

		redisStore, redisClient, err := auth.NewRedisSessionStoreFromURL(redisURL, cfg.SessionRedisPrefix)
		if err != nil {
			return nil, nil, fmt.Errorf("session_store=redis initialize store: %w", err)
		}
		sessionManager.SetStore(redisStore)

		return sessionManager, func(context.Context) error {
			return redisClient.Close()
		}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported session_store %q (supported: memory, sql, redis)", store)
	}
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
func rbacPolicyPath(cfg *Config) string {
	if cfg == nil {
		return ""
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
				return p
			}
		}
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}
