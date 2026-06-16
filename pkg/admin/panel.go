// Package admin provides an auto-generated administration panel for Nucleus,
// similar to Django's contrib.admin. It exposes a REST API for CRUD operations
// on registered models and serves an embedded SPA frontend.
package admin

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/jcsvwinston/nucleus/pkg/signals"
	"github.com/jcsvwinston/nucleus/pkg/storage"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

type adminAuthContextKey struct{}

const adminSessionTouchKey = "__nucleus_admin_seen_at"

// DatabaseRuntimeInfo describes one configured DB alias for admin observability.
type DatabaseRuntimeInfo struct {
	Alias     string `json:"alias"`
	Engine    string `json:"engine"`
	Dialect   string `json:"dialect"`
	IsDefault bool   `json:"is_default"`
}

// AdminAuth is the interface for admin panel authentication and authorization.
type AdminAuth interface {
	Authenticate(r *http.Request) (*auth.User, error)
	Authorize(user *auth.User, model string, action string) bool
	LoginHandler() http.Handler
}

// PanelConfig configures the admin panel.
type PanelConfig struct {
	Prefix              string // URL prefix (default "/admin")
	Title               string // Site title shown in the UI
	Environment         string
	OTLPEndpoint        string          // optional OTLP endpoint configured by the host app
	RedisURL            string          // optional Redis URL for background jobs runtime snapshot
	TaskInspector       tasks.Inspector // optional configured queue inspector
	LiveExcludePatterns []string        // optional path patterns excluded from live HTTP capture
	LiveClusterEnabled  bool            // when true, publish/subscribe live telemetry through Redis
	LiveClusterRedisURL string          // optional Redis URL for live cluster relay (falls back to RedisURL)
	LiveClusterChannel  string          // optional pub/sub channel (default nucleus:admin:live:v1)
	LiveClusterNodeID   string          // optional explicit node id (defaults to runtime identity)
	LiveClusterToken    string          // optional shared token to reject untrusted relay messages
	TraceURLTemplate    string          // optional trace explorer URL template (supports {trace_id})
	Databases           []DatabaseRuntimeInfo
	DatabaseHandles     map[string]*db.DB // optional alias->db handle mapping for runtime stats
	EnvironmentSnapshot []string          // optional env snapshot (defaults to os.Environ at startup)
	FeatureFlags        map[string]bool   // optional initial in-memory feature flags
	MailDriver          string
	MailFrom            string
	SMTPHost            string
	Auth                AdminAuth
	Session             *auth.SessionManager // optional session manager for admin telemetry
	SessionStore        string               // configured session store label (memory|sql|redis)
	SessionRuntime      auth.SessionRuntimeIdentity

	// Multi-tenant configuration
	MultiTenantEnabled    bool     // whether multi-tenant mode is active
	MultiTenantDefault    string   // default tenant ID when none specified
	MultiTenantAutoFilter bool     // auto-filter CRUD queries by tenant (default true when multi-tenant enabled)
	MultiTenantField      string   // override tenant field name (empty = auto-detect from model)
	MultiTenantIDs        []string // known tenant IDs for the selector UI (empty = discover from scope)
	MultiSiteEnabled      bool     // whether multi-site mode is active
	MultiSiteDefault      string   // default site name
	MultiSiteNames        []string // known site names for the selector UI

	// RBAC configuration
	RBACEnforcer *authz.Enforcer // optional Casbin enforcer for fine-grained authorization

	// Audit logging configuration
	AuditEnabled bool // whether audit logging is enabled
	AuditMaxSize int  // max audit entries in memory (default 10000)

	// Migrations path
	MigrationsPath string // path to migration files directory

	// Storage for exports/imports
	Store storage.Store
}

