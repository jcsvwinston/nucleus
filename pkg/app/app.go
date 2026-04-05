package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
	Mailer  mail.Sender
	Session *auth.SessionManager
	Models  *model.Registry
	Admin   *admin.Panel

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

	dbConn, err := db.New(db.Config{
		Engine:              db.Engine(effective.DatabaseEngine),
		DatabaseURL:         effective.DatabaseURL,
		DatabaseMaxOpen:     effective.DatabaseMaxOpen,
		DatabaseMaxIdle:     effective.DatabaseMaxIdle,
		DatabaseMaxLifetime: effective.DatabaseMaxLifetime,
	}, logger)
	if err != nil {
		_ = telemetryShutdown(context.Background())
		return nil, wrapOp("New db", err)
	}

	sessionManager, sessionStoreShutdown, err := buildSessionManager(effective, dbConn)
	if err != nil {
		_ = dbConn.Close()
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
		Config:  effective,
		Logger:  logger,
		Router:  r,
		DB:      dbConn,
		Mailer:  mailer,
		Session: sessionManager,
		Models:  reg,
		Admin:   adminPanel,
	}

	// DB close should always happen on app shutdown.
	a.OnShutdown(func(context.Context) error {
		return a.DB.Close()
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
	if merged.DatabaseURL == "" {
		merged.DatabaseURL = base.DatabaseURL
	}
	if merged.DatabaseEngine == "" {
		merged.DatabaseEngine = base.DatabaseEngine
	}
	if merged.DatabaseMaxOpen == 0 {
		merged.DatabaseMaxOpen = base.DatabaseMaxOpen
	}
	if merged.DatabaseMaxIdle == 0 {
		merged.DatabaseMaxIdle = base.DatabaseMaxIdle
	}
	if merged.DatabaseMaxLifetime == 0 {
		merged.DatabaseMaxLifetime = base.DatabaseMaxLifetime
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

	return &merged
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
			DatabaseURL: cfg.DatabaseURL,
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
