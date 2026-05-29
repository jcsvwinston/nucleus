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
	"syscall"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/admin"
	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observability"
	"github.com/jcsvwinston/nucleus/pkg/observability/hooks"
	"github.com/jcsvwinston/nucleus/pkg/observe"
	"github.com/jcsvwinston/nucleus/pkg/openapi"
	"github.com/jcsvwinston/nucleus/pkg/outbox"
	"github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// App is the main Nucleus application container. It wires the minimum runtime
// dependencies (config, logger, router, DB, model registry, and admin panel).
//
// By default, app.New(cfg) initializes all subsystems (admin, storage, mail, authz).
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
	Admin      *admin.Panel
	Authorizer *authz.Enforcer
	Storage    storage.Store
	Outbox     *outbox.ManagedOutbox
	Templates  *template.Template

	// Observability is the in-process event bus for HTTP, SQL, session and
	// custom events. It is always non-nil after app.New returns. The
	// embedded admin observability agent (admin/agent, Phase 3) subscribes
	// to it; ad-hoc subscribers can also be attached directly. See
	// pkg/observability for the full ownership model.
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
	adminMounted   bool
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

// RegisterAdminModels registers multiple models with the admin panel using default configurations.
func (a *App) RegisterAdminModels(models ...any) error {
	for _, m := range models {
		if err := a.RegisterModel(m, model.ModelConfig{}); err != nil {
			return fmt.Errorf("register admin models: %w", err)
		}
	}
	return nil
}