// Panel is the admin panel instance that provides CRUD UI for registered models.
type Panel struct {
	db             *db.DB
	registry       *model.Registry
	config         PanelConfig
	logger         *slog.Logger
	bus            *signals.Bus
	cruds          map[string]model.CRUDOperator
	live           *liveRuntime
	liveExcludeMu  sync.RWMutex
	liveExcludes   []string
	flags          *featureFlagStore
	bootEnv        []systemEnvVar
	defaultDBAlias string
	liveNode       string
	liveClusterMu  sync.RWMutex
	liveCluster    *liveClusterRelay

	// Observability bus → live SQL feed. When connected, every model.CRUD
	// query across the whole application (not just the admin's own Data
	// Studio CRUDs) reaches the live view, and getCRUD skips the redundant
	// per-CRUD observer. observCancel (idempotent via observStopOnce) stops
	// the consumer goroutine; it is assigned once at ConsumeObservability and
	// only ever read afterwards.
	observConnected atomic.Bool
	observCancel    func()
	observStopOnce  sync.Once

	// Tenant resolution cache: model name -> tenant column
	tenantFields map[string]string

	// RBAC enforcer for fine-grained authorization
	rbac *authz.Enforcer

	// Audit log store
	audit *auditStore

	// Storage for exports/imports
	store storage.Store

	// Export results cache (in-memory for quick status lookup)
	exportMu      sync.RWMutex
	exportResults map[string]ExportResult
}

// NewPanel creates a new admin panel.
func NewPanel(database *db.DB, registry *model.Registry, logger *slog.Logger, cfg PanelConfig) *Panel {
	cfg.Prefix = NormalizePrefix(cfg.Prefix)
	if cfg.Title == "" {
		cfg.Title = "Nucleus Admin"
	}
	env := cfg.EnvironmentSnapshot
	if len(env) == 0 {
		env = os.Environ()
	}

	return &Panel{
		db:             database,
		registry:       registry,
		config:         cfg,
		logger:         logger,
		cruds:          make(map[string]model.CRUDOperator),
		live:           newLiveRuntime(),
		liveExcludes:   normalizeLiveExcludePatterns(cfg.Prefix, cfg.LiveExcludePatterns),
		flags:          newFeatureFlagStore(cfg.FeatureFlags),
		bootEnv:        buildSystemEnvironmentRows(env),
		defaultDBAlias: defaultDatabaseAlias(cfg),
		liveNode:       resolvePanelLiveNodeID(cfg),
		tenantFields:   make(map[string]string),
		rbac:           cfg.RBACEnforcer,
		audit: func() *auditStore {
			if cfg.AuditEnabled {
				return newAuditStore(cfg.AuditMaxSize)
			}
			return nil
		}(),
		store:         cfg.Store,
		exportResults: make(map[string]ExportResult),
	}
}

// EnableLiveClusterRelay enables cluster-aware live telemetry distribution.
// It is optional and safe to call multiple times.
func (p *Panel) EnableLiveClusterRelay() error {
	if p == nil {
		return fmt.Errorf("admin panel is not initialized")
	}
	if !p.config.LiveClusterEnabled {
		return nil
	}

	p.liveClusterMu.RLock()
	existing := p.liveCluster
	p.liveClusterMu.RUnlock()
	if existing != nil {
		return nil
	}

	redisURL := strings.TrimSpace(p.config.LiveClusterRedisURL)
	if redisURL == "" {
		redisURL = strings.TrimSpace(p.config.RedisURL)
	}
	if redisURL == "" {
		return fmt.Errorf("admin live cluster enabled but redis url is empty")
	}

	channel := strings.TrimSpace(p.config.LiveClusterChannel)
	if channel == "" {
		channel = defaultLiveClusterChannel
	}

	nodeID := strings.TrimSpace(p.config.LiveClusterNodeID)
	if nodeID == "" {
		nodeID = p.liveNodeID()
	}
	relay, err := newLiveClusterRelay(liveClusterRelayConfig{
		RedisURL: redisURL,
		Channel:  channel,
		NodeID:   nodeID,
		Token:    strings.TrimSpace(p.config.LiveClusterToken),
		Logger:   p.logger,
		OnEvent:  p.ingestClusterLiveEvent,
	})
	if err != nil {
		return fmt.Errorf("admin live cluster relay: %w", err)
	}

	p.liveClusterMu.Lock()
	defer p.liveClusterMu.Unlock()
	if p.liveCluster != nil {
		_ = relay.close(context.Background())
		return nil
	}
	p.liveCluster = relay
	p.liveNode = nodeID
	return nil
}

// Close releases optional admin panel background resources.
func (p *Panel) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if p.observCancel != nil {
		p.observCancel() // idempotent (observStopOnce): stop the SQL feed consumer
	}
	p.liveClusterMu.Lock()
	relay := p.liveCluster
	p.liveCluster = nil
	p.liveClusterMu.Unlock()
	if relay == nil {
		return nil
	}
	return relay.close(ctx)
}

