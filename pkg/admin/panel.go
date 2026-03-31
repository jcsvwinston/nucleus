// Package admin provides an auto-generated administration panel for GoFrame,
// similar to Django's contrib.admin. It exposes a REST API for CRUD operations
// on registered models and serves an embedded SPA frontend.
package admin

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jcsvwinston/GoFrame/pkg/auth"
	"github.com/jcsvwinston/GoFrame/pkg/db"
	"github.com/jcsvwinston/GoFrame/pkg/model"
	"github.com/jcsvwinston/GoFrame/pkg/signals"
)

//go:embed ui/*
var uiFS embed.FS

// AdminAuth is the interface for admin panel authentication and authorization.
type AdminAuth interface {
	Authenticate(r *http.Request) (*auth.User, error)
	Authorize(user *auth.User, model string, action string) bool
	LoginHandler() http.Handler
}

// PanelConfig configures the admin panel.
type PanelConfig struct {
	Prefix string // URL prefix (default "/admin")
	Title  string // Site title shown in the UI
	Auth   AdminAuth
}

// Panel is the admin panel instance that provides CRUD UI for registered models.
type Panel struct {
	db       *db.DB
	registry *model.Registry
	config   PanelConfig
	logger   *slog.Logger
	bus      *signals.Bus
	cruds    map[string]model.CRUDOperator
}

// NewPanel creates a new admin panel.
func NewPanel(database *db.DB, registry *model.Registry, logger *slog.Logger, cfg PanelConfig) *Panel {
	if cfg.Prefix == "" {
		cfg.Prefix = "/admin"
	}
	if cfg.Title == "" {
		cfg.Title = "GoFrame Admin"
	}

	return &Panel{
		db:       database,
		registry: registry,
		config:   cfg,
		logger:   logger,
		cruds:    make(map[string]model.CRUDOperator),
	}
}

// SetSignalBus sets the signal bus for CRUD operations.
func (p *Panel) SetSignalBus(bus *signals.Bus) {
	p.bus = bus
}

// getCRUD returns or creates a CRUD instance for the given model.
func (p *Panel) getCRUD(meta *model.ModelMeta) (model.CRUDOperator, error) {
	if c, ok := p.cruds[meta.Name]; ok {
		return c, nil
	}

	if p.db == nil {
		return nil, fmt.Errorf("admin.getCRUD model=%s: nil database", meta.Name)
	}

	var c model.CRUDOperator
	switch p.db.Engine() {
	case db.EngineBun:
		bunDB := p.db.BunDB()
		if bunDB == nil {
			return nil, fmt.Errorf("admin.getCRUD model=%s: %w", meta.Name, db.ErrBunRequired)
		}
		c = model.NewCRUDBun(bunDB, meta, p.bus)
	case db.EngineGORM:
		gormDB := p.db.GormDB()
		if gormDB == nil {
			return nil, fmt.Errorf("admin.getCRUD model=%s: %w", meta.Name, db.ErrGORMRequired)
		}
		c = model.NewCRUD(gormDB, meta, p.bus)
	default:
		return nil, fmt.Errorf("admin.getCRUD model=%s: unsupported engine %s", meta.Name, p.db.Engine())
	}

	p.cruds[meta.Name] = c
	return c, nil
}

// Handler returns a chi.Router that can be mounted on the application router.
func (p *Panel) Handler() chi.Router {
	r := chi.NewRouter()

	// Auth middleware if configured
	if p.config.Auth != nil {
		r.Handle("/login", p.config.Auth.LoginHandler())
		r.Group(func(r chi.Router) {
			r.Use(p.authMiddleware)
			p.mountRoutes(r)
		})
	} else {
		p.mountRoutes(r)
	}

	return r
}

func (p *Panel) mountRoutes(r chi.Router) {
	// API routes
	r.Get("/api/models", p.handleListModels)
	r.Get("/api/models/{name}/schema", p.handleGetSchema)
	r.Get("/api/models/{name}", p.handleListRecords)
	r.Post("/api/models/{name}", p.handleCreateRecord)
	r.Get("/api/models/{name}/{id}", p.handleGetRecord)
	r.Put("/api/models/{name}/{id}", p.handleUpdateRecord)
	r.Delete("/api/models/{name}/{id}", p.handleDeleteRecord)
	r.Post("/api/models/{name}/bulk", p.handleBulkAction)
	r.Get("/api/models/{name}/export", p.handleExportCSV)

	// Serve embedded UI
	uiContent, _ := fs.Sub(uiFS, "ui")
	fileServer := http.FileServer(http.FS(uiContent))
	r.Get("/static/*", http.StripPrefix(p.config.Prefix+"/static", fileServer).ServeHTTP)
	r.Get("/*", p.handleSPA(uiContent))
}

func (p *Panel) handleSPA(fsys fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		content, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.Error(w, "admin UI not found", 500)
			return
		}

		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(content))
	}
}

func (p *Panel) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := p.config.Auth.Authenticate(r)
		if err != nil {
			http.Redirect(w, r, p.config.Prefix+"/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}
