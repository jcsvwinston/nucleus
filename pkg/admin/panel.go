// Package admin provides an auto-generated administration panel for GoFrame,
// similar to Django's contrib.admin. It exposes a REST API for CRUD operations
// on registered models and serves an embedded SPA frontend.
package admin

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/goframe/goframe/pkg/auth"
	"github.com/goframe/goframe/pkg/model"
	"github.com/goframe/goframe/pkg/signals"
	"gorm.io/gorm"
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
	db       *gorm.DB
	registry *model.Registry
	config   PanelConfig
	logger   *slog.Logger
	bus      *signals.Bus
	cruds    map[string]*model.CRUD
}

// NewPanel creates a new admin panel.
func NewPanel(db *gorm.DB, registry *model.Registry, logger *slog.Logger, cfg PanelConfig) *Panel {
	if cfg.Prefix == "" {
		cfg.Prefix = "/admin"
	}
	if cfg.Title == "" {
		cfg.Title = "GoFrame Admin"
	}

	return &Panel{
		db:       db,
		registry: registry,
		config:   cfg,
		logger:   logger,
		cruds:    make(map[string]*model.CRUD),
	}
}

// SetSignalBus sets the signal bus for CRUD operations.
func (p *Panel) SetSignalBus(bus *signals.Bus) {
	p.bus = bus
}

// getCRUD returns or creates a CRUD instance for the given model.
func (p *Panel) getCRUD(meta *model.ModelMeta) *model.CRUD {
	if c, ok := p.cruds[meta.Name]; ok {
		return c
	}
	c := model.NewCRUD(p.db, meta, p.bus)
	p.cruds[meta.Name] = c
	return c
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
		f, err := fsys.Open("index.html")
		if err != nil {
			http.Error(w, "admin UI not found", 500)
			return
		}
		defer f.Close()
		stat, _ := f.Stat()
		http.ServeContent(w, r, "index.html", stat.ModTime(), f.(interface{ Read([]byte) (int, error); Seek(int64, int) (int64, error) }).(http.File))
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