func (p *Panel) liveNodeID() string {
	if p == nil {
		return "node-local"
	}
	node := strings.TrimSpace(p.liveNode)
	if node == "" {
		return "node-local"
	}
	return node
}

func resolvePanelLiveNodeID(cfg PanelConfig) string {
	if explicit := normalizePanelNodeID(cfg.LiveClusterNodeID); explicit != "" {
		return explicit
	}
	if instance := normalizePanelNodeID(cfg.SessionRuntime.Instance); instance != "" {
		return instance
	}
	if pod := normalizePanelNodeID(cfg.SessionRuntime.Pod); pod != "" {
		host := normalizePanelNodeID(cfg.SessionRuntime.Host)
		if host != "" && !strings.EqualFold(pod, host) {
			return pod + "@" + host
		}
		return pod
	}
	if host := normalizePanelNodeID(cfg.SessionRuntime.Host); host != "" {
		return host
	}
	hostname, err := os.Hostname()
	if err == nil {
		if host := normalizePanelNodeID(hostname); host != "" {
			return host
		}
	}
	return "node-local"
}

func normalizePanelNodeID(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, " ", "-")
	if len(value) > 120 {
		value = value[:120]
	}
	return value
}

// SetSignalBus sets the signal bus for CRUD operations.
func (p *Panel) SetSignalBus(bus *signals.Bus) {
	p.bus = bus
}

// getCRUD returns or creates a CRUD instance for the given model and database alias.
func (p *Panel) getCRUD(meta *model.ModelMeta, databaseAlias string) (model.CRUDOperator, error) {
	alias, err := p.resolveDatabaseAlias(databaseAlias)
	if err != nil {
		return nil, err
	}

	cacheKey := alias + "::" + meta.Name
	if c, ok := p.cruds[cacheKey]; ok {
		return c, nil
	}

	dbHandle, err := p.databaseHandle(alias)
	if err != nil {
		return nil, fmt.Errorf("admin.getCRUD alias=%s model=%s: %w", alias, meta.Name, err)
	}

	sqlDB, err := dbHandle.SqlDB()
	if err != nil {
		return nil, fmt.Errorf("admin.getCRUD alias=%s model=%s: %w", alias, meta.Name, err)
	}
	c := model.NewCRUD(sqlDB, meta, p.bus)
	// When the observability bus feeds the live SQL view, it already carries
	// every model.CRUD query (these admin CRUDs included), so the per-CRUD
	// observer would double-record. Install it only as the fallback for when
	// no bus is connected (e.g. a Panel built without app.New).
	if !p.observConnected.Load() {
		c.SetSQLQueryObserver(p.onModelSQLQuery)
	}

	// Set dialect for estimation strategies
	if info, ok := p.databaseRuntimeInfoByAlias(alias); ok {
		dialect := info.Dialect
		if dialect == "" {
			dialect = info.Engine
		}
		c.SetDialect(dialect)
	}

	p.cruds[cacheKey] = c
	return c, nil
}

// Handler returns a *router.Mux that can be mounted on the application router.
func (p *Panel) Handler() *router.Mux {
	r := router.NewMux()
	p.mountRoutes(r)
	return r
}

