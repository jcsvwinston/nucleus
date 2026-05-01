// Package fluent provides a simplified, Gin/Fiber-like API for the GoFrame framework
// while maintaining all enterprise capabilities (multi-tenancy, admin, observability).
package fluent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/app"
	routerpkg "github.com/jcsvwinston/GoFrame/pkg/router"
)

// AppBuilder provides a fluent API for configuring GoFrame applications
type AppBuilder struct {
	config    app.Config
	router    *routerpkg.Router
	models    []interface{}
	providers []interface{}
	logger    *slog.Logger

	// SPA configuration
	spaEnabled bool
	spaDir     string
	spaConfig  SPAConfig

	// Route groups
	routes []routeDef
}

// SPAConfig configures SPA serving behavior
type SPAConfig struct {
	IndexFile string
	APIPrefix string
}

// New creates a new GoFrame application with sensible defaults
func New() *AppBuilder {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg := app.DefaultConfig()
	r := routerpkg.New(logger)

	return &AppBuilder{
		config: cfg,
		router: r,
		logger: logger,
	}
}

// routeDef stores route definitions for later registration
type routeDef struct {
	method      string
	pattern     string
	handlers    []routerpkg.Handler
	middlewares []func(http.Handler) http.Handler
}

// Load creates an AppBuilder from a YAML configuration file
func Load(path string) *AppBuilder {
	cfg, err := app.LoadConfig(path)
	if err != nil {
		panic(fmt.Sprintf("failed to load config: %v", err))
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	r := routerpkg.New(logger)

	return &AppBuilder{
		config: *cfg,
		router: r,
		logger: logger,
	}
}

// Port sets the server port
func (b *AppBuilder) Port(p int) *AppBuilder {
	b.config.Port = p
	return b
}

// Host sets the server host
func (b *AppBuilder) Host(h string) *AppBuilder {
	b.config.Host = h
	return b
}

// SQLite configures a SQLite database
func (b *AppBuilder) SQLite(path string) *AppBuilder {
	b.config.Databases["default"] = app.DatabaseConfig{
		URL:         fmt.Sprintf("sqlite://%s", path),
		MaxOpen:     25,
		MaxIdle:     5,
		MaxLifetime: 5 * time.Minute,
	}
	return b
}

// Postgres configures a PostgreSQL database
func (b *AppBuilder) Postgres(url string) *AppBuilder {
	b.config.Databases["default"] = app.DatabaseConfig{
		URL:         url,
		MaxOpen:     25,
		MaxIdle:     5,
		MaxLifetime: 5 * time.Minute,
	}
	return b
}

// MySQL configures a MySQL database
func (b *AppBuilder) MySQL(url string) *AppBuilder {
	b.config.Databases["default"] = app.DatabaseConfig{
		URL:         url,
		MaxOpen:     25,
		MaxIdle:     5,
		MaxLifetime: 5 * time.Minute,
	}
	return b
}

// WithAdmin enables the admin panel
func (b *AppBuilder) WithAdmin(prefix string) *AppBuilder {
	b.config.AdminPrefix = prefix
	return b
}

// SPA configures single-page application serving
func (b *AppBuilder) SPA(dir string, cfg SPAConfig) *AppBuilder {
	b.spaEnabled = true
	b.spaDir = dir
	b.spaConfig = cfg
	return b
}

// Templates configures HTML template directory
func (b *AppBuilder) Templates(dir string) *AppBuilder {
	b.config.TemplatesDir = dir
	return b
}

// Static configures static file serving
func (b *AppBuilder) Static(dir string) *AppBuilder {
	b.config.StaticRoot = dir
	b.config.StaticPrefix = "/static/"
	return b
}

// Cors configures CORS
func (b *AppBuilder) Cors(cfg CorsConfig) *AppBuilder {
	if cfg.AllowAll {
		b.router.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				if r.Method == "OPTIONS" {
					w.WriteHeader(http.StatusOK)
					return
				}
				next.ServeHTTP(w, r)
			})
		})
	}
	return b
}

// CorsConfig configures CORS behavior
type CorsConfig struct {
	AllowAll bool
	Origins  []string
}

// CorsAllowAll returns a CORS config that allows all origins
func CorsAllowAll() CorsConfig {
	return CorsConfig{AllowAll: true}
}

// Provide registers a service for dependency injection
func (b *AppBuilder) Provide(svc interface{}) *AppBuilder {
	b.providers = append(b.providers, svc)
	return b
}

// Model registers a model for auto-migration
func (b *AppBuilder) Model(m interface{}) *AppBuilder {
	b.models = append(b.models, m)
	return b
}

// AutoMigrate runs migrations for all registered models
func (b *AppBuilder) AutoMigrate() *AppBuilder {
	// Will be executed during Run()
	return b
}

// Run starts the application
func (b *AppBuilder) Run() error {
	// Create the app
	a, err := app.New(&b.config)
	if err != nil {
		return fmt.Errorf("failed to create app: %w", err)
	}

	// Migrate models if any
	if len(b.models) > 0 {
		if err := a.AutoMigrate(b.models...); err != nil {
			return fmt.Errorf("failed to migrate: %w", err)
		}
	}

	// Register all routes
	for _, route := range b.routes {
		switch route.method {
		case "GET":
			a.Router.With(route.middlewares...).Get(route.pattern, route.handlers...)
		case "POST":
			a.Router.With(route.middlewares...).Post(route.pattern, route.handlers...)
		case "PUT":
			a.Router.With(route.middlewares...).Put(route.pattern, route.handlers...)
		case "DELETE":
			a.Router.With(route.middlewares...).Delete(route.pattern, route.handlers...)
		}
	}

	// Setup SPA serving if enabled (must be last to act as fallback)
	if b.spaEnabled {
		b.setupSPA(a.Router)
	}

	fmt.Printf("GoFrame running on http://%s\n", b.config.Addr())
	return a.Run(context.Background())
}

func (b *AppBuilder) setupSPA(r *routerpkg.Router) {
	// SPA FileServer wrapper: serves static files if they exist, otherwise index.html
	spaHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip API routes
		if b.spaConfig.APIPrefix != "" && strings.HasPrefix(r.URL.Path, b.spaConfig.APIPrefix) {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Try to open the requested file
		path := filepath.Join(b.spaDir, filepath.Clean(r.URL.Path))
		info, err := os.Stat(path)

		// Serve static file if it exists and is not a directory
		if err == nil && !info.IsDir() {
			http.FileServer(http.Dir(b.spaDir)).ServeHTTP(w, r)
			return
		}

		// For directories or non-existent files, serve index.html (SPA routing)
		http.ServeFile(w, r, filepath.Join(b.spaDir, b.spaConfig.IndexFile))
	})

	// Register as subtree pattern (matches all paths under /)
	r.Handle("/", spaHandler)
}

// WithConfig provides direct access to app.Config for advanced configuration.
// This is the primary mechanism for configuring features not covered by
// convenience methods like Port(), SQLite(), etc.
func (b *AppBuilder) WithConfig(fn func(*app.Config)) *AppBuilder {
	fn(&b.config)
	return b
}

// Config returns the underlying app.Config for read access or advanced mutation.
// Prefer WithConfig() for modifications to maintain fluent chaining.
func (b *AppBuilder) Config() *app.Config {
	return &b.config
}

// Logger returns the application logger
func (b *AppBuilder) Logger() *slog.Logger {
	return b.logger
}
