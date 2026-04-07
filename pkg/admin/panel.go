// Package admin provides an auto-generated administration panel for GoFrame,
// similar to Django's contrib.admin. It exposes a REST API for CRUD operations
// on registered models and serves an embedded SPA frontend.
package admin

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/auth"
	"github.com/jcsvwinston/GoFrame/pkg/db"
	"github.com/jcsvwinston/GoFrame/pkg/model"
	"github.com/jcsvwinston/GoFrame/pkg/router"
	"github.com/jcsvwinston/GoFrame/pkg/signals"
)

//go:embed ui/*
var uiFS embed.FS

type adminAuthContextKey struct{}

const adminSessionTouchKey = "__goframe_admin_seen_at"

// AdminAuth is the interface for admin panel authentication and authorization.
type AdminAuth interface {
	Authenticate(r *http.Request) (*auth.User, error)
	Authorize(user *auth.User, model string, action string) bool
	LoginHandler() http.Handler
}

// PanelConfig configures the admin panel.
type PanelConfig struct {
	Prefix         string // URL prefix (default "/admin")
	Title          string // Site title shown in the UI
	Auth           AdminAuth
	Session        *auth.SessionManager // optional session manager for admin telemetry
	SessionStore   string               // configured session store label (memory|sql|redis)
	SessionRuntime auth.SessionRuntimeIdentity
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

	sqlDB, err := p.db.SqlDB()
	if err != nil {
		return nil, fmt.Errorf("admin.getCRUD model=%s: %w", meta.Name, err)
	}
	c := model.NewCRUD(sqlDB, meta, p.bus)

	p.cruds[meta.Name] = c
	return c, nil
}

// Handler returns a *router.Mux that can be mounted on the application router.
func (p *Panel) Handler() *router.Mux {
	r := router.NewMux()

	// Auth middleware if configured
	if p.config.Auth != nil {
		loginHandler := p.config.Auth.LoginHandler()
		r.Get("/login", loginHandler.ServeHTTP)
		r.Post("/login", loginHandler.ServeHTTP)
		r.Group(func(r *router.Mux) {
			r.Use(p.authMiddleware)
			r.Use(p.sessionActivityMiddleware)
			p.mountRoutes(r)
		})
	} else {
		r.Use(p.sessionActivityMiddleware)
		p.mountRoutes(r)
	}

	return r
}

func (p *Panel) mountRoutes(r *router.Mux) {
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
	r.Get("/api/sessions", p.handleListSessions)

	// Serve embedded UI
	uiContent, _ := fs.Sub(uiFS, "ui")
	fileServer := http.FileServer(http.FS(uiContent))
	r.Get("/static/{filepath...}", http.StripPrefix("/static", fileServer).ServeHTTP)
	r.Get("/{path...}", p.handleSPA(uiContent))
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
		user, err := p.config.Auth.Authenticate(r)
		if err != nil {
			http.Redirect(w, r, p.config.Prefix+"/login", http.StatusFound)
			return
		}
		ctx := context.WithValue(r.Context(), adminAuthContextKey{}, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (p *Panel) sessionActivityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.touchAdminSession(r)
		next.ServeHTTP(w, r)
	})
}

func (p *Panel) touchAdminSession(r *http.Request) {
	if p == nil || p.config.Session == nil || r == nil {
		return
	}
	ctx := r.Context()
	if !sessionContextReady(p.config.Session, ctx) {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(p.config.Session.GetString(ctx, auth.SessionMetaFirstSeenAtKey)) == "" {
		p.config.Session.Put(ctx, auth.SessionMetaFirstSeenAtKey, now)
	}
	p.config.Session.Put(ctx, auth.SessionMetaLastSeenAtKey, now)

	if pod := strings.TrimSpace(p.config.SessionRuntime.Pod); pod != "" {
		p.config.Session.Put(ctx, auth.SessionMetaPodKey, pod)
	}
	if host := strings.TrimSpace(p.config.SessionRuntime.Host); host != "" {
		p.config.Session.Put(ctx, auth.SessionMetaHostKey, host)
	}
	if instance := strings.TrimSpace(p.config.SessionRuntime.Instance); instance != "" {
		p.config.Session.Put(ctx, auth.SessionMetaInstanceKey, instance)
	}

	// Keep a dedicated admin heartbeat value so the session always commits.
	p.config.Session.Put(ctx, adminSessionTouchKey, now)
}

func sessionContextReady(sm *auth.SessionManager, ctx context.Context) (ok bool) {
	if sm == nil || ctx == nil {
		return false
	}
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	_ = sm.SCS().Token(ctx)
	return true
}

func (p *Panel) authorizeAction(w http.ResponseWriter, r *http.Request, modelName, action string) bool {
	if p.config.Auth == nil {
		return true
	}

	user, err := p.authenticatedUser(r)
	if err != nil {
		writeErr(w, authErrorToDomain(err))
		return false
	}
	if !p.config.Auth.Authorize(user, modelName, action) {
		writeErr(w, authDeniedDomain(modelName, action))
		return false
	}
	return true
}

func (p *Panel) authenticatedUser(r *http.Request) (*auth.User, error) {
	if r != nil {
		if user, ok := r.Context().Value(adminAuthContextKey{}).(*auth.User); ok && user != nil {
			return user, nil
		}
	}
	return p.config.Auth.Authenticate(r)
}