func (p *Panel) mountRoutes(r *router.Mux) {
	uiContent := adminUIContentFS()
	fileServer := http.FileServer(http.FS(uiContent))
	r.Get("/static/{filepath...}", router.FromHTTP(http.StripPrefix("/static", fileServer).ServeHTTP))

	if assetsFS, err := fs.Sub(uiContent, "assets"); err == nil {
		assetsServer := http.FileServer(http.FS(assetsFS))
		r.Get("/assets/{filepath...}", router.FromHTTP(http.StripPrefix("/assets", assetsServer).ServeHTTP))
	}
	r.Get("/favicon.svg", router.FromHandler(fileServer))

	// API routes and SPA fallback.
	//
	// When an admin auth provider is configured — every framework-wired
	// deployment sets one (pkg/app, pkg/nucleus) — all /api/* routes are
	// mounted behind authMiddleware so authentication is enforced at the
	// router edge instead of relying on each handler's authorizeAction()
	// call. Per-handler authorizeAction() still performs RBAC
	// authorization on top. This closes the gap where a handler missing
	// its authorizeAction() call would have been silently reachable
	// without authentication. See ADR-016.
	if p.config.Auth != nil {
		loginHandler := p.config.Auth.LoginHandler()
		r.Get("/login", router.FromHandler(loginHandler))
		r.Post("/login", router.FromHandler(loginHandler))

		r.Group(func(sub *router.Mux) {
			sub.Use(p.authMiddleware)

			// /api/* — authenticated at the edge here, authorized per
			// handler. They intentionally do not inherit the SPA-only
			// observation middlewares applied in the nested group below
			// (preserving their prior router-layer middleware set, which
			// under the authenticated branch was none).
			p.mountAPIRoutes(sub)

			// SPA fallback keeps its existing middleware stack, now nested
			// under — and inheriting — authMiddleware.
			sub.Group(func(spa *router.Mux) {
				spa.Use(p.tenantContextMiddleware)
				spa.Use(p.auditMiddleware)
				spa.Use(p.sessionActivityMiddleware)
				spa.Use(p.liveTrafficMiddleware)
				spa.Get("/{path...}", p.handleSPA(uiContent))
			})
		})
		return
	}

	// No auth provider configured: a development/test-only posture in which
	// the admin API and UI are fully open. Not reachable via pkg/app or
	// pkg/nucleus (both always set Auth); warn loudly in case a panel was
	// wired by hand. See ADR-016.
	p.warnAdminAuthDisabled()
	r.Use(p.tenantContextMiddleware)
	r.Use(p.auditMiddleware)
	r.Use(p.sessionActivityMiddleware)
	r.Use(p.liveTrafficMiddleware)
	p.mountAPIRoutes(r)
	r.Get("/{path...}", p.handleSPA(uiContent))
}

// mountAPIRoutes registers every /api/* admin endpoint on the given mux.
// In a deployment with an auth provider configured it is invoked inside the
// authMiddleware group (see mountRoutes), so every endpoint is authenticated
// at the router edge; the per-handler authorizeAction() calls add RBAC
// authorization on top.
func (p *Panel) mountAPIRoutes(m *router.Mux) {
	// DEPRECATED (Phase 7): the /api/models/* surface is preserved for
	// backwards compatibility while Data Studio migrates to the new
	// admin observability server (admin/server). New deployments should
	// point operators at the standalone admin server's UI, which calls
	// the typed Connect-RPC DataStudioService instead. These routes
	// will be removed in Phase 8 once every consumer has migrated.
	m.Get("/api/models", p.handleListModels)
	m.Get("/api/models/{name}/schema", p.handleGetSchema)
	m.Put("/api/models/{name}/schema/fields", p.handleUpdateFieldMeta)
	m.Get("/api/models/{name}", p.handleListRecords)
	m.Post("/api/models/{name}", p.handleCreateRecord)
	m.Get("/api/models/{name}/{id}", p.handleGetRecord)
	m.Put("/api/models/{name}/{id}", p.handleUpdateRecord)
	m.Delete("/api/models/{name}/{id}", p.handleDeleteRecord)
	m.Post("/api/models/{name}/bulk", p.handleBulkAction)
	m.Get("/api/models/{name}/export", p.handleExportCSV)
	m.Post("/api/logout", p.handleLogout)
	m.Get("/api/sessions", p.handleListSessions)
	m.Get("/api/live/snapshot", p.handleLiveSnapshot)
	m.Get("/api/live/excludes", p.handleListLiveExcludePatterns)
	m.Post("/api/live/excludes", p.handleAddLiveExcludePattern)
	m.Delete("/api/live/excludes", p.handleDeleteLiveExcludePattern)
	m.Get("/api/live/ws", p.handleLiveWS)
	m.Get("/api/system/snapshot", p.handleSystemSnapshot)
	m.Get("/api/system/flags", p.handleListSystemFlags)
	m.Post("/api/system/flags", p.handleCreateSystemFlag)
	m.Put("/api/system/flags/{name}", p.handleSetSystemFlag)
	m.Delete("/api/system/flags/{name}", p.handleDeleteSystemFlag)
	m.Get("/api/features", p.handleListSystemFlags)
	m.Put("/api/features/{name}", p.handleSetSystemFlag)
	m.Post("/api/system/jobs/queues/{name}/actions/{action}", p.handleSystemQueueAction)

	// RBAC management endpoints
	m.Get("/api/rbac/policies", p.handleListRBACPolicies)
	m.Post("/api/rbac/policies", p.handleAddRBACPolicy)
	m.Delete("/api/rbac/policies", p.handleRemoveRBACPolicy)
	m.Post("/api/rbac/roles/assign", p.handleAssignRBACRole)
	m.Post("/api/rbac/roles/remove", p.handleRemoveRBACRole)
	m.Get("/api/rbac/roles", p.handleGetRBACRoles)
	m.Get("/api/rbac/check", p.handleCheckRBACPermission)

	// Audit log endpoints
	m.Get("/api/audit", p.handleListAuditLog)
	m.Post("/api/audit/clear", p.handleClearAuditLog)

	// Management endpoints
	m.Get("/api/migrations", p.handleListMigrations)
	m.Post("/api/migrations/apply", p.handleApplyMigrations)
	m.Get("/api/health", p.handleHealthCheck)
	m.Get("/api/jobs", p.handleListJobQueues)
	m.Get("/api/sites", p.handleListSites)

	// P2 features
	m.Get("/api/deployment", p.handleDeploymentInfo)
	m.Get("/api/cache", p.handleCacheStats)
	m.Post("/api/cache/flush", p.handleFlushCache)
	m.Get("/api/storage", p.handleListStorage)
	m.Get("/api/email", p.handleEmailStats)

	// Data management
	m.Post("/api/exports", p.handleExportCreate)
	m.Get("/api/exports", p.handleExportList)
	m.Get("/api/exports/{id}", p.handleExportStatus)
	m.Get("/api/exports/{id}/download", p.handleExportDownload)
	m.Post("/api/imports", p.handleImportUpload)
	m.Post("/api/import/validate", p.handleImportValidate)
	m.Post("/api/import/execute", p.handleImportExecute)

	// Fixtures (Django-style dumpdata/loaddata)
	m.Post("/api/fixtures/dumpdata", p.handleDumpdata)
	m.Post("/api/fixtures/loaddata", p.handleLoaddata)
}

