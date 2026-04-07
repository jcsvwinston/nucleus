package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/admin"
	"github.com/jcsvwinston/GoFrame/pkg/auth"
	"github.com/jcsvwinston/GoFrame/pkg/db"
	"github.com/jcsvwinston/GoFrame/pkg/mail"
	"github.com/jcsvwinston/GoFrame/pkg/model"
	"github.com/jcsvwinston/GoFrame/pkg/observe"
	"github.com/jcsvwinston/GoFrame/pkg/router"
)

// App is the main GoFrame application container. It wires the minimum runtime
// dependencies (config, logger, router, DB, model registry, and admin panel).
type App struct {
	Config  *Config
	Logger  *slog.Logger
	Router  *router.Router
	DB      *db.DB
	DBs     map[string]*db.DB
	Mailer  mail.Sender
	Session *auth.SessionManager
	Models  *model.Registry
	Admin   *admin.Panel

	databaseDefaultAlias string
	scopeResolver        *requestScopeResolver

	mu           sync.Mutex
	server       *http.Server
	shutdownFns  []func(context.Context) error
	adminMounted bool
}

// New creates an application container with default wiring.
func New(cfg *Config) (*App, error) {
	if cfg == nil {
		return nil, wrapOp("New", ErrNilConfig)
	}

	effective := mergeDefaults(cfg)
	if err := validateMultiTenantIsolation(effective); err != nil {
		return nil, wrapOp("New validate multitenant", err)
	}
	logger := observe.NewLogger(effective.LogLevel, effective.LogFormat)

	telemetryShutdown, err := observe.SetupOpenTelemetry(context.Background(), observe.TelemetryConfig{
		ServiceName:  "goframe-app",
		OTLPEndpoint: effective.OTLPEndpoint,
	}, logger)
	if err != nil {
		return nil, wrapOp("New telemetry", err)
	}

	mailer, err := mail.NewSender(mail.Config{
		Driver:           effective.MailDriver,
		Timeout:          effective.WriteTimeout,
		SMTPHost:         effective.SMTPHost,
		SMTPPort:         effective.SMTPPort,
		SMTPUser:         effective.SMTPUser,
		SMTPPass:         effective.SMTPPass,
		SendGridAPIKey:   effective.SendGridAPIKey,
		SendGridEndpoint: effective.SendGridEndpoint,
	})
	if err != nil {
		_ = telemetryShutdown(context.Background())
		return nil, wrapOp("New mail", err)
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
	r.Use(auth.RuntimeMetadataMiddleware(sessionManager, sessionRuntimeIdentity, 30*time.Second))
	reg := model.NewRegistry()
	sessionStoreLabel := strings.ToLower(strings.TrimSpace(effective.SessionStore))
	if sessionStoreLabel == "" {
		sessionStoreLabel = "memory"
	}
	adminPanel := admin.NewPanel(dbConn, reg, logger, admin.PanelConfig{
		Prefix:         effective.AdminPrefix,
		Title:          effective.AdminTitle,
		Session:        sessionManager,
		SessionStore:   sessionStoreLabel,
		SessionRuntime: sessionRuntimeIdentity,
	})

	a := &App{
		Config:               effective,
		Logger:               logger,
		Router:               r,
		DB:                   dbConn,
		DBs:                  dbs,
		Mailer:               mailer,
		Session:              sessionManager,
		Models:               reg,
		Admin:                adminPanel,
		databaseDefaultAlias: defaultAlias,
		scopeResolver:        scopeResolver,
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

	if err := a.MountAdmin(); err != nil {
		_ = a.Shutdown(context.Background())
		return nil, wrapOp("New mount admin", err)
	}

	return a, nil
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
	if prefix == "" {
		prefix = "/admin"
	}

	a.Router.Mount(prefix, a.Admin.Handler())
	a.adminMounted = true
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
		err := srv.ListenAndServe()
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
	if merged.MailDriver == "" {
		merged.MailDriver = base.MailDriver
	}
	if merged.SMTPPort == 0 {
		merged.SMTPPort = base.SMTPPort
	}
	if merged.MailFrom == "" {
		merged.MailFrom = base.MailFrom
	}
	if merged.SendGridEndpoint == "" {
		merged.SendGridEndpoint = base.SendGridEndpoint
	}
	if merged.LogLevel == "" {
		merged.LogLevel = base.LogLevel
	}
	if merged.LogFormat == "" {
		merged.LogFormat = base.LogFormat
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