// New creates an application container with default wiring.
//
// When called without options, New initializes all subsystems (admin, storage,
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
//	    app.WithExtensions(admin.Extension()),
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

	defaultAlias, dbs, err := openDatabases(effective, logger)
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

	// adminAuthSQLDB is the *sql.DB backing the admin panel's login. It is
	// resolved (and its alias validated) only when default subsystems are
	// active. Under app.WithoutDefaults() a core-only app must not fail
	// startup over an admin_auth_database alias it will never use, so the
	// whole resolution — alias lookup, nil-check, and SqlDB() handle — is
	// gated behind !o.skipDefaults and the variable stays nil otherwise.
	// (FW-3: previously this block ran unconditionally before the gate and
	// rejected a stray admin_auth_database alias even for core-only apps.)
	var adminAuthSQLDB *sql.DB

	// The admin bootstrap user backs the admin panel's login, which is only
	// mounted as a default subsystem; gate it behind !skipDefaults so
	// app.WithoutDefaults() stays free of admin side effects (no privileged
	// user, no one-time stderr password) for core-only apps.
	if !o.skipDefaults {
		adminAuthAlias := normalizeAlias(effective.AdminAuthDatabase)
		if adminAuthAlias == "" {
			adminAuthAlias = defaultAlias
		}
		adminAuthDB := dbs[adminAuthAlias]
		if adminAuthDB == nil {
			_ = closeDatabases(dbs)
			_ = telemetryShutdown(context.Background())
			return nil, wrapOp("New admin auth", fmt.Errorf("admin_auth_database alias %q not initialized", adminAuthAlias))
		}

		adminAuthSQLDB, err = adminAuthDB.SqlDB()
		if err != nil {
			_ = closeDatabases(dbs)
			_ = telemetryShutdown(context.Background())
			return nil, wrapOp("New admin auth sql handle", err)
		}

		bootstrapResult, err := admin.EnsureBootstrapAdminUser(context.Background(), adminAuthSQLDB, admin.BootstrapAdminConfig{
			Username: effective.AdminBootstrapUsername,
			Email:    effective.AdminBootstrapEmail,
			Password: effective.AdminBootstrapPassword,
			System:   adminAuthDB.System(),
		})
		if err != nil {
			_ = closeDatabases(dbs)
			_ = telemetryShutdown(context.Background())
			return nil, wrapOp("New admin bootstrap user", err)
		}
		if bootstrapResult.Created {
			if bootstrapResult.PasswordGenerated {
				// The structured log records that a credential was generated
				// — but never the credential itself. With ADR-007 redaction
				// on, a "password" attr would be [REDACTED] anyway; logging
				// it would be both pointless and a leak risk if redaction
				// were ever disabled. The generated password is written once
				// to stderr instead, deliberately bypassing the logger, so a
				// human running the first boot can capture it. This is the
				// one sanctioned secret-to-stderr path in the framework.
				logger.Warn(
					"admin bootstrap credentials created",
					"database_alias", adminAuthAlias,
					"username", bootstrapResult.Username,
					"note", "a one-time password was written to stderr — capture it now and rotate immediately",
				)
				fmt.Fprintf(os.Stderr,
					"\n=== Nucleus admin bootstrap ===\n"+
						"  username: %s\n"+
						"  password: %s\n"+
						"  This one-time password is shown ONCE, on stderr only. "+
						"Capture it now and rotate it immediately.\n"+
						"===============================\n\n",
					bootstrapResult.Username, bootstrapResult.Password,
				)
			} else {
				logger.Info(
					"admin bootstrap credentials created",
					"database_alias", adminAuthAlias,
					"username", bootstrapResult.Username,
				)
			}
		}
	}

	sessionManager, sessionStoreShutdown, err := buildSessionManager(effective, dbConn)
	if err != nil {
		_ = closeDatabases(dbs)
		_ = telemetryShutdown(context.Background())
		return nil, wrapOp("New session", err)
	}

	routerOpts := []router.Option{
		router.WithTimeout(toTimeoutSeconds(effective.ReadTimeout)),
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
	r := router.New(logger, routerOpts...)
	scopeResolver := newRequestScopeResolver(effective)
	r.Use(scopeResolver.Middleware())
	r.Use(sessionManager.Middleware())
	sessionRuntimeIdentity := auth.DetectSessionRuntimeIdentity()
	// Override host with explicit cluster node ID so multi-node sessions
	// show distinct identities instead of the raw container hostname.
	if explicit := strings.TrimSpace(effective.AdminClusterNodeID); explicit != "" {
		sessionRuntimeIdentity.Host = explicit
		if sessionRuntimeIdentity.Instance == "" {
			sessionRuntimeIdentity.Instance = explicit
		}
		// In Docker (non-K8s) the pod field stays empty; use node ID as pod
		// label so the admin UI shows a meaningful identifier.
		if sessionRuntimeIdentity.Pod == "" {
			sessionRuntimeIdentity.Pod = explicit
		}
	}
	r.Use(auth.RuntimeMetadataMiddleware(sessionManager, sessionRuntimeIdentity, 30*time.Second))
	sessionStoreLabel := strings.ToLower(strings.TrimSpace(effective.SessionStore))
	if sessionStoreLabel == "" {
		sessionStoreLabel = "memory"
	}

	// --- Observability bus (Phase 2 of the admin refactor) ---
	//
	// The bus is constructed unconditionally and is always non-nil. Hooks
	// gate event construction on HasSubscribers, so when nobody is
	// subscribed the cost is one atomic load per request. The embedded
	// admin observability agent (admin/agent, Phase 3) is the primary
	// subscriber in production; tests and custom subscribers use the same
	// bus directly.
	observBus := observability.NewBus(logger)
	nodeIDForObserv := strings.TrimSpace(sessionRuntimeIdentity.Instance)
	if nodeIDForObserv == "" {
		nodeIDForObserv = strings.TrimSpace(sessionRuntimeIdentity.Host)
	}
	r.Use(hooks.NewHTTPMiddleware(hooks.HTTPMiddlewareConfig{
		Bus:          observBus,
		NodeID:       nodeIDForObserv,
		ExcludePaths: append([]string(nil), effective.AdminLiveExcludePatterns...),
	}))
	// Process-wide default SQL observer. Coexists additively with the
	// per-CRUD observer that pkg/admin.Panel installs for its legacy live
	// view (both fire). When pkg/admin's live view is retired in a future
	// phase, this becomes the single SQL feed.
	model.SetDefaultSQLObserver(hooks.NewSQLObserver(hooks.SQLObserverConfig{
		Bus:    observBus,
		NodeID: nodeIDForObserv,
	}))
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
		if err := attachDefaultSubsystems(a, effective, dbs, defaultAlias, adminAuthSQLDB, sessionManager, sessionStoreLabel, sessionRuntimeIdentity); err != nil {
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

	managedOutbox, err := outbox.NewManagedOutbox(outbox.ManagedConfig{
		DB:            sqlDB,
		TableName:     cfg.Outbox.TableName,
		Flavor:        outboxFlavorForConfig(cfg),
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

func outboxFlavorForConfig(cfg *Config) outbox.Flavor {
	if cfg == nil {
		return outbox.FlavorSQLite
	}
	dbCfg, ok := cfg.DatabaseByAlias(cfg.DefaultDatabaseAlias())
	if !ok {
		return outbox.FlavorSQLite
	}
	return outboxFlavorForDatabaseURL(dbCfg.URL)
}

func outboxFlavorForDatabaseURL(raw string) outbox.Flavor {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(normalized, "postgres://"), strings.HasPrefix(normalized, "postgresql://"):
		return outbox.FlavorPostgres
	case strings.HasPrefix(normalized, "mysql://"):
		return outbox.FlavorMySQL
	default:
		return outbox.FlavorSQLite
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

// attachDefaultSubsystems initializes mail, storage, authz, and admin when
// app.New is called without WithoutDefaults(). This preserves full backward
// compatibility with existing code.
func attachDefaultSubsystems(
	a *App,
	effective *Config,
	dbs map[string]*db.DB,
	defaultAlias string,
	adminAuthSQLDB *sql.DB,
	sessionManager *auth.SessionManager,
	sessionStoreLabel string,
	sessionRuntimeIdentity auth.SessionRuntimeIdentity,
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
	// mount the default-deny middleware on the router. The enforcer is
	// also passed to the admin panel below so its existing RBAC paths
	// keep working off the same instance.
	rbacPath := rbacPolicyPath(effective)
	rbacEnforcer, err := authz.New(a.Logger, rbacPath)
	if err != nil {
		return wrapOp("New RBAC enforcer", err)
	}
	if err := rbacEnforcer.SeedBootstrapAllowList(); err != nil {
		return wrapOp("New RBAC bootstrap allow-list", err)
	}
	// The bootstrap allow-list hardcodes "/admin/*" because that is the
	// default prefix; when the operator overrides AdminPrefix the same
	// allow needs to follow. Admin owns its own auth+RBAC flow against
	// the same Enforcer, so the framework default-deny must not double-
	// gate the prefix the admin panel actually mounts at.
	if customPrefix := strings.TrimSpace(effective.AdminPrefix); customPrefix != "" && customPrefix != "/admin" {
		if err := rbacEnforcer.AddPolicy(authz.BootstrapSubject, customPrefix+"/*", "*"); err != nil {
			return wrapOp("New RBAC admin-prefix allow", err)
		}
	}
	a.Authorizer = rbacEnforcer

	if rbacPath == "" {
		a.Logger.Warn(
			"authz: no user policies loaded; only bootstrap routes will respond — " +
				"set admin_rbac_policy_file or call App.Authorizer.AddPolicy programmatically, " +
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

	// --- Admin ---
	adminClusterRedisURL := strings.TrimSpace(effective.AdminClusterRedisURL)
	if adminClusterRedisURL == "" {
		adminClusterRedisURL = strings.TrimSpace(effective.RedisURL)
	}

	adminPanel := admin.NewPanel(a.DB, a.Models, a.Logger, admin.PanelConfig{
		Prefix:              effective.AdminPrefix,
		Title:               effective.AdminTitle,
		Environment:         effective.Env,
		OTLPEndpoint:        strings.TrimSpace(effective.OTLPEndpoint),
		RedisURL:            effective.RedisURL,
		LiveExcludePatterns: append([]string(nil), effective.AdminLiveExcludePatterns...),
		LiveClusterEnabled:  effective.AdminClusterEnabled,
		LiveClusterRedisURL: adminClusterRedisURL,
		LiveClusterChannel:  effective.AdminClusterChannel,
		LiveClusterNodeID:   effective.AdminClusterNodeID,
		LiveClusterToken:    effective.AdminClusterToken,
		TraceURLTemplate:    effective.AdminTraceURLTemplate,
		Databases:           buildAdminDatabaseRuntimeInfo(effective, dbs, defaultAlias),
		DatabaseHandles:     dbs,
		MailDriver:          effective.MailDriver,
		MailFrom:            effective.MailFrom,
		SMTPHost:            effective.SMTPHost,
		Auth:                admin.NewDatabaseAdminAuth(adminAuthSQLDB, sessionManager, effective.AdminPrefix),
		Session:             sessionManager,
		SessionStore:        sessionStoreLabel,
		SessionRuntime:      sessionRuntimeIdentity,

		// Multi-tenant config
		MultiTenantEnabled:    effective.MultiTenant.Enabled,
		MultiTenantDefault:    effective.MultiTenant.DefaultTenant,
		MultiTenantAutoFilter: true,
		MultiTenantIDs:        tenantIDs(effective),
		MultiSiteEnabled:      effective.MultiSite.Enabled,
		MultiSiteDefault:      effective.MultiSite.DefaultSite,
		MultiSiteNames:        siteNames(effective),

		// RBAC config
		RBACEnforcer: rbacEnforcer,

		// Audit config
		AuditEnabled: true,
		AuditMaxSize: 10000,

		// Migrations path
		MigrationsPath: "migrations",

		// Storage for exports/imports
		Store: store,
	})
	if err := adminPanel.EnableLiveClusterRelay(); err != nil {
		return wrapOp("New admin live cluster", err)
	}
	a.Router.Use(adminPanel.LiveTrafficMiddleware())
	a.Admin = adminPanel

	a.OnShutdown(func(ctx context.Context) error {
		return a.Admin.Close(ctx)
	})

	if err := a.MountAdmin(); err != nil {
		return wrapOp("New mount admin", err)
	}

	return nil
}

// RegisterModel registers a model in the shared registry used by the admin panel.
func (a *App) RegisterModel(m interface{}, cfg ...model.ModelConfig) error {
	if a == nil {
		return wrapOp("RegisterModel", ErrNilApp)
	}
	if a.Models == nil {
		return wrapOp("RegisterModel", ErrModelsRegistryNotInitialized)
	}
	return a.Models.Register(m, cfg...)
}

// MountAdmin mounts the admin panel in the router exactly once.
func (a *App) MountAdmin() error {
	if a == nil {
		return wrapOp("MountAdmin", ErrNilApp)
	}
	if a.Admin == nil {
		return nil
	}
	if a.Router == nil || a.Config == nil {
		return wrapOp("MountAdmin", ErrNotInitialized)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.adminMounted {
		return nil
	}

	prefix := a.Config.AdminPrefix
	prefix = admin.NormalizePrefix(prefix)

	a.Router.Mount(prefix, a.Admin.Handler())
	a.adminMounted = true
	return nil
}

// MountOpenAPI mounts a JSON OpenAPI document endpoint exactly once per path.
func (a *App) MountOpenAPI(pattern string, provider openapi.DocumentProvider) error {
	if a == nil {
		return wrapOp("MountOpenAPI", ErrNilApp)
	}
	if a.Router == nil {
		return wrapOp("MountOpenAPI", ErrNotInitialized)
	}
	if provider == nil {
		return wrapOp("MountOpenAPI", errors.New("openapi provider is nil"))
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

	a.Router.Get(path, router.FromHTTP(openapi.HandlerFunc(provider)))
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
	if merged.AdminPrefix == "" {
		merged.AdminPrefix = base.AdminPrefix
	}
	if merged.AdminTitle == "" {
		merged.AdminTitle = base.AdminTitle
	}
	if merged.AdminAuthDatabase == "" {
		merged.AdminAuthDatabase = base.AdminAuthDatabase
	}
	if merged.AdminBootstrapUsername == "" {
		merged.AdminBootstrapUsername = base.AdminBootstrapUsername
	}
	if merged.AdminBootstrapEmail == "" {
		merged.AdminBootstrapEmail = base.AdminBootstrapEmail
	}
	if merged.AdminBootstrapPassword == "" {
		merged.AdminBootstrapPassword = base.AdminBootstrapPassword
	}
	if merged.AdminClusterChannel == "" {
		merged.AdminClusterChannel = base.AdminClusterChannel
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

func openDatabases(cfg *Config, logger *slog.Logger) (string, map[string]*db.DB, error) {
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

func buildAdminDatabaseRuntimeInfo(cfg *Config, dbs map[string]*db.DB, defaultAlias string) []admin.DatabaseRuntimeInfo {
	if cfg == nil || len(dbs) == 0 {
		return []admin.DatabaseRuntimeInfo{}
	}

	aliases := make([]string, 0, len(dbs))
	for alias := range dbs {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)

	out := make([]admin.DatabaseRuntimeInfo, 0, len(aliases))
	for _, alias := range aliases {
		handle := dbs[alias]
		dbCfg, _ := cfg.DatabaseByAlias(alias)
		out = append(out, admin.DatabaseRuntimeInfo{
			Alias:     alias,
			Engine:    strings.TrimSpace(string(handle.Engine())),
			Dialect:   detectDatabaseDialect(dbCfg.URL),
			IsDefault: alias == defaultAlias,
		})
	}
	return out
}

func detectDatabaseDialect(rawURL string) string {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		return "postgres"
	case strings.HasPrefix(lower, "mysql://"):
		return "mysql"
	case strings.HasPrefix(lower, "sqlserver://"), strings.HasPrefix(lower, "mssql://"):
		return "sqlserver"
	case strings.HasPrefix(lower, "oracle://"):
		return "oracle"
	case strings.HasPrefix(lower, "sqlite://"), strings.HasSuffix(lower, ".db"), strings.HasSuffix(lower, ".sqlite"), lower == ":memory:":
		return "sqlite"
	default:
		return "unknown"
	}
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

// tenantIDs returns a list of configured tenant IDs for the admin selector.
func tenantIDs(cfg *Config) []string {
	if cfg == nil || !cfg.MultiTenant.Enabled || len(cfg.MultiTenant.Tenants) == 0 {
		return nil
	}
	ids := make([]string, 0, len(cfg.MultiTenant.Tenants))
	for id := range cfg.MultiTenant.Tenants {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// siteNames returns a list of configured site names for the admin selector.
func siteNames(cfg *Config) []string {
	if cfg == nil || !cfg.MultiSite.Enabled || len(cfg.MultiSite.Sites) == 0 {
		return nil
	}
	names := make([]string, 0, len(cfg.MultiSite.Sites))
	for name := range cfg.MultiSite.Sites {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// rbacPolicyPath returns the RBAC policy file path if it exists.
func rbacPolicyPath(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	path := strings.TrimSpace(cfg.AdminRBACPolicyFile)
	if path == "" {
		// Check default locations
		for _, p := range []string{"admin_rbac.csv", "config/admin_rbac.csv", "rbac/admin_rbac.csv"} {
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