// warnAdminAuthDisabled logs a prominent warning that the admin panel is
// being served without an authentication provider, leaving every /admin
// API and UI route publicly reachable. This posture is intended for local
// development and tests only; pkg/app and pkg/nucleus always configure an
// auth provider. See ADR-016.
func (p *Panel) warnAdminAuthDisabled() {
	if p == nil {
		return
	}
	// Fall back to the default logger rather than dropping the warning: the
	// whole point is to be loud about an open admin surface.
	lg := p.logger
	if lg == nil {
		lg = slog.Default()
	}
	lg.Warn("admin panel mounted without an authentication provider; " +
		"all /admin API and UI routes are publicly accessible — configure an " +
		"admin auth provider for any shared or production deployment " +
		"(development/test posture only)")
}

// LiveTrafficMiddleware returns non-blocking runtime observation middleware
// that can be mounted at app-router level to capture traffic outside /admin.
func (p *Panel) LiveTrafficMiddleware() func(http.Handler) http.Handler {
	return p.liveTrafficMiddleware
}

func (p *Panel) handleSPA(fsys fs.FS) router.Handler {
	return func(c *router.Context) error {
		w, r := c.Writer, c.Request
		// If the request is for a JS/CSS asset, serve it directly
		if strings.HasSuffix(r.URL.Path, ".js") || strings.HasSuffix(r.URL.Path, ".css") {
			assetPath := strings.TrimPrefix(r.URL.Path, "/")
			content, err := fs.ReadFile(fsys, assetPath)
			if err == nil {
				if strings.HasSuffix(r.URL.Path, ".js") {
					w.Header().Set("Content-Type", "application/javascript")
				} else {
					w.Header().Set("Content-Type", "text/css")
				}
				_, _ = w.Write(content)
				return nil
			}
		}

		// Otherwise serve index.html for SPA routing
		content, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.Error(w, "admin UI not found", 500)
			return nil
		}

		content = injectAdminPrefix(content, NormalizePrefix(p.config.Prefix))

		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(content))
		return nil
	}
}

func (p *Panel) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := p.config.Auth.Authenticate(r)
		if err != nil {
			if isAdminAPIRequest(r) {
				writeErr(w, r, p.authErrorToDomain(err))
				return
			}
			http.Redirect(w, r, p.adminLoginURL(r), http.StatusFound)
			return
		}
		ctx := context.WithValue(r.Context(), adminAuthContextKey{}, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isAdminAPIRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	path := strings.TrimSpace(r.URL.Path)
	if strings.HasPrefix(path, "/api/") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return true
	}
	accept := strings.ToLower(strings.TrimSpace(r.Header.Get("Accept")))
	return strings.Contains(accept, "application/json")
}

func (p *Panel) adminLoginURL(r *http.Request) string {
	loginPath := NormalizePrefix(p.config.Prefix) + "/login"
	if r == nil || r.URL == nil {
		return loginPath
	}

	nextPath := NormalizePrefix(p.config.Prefix) + r.URL.Path
	if rawQuery := strings.TrimSpace(r.URL.RawQuery); rawQuery != "" {
		nextPath += "?" + rawQuery
	}

	query := url.Values{}
	query.Set("next", nextPath)
	return loginPath + "?" + query.Encode()
}

func (p *Panel) handleLogout(c *router.Context) error {
	r := c.Request
	if p == nil || p.config.Session == nil {
		return fmt.Errorf("admin logout: session manager is not configured")
	}
	if !sessionContextReady(p.config.Session, r.Context()) {
		return fmt.Errorf("admin logout: session context is not available")
	}

	if err := p.config.Session.Destroy(r.Context()); err != nil {
		return fmt.Errorf("admin logout: %w", err)
	}

	p.recordAuditEntry(r, AuditEntry{Action: "logout"})
	return c.JSON(http.StatusOK, map[string]interface{}{
		"logged_out": true,
	})
}

func (p *Panel) tenantContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p == nil {
			next.ServeHTTP(w, r)
			return
		}

		tenantCtx := &TenantContext{
			Enabled:    p.config.MultiTenantEnabled,
			TenantID:   p.config.MultiTenantDefault,
			AutoFilter: p.config.MultiTenantAutoFilter,
		}

		// Extract tenant from request scope (subdomain/header resolution)
		if scope, ok := requestScopeFromContext(r.Context()); ok {
			if scope.Tenant != "" {
				tenantCtx.TenantID = scope.Tenant
			}
		}

		// Allow explicit tenant override via query parameter
		if explicit := requestTenant(r); explicit != "" {
			tenantCtx.TenantID = explicit
		}

		// When multi-tenant is enabled but no tenant specified, use default
		if tenantCtx.Enabled && tenantCtx.TenantID == "" {
			tenantCtx.TenantID = p.config.MultiTenantDefault
		}

		// If tenant is empty string or "all", disable auto-filtering (view all tenants)
		if tenantCtx.TenantID == "" || tenantCtx.TenantID == "all" {
			tenantCtx.AutoFilter = false
			tenantCtx.TenantID = ""
		}

		ctx := context.WithValue(r.Context(), adminTenantCtxKey, tenantCtx)
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
	if ip := auth.ClientIPFromRequest(r); ip != "" {
		p.config.Session.Put(ctx, auth.SessionMetaRemoteIPKey, ip)
	}

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

func defaultDatabaseAlias(cfg PanelConfig) string {
	for _, item := range cfg.Databases {
		if item.IsDefault && strings.TrimSpace(item.Alias) != "" {
			return strings.TrimSpace(item.Alias)
		}
	}
	for _, item := range cfg.Databases {
		if strings.TrimSpace(item.Alias) != "" {
			return strings.TrimSpace(item.Alias)
		}
	}
	for alias := range cfg.DatabaseHandles {
		if trimmed := strings.TrimSpace(alias); trimmed != "" {
			return trimmed
		}
	}
	return "default"
}

func (p *Panel) resolveDatabaseAlias(raw string) (string, error) {
	if p == nil {
		return "default", fmt.Errorf("admin panel not initialized")
	}

	alias := strings.TrimSpace(raw)
	inputProvided := alias != ""
	if alias == "" {
		alias = strings.TrimSpace(p.defaultDBAlias)
		if alias == "" {
			alias = "default"
		}
	}

	if len(p.config.DatabaseHandles) == 0 {
		if p.db == nil {
			return "", fmt.Errorf("database %q is not configured", alias)
		}
		if inputProvided && alias != p.defaultDBAlias {
			return "", fmt.Errorf("database %q is not configured", alias)
		}
		return alias, nil
	}

	if _, ok := p.config.DatabaseHandles[alias]; !ok {
		return "", fmt.Errorf("database %q is not configured", alias)
	}
	return alias, nil
}

func (p *Panel) databaseHandle(alias string) (*db.DB, error) {
	if p == nil {
		return nil, fmt.Errorf("admin panel not initialized")
	}
	if len(p.config.DatabaseHandles) == 0 {
		if p.db == nil {
			return nil, fmt.Errorf("no default database handle available")
		}
		return p.db, nil
	}
	handle, ok := p.config.DatabaseHandles[alias]
	if !ok || handle == nil {
		return nil, fmt.Errorf("database %q handle is not available", alias)
	}
	return handle, nil
}

func (p *Panel) requestDatabaseAlias(r *http.Request) (string, error) {
	if r == nil || r.URL == nil {
		return p.resolveDatabaseAlias("")
	}
	alias := strings.TrimSpace(r.URL.Query().Get("db"))
	if alias == "" {
		alias = strings.TrimSpace(r.URL.Query().Get("database"))
	}
	if alias == "" {
		alias = strings.TrimSpace(r.URL.Query().Get("db_alias"))
	}
	return p.resolveDatabaseAlias(alias)
}

func (p *Panel) sortedDatabaseAliases() []string {
	if p == nil {
		return []string{}
	}
	aliases := make([]string, 0, len(p.config.Databases))
	seen := map[string]struct{}{}
	for _, item := range p.config.Databases {
		alias := strings.TrimSpace(item.Alias)
		if alias == "" {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		aliases = append(aliases, alias)
	}
	if len(aliases) == 0 {
		for alias := range p.config.DatabaseHandles {
			trimmed := strings.TrimSpace(alias)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			aliases = append(aliases, trimmed)
		}
	}
	sort.Strings(aliases)
	defaultAlias := strings.TrimSpace(p.defaultDBAlias)
	if defaultAlias != "" {
		for i, alias := range aliases {
			if alias != defaultAlias {
				continue
			}
			if i > 0 {
				aliases = append([]string{defaultAlias}, append(aliases[:i], aliases[i+1:]...)...)
			}
			break
		}
	}
	if len(aliases) == 0 {
		aliases = append(aliases, p.defaultDBAlias)
	}
	return aliases
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

func (p *Panel) authorizeAction(c *router.Context, modelName, action string) error {
	if p.config.Auth == nil {
		return nil
	}

	user, err := p.authenticatedUser(c.Request)
	if err != nil {
		return p.authErrorToDomain(err)
	}

	// If RBAC enforcer is configured, use it for authorization
	if p.rbac != nil {
		// Superusers bypass policy checks
		if user.IsSuperuser {
			return nil
		}

		resource := "admin:" + modelName
		if p.rbac.Can(user.ID, resource, action) {
			return nil
		}
		if p.rbac.Can(user.Role, resource, action) {
			return nil
		}
		if p.rbac.Can(user.Username, resource, action) {
			return nil
		}
		return authDeniedDomain(modelName, action)
	}

	// Fallback to default auth provider
	if !p.config.Auth.Authorize(user, modelName, action) {
		return authDeniedDomain(modelName, action)
	}
	return nil
}

func (p *Panel) authenticatedUser(r *http.Request) (*auth.User, error) {
	if r != nil {
		if user, ok := r.Context().Value(adminAuthContextKey{}).(*auth.User); ok && user != nil {
			return user, nil
		}
	}
	return p.config.Auth.Authenticate(r)
}

// resolveTenantField returns the tenant column name for a model.
// It caches the result for performance.
func (p *Panel) resolveTenantField(modelName string) string {
	if field, ok := p.tenantFields[modelName]; ok {
		return field
	}

	meta, ok := p.registry.Get(modelName)
	if !ok {
		p.tenantFields[modelName] = ""
		return ""
	}

	// Check for override in config
	if p.config.MultiTenantField != "" {
		p.tenantFields[modelName] = p.config.MultiTenantField
		return p.config.MultiTenantField
	}

	// Auto-detect from model metadata
	field := meta.TenantFieldName()
	p.tenantFields[modelName] = field
	return field
}
