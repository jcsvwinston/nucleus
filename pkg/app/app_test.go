package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/openapi"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

func testAppConfig() *Config {
	return &Config{
		Host:            "127.0.0.1",
		Port:            0,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    2 * time.Second,
		IdleTimeout:     5 * time.Second,
		DatabaseDefault: "default",
		Databases: map[string]DatabaseConfig{
			"default": {
				URL:         "sqlite://:memory:",
				MaxOpen:     1,
				MaxIdle:     1,
				MaxLifetime: time.Minute,
			},
		},
		LogLevel:  "error",
		LogFormat: "text",
	}
}

func TestAppNew_NilConfig(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	if !errors.Is(err, ErrNilConfig) {
		t.Fatalf("expected ErrNilConfig, got: %v", err)
	}
}

func TestAppNew_WithoutDefaults_CoreOnly(t *testing.T) {
	a, err := New(testAppConfig(), WithoutDefaults())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.Shutdown(context.Background())

	// Core components should be initialized.
	if a.Config == nil || a.Logger == nil || a.Router == nil || a.DB == nil || a.Models == nil {
		t.Fatal("expected core components to be initialized")
	}
	if a.Session == nil {
		t.Fatal("expected session manager to be initialized")
	}

	// Default subsystems should NOT be initialized.
	if a.Mailer != nil {
		t.Fatal("expected mailer to be nil with WithoutDefaults")
	}
	if a.Storage != nil {
		t.Fatal("expected storage to be nil with WithoutDefaults")
	}
}

func TestAppNew_WithExtensions(t *testing.T) {
	var attached, shutdown bool
	ext := &testExtension{
		name:         "test-ext",
		attachFunc:   func(a *App) error { attached = true; return nil },
		shutdownFunc: func(ctx context.Context) error { shutdown = true; return nil },
	}

	a, err := New(testAppConfig(), WithoutDefaults(), WithExtensions(ext))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !attached {
		t.Fatal("expected extension Attach to be called")
	}

	_ = a.Shutdown(context.Background())
	if !shutdown {
		t.Fatal("expected extension Shutdown to be called")
	}
}

func TestAppNew_WithExtensions_Error(t *testing.T) {
	ext := &testExtension{
		name:         "failing-ext",
		attachFunc:   func(a *App) error { return errors.New("extension init failed") },
		shutdownFunc: func(ctx context.Context) error { return nil },
	}

	_, err := New(testAppConfig(), WithoutDefaults(), WithExtensions(ext))
	if err == nil {
		t.Fatal("expected error from failing extension")
	}
	if !strings.Contains(err.Error(), "failing-ext") {
		t.Fatalf("expected extension name in error, got: %v", err)
	}
}

// testExtension is a test helper implementing the Extension interface.
type testExtension struct {
	name         string
	attachFunc   func(a *App) error
	shutdownFunc func(ctx context.Context) error
}

func (e *testExtension) Name() string                       { return e.name }
func (e *testExtension) Attach(a *App) error                { return e.attachFunc(a) }
func (e *testExtension) Shutdown(ctx context.Context) error { return e.shutdownFunc(ctx) }

func TestAppNew_InitializesCoreComponents(t *testing.T) {
	a, err := New(testAppConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.Shutdown(context.Background())

	if a.Config == nil || a.Logger == nil || a.Router == nil || a.DB == nil || a.Models == nil {
		t.Fatal("expected app core components to be initialized")
	}
	if a.Mailer == nil {
		t.Fatal("expected mailer to be initialized")
	}
	if a.Session == nil {
		t.Fatal("expected session manager to be initialized")
	}
	if err := a.DB.Health(context.Background()); err != nil {
		t.Fatalf("expected DB health to pass, got: %v", err)
	}
}

func TestAppNew_SQLRuntime_InitializesDB(t *testing.T) {
	cfg := testAppConfig()

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.Shutdown(context.Background())

	if a.DB == nil {
		t.Fatal("expected DB to be initialized")
	}
	if a.DB.Engine() != "sql" {
		t.Fatalf("expected db engine sql, got %s", a.DB.Engine())
	}
}

func TestAppRegisterModel(t *testing.T) {
	type User struct {
		ID    uint
		Email string
	}

	a, err := New(testAppConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.Shutdown(context.Background())

	if err := a.RegisterModel(&User{}); err != nil {
		t.Fatalf("RegisterModel failed: %v", err)
	}
	if a.Models.Count() != 1 {
		t.Fatalf("expected 1 model registered, got %d", a.Models.Count())
	}
}

func TestAppShutdown_ReverseHookOrderAndErrorAggregation(t *testing.T) {
	a := &App{Config: testAppConfig()}
	var order []int

	a.OnShutdown(func(context.Context) error {
		order = append(order, 1)
		return nil
	})
	a.OnShutdown(func(context.Context) error {
		order = append(order, 2)
		return errors.New("hook two failed")
	})
	a.OnShutdown(func(context.Context) error {
		order = append(order, 3)
		return nil
	})

	err := a.Shutdown(context.Background())
	if err == nil {
		t.Fatal("expected aggregated shutdown error")
	}
	if !strings.Contains(err.Error(), "hook two failed") {
		t.Fatalf("expected hook error in shutdown error, got: %v", err)
	}

	got := fmt.Sprint(order)
	if got != "[3 2 1]" {
		t.Fatalf("expected reverse hook order [3 2 1], got %s", got)
	}
}

func TestAppMethods_NilReceiver(t *testing.T) {
	var a *App

	if err := a.Run(context.Background()); !errors.Is(err, ErrNilApp) {
		t.Fatalf("Run: expected ErrNilApp, got %v", err)
	}
	if err := a.Shutdown(context.Background()); !errors.Is(err, ErrNilApp) {
		t.Fatalf("Shutdown: expected ErrNilApp, got %v", err)
	}
	if err := a.RegisterModel(&struct{ ID uint }{}); !errors.Is(err, ErrNilApp) {
		t.Fatalf("RegisterModel: expected ErrNilApp, got %v", err)
	}
	if err := a.MountOpenAPIHandler("/openapi.json", openapi.Handler(func() *openapi.Document { return openapi.NewDocument("Test", "0.1.0") })); !errors.Is(err, ErrNilApp) {
		t.Fatalf("MountOpenAPIHandler: expected ErrNilApp, got %v", err)
	}
}

func TestAppRun_NotInitialized(t *testing.T) {
	a := &App{}
	err := a.Run(context.Background())
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("expected ErrNotInitialized, got %v", err)
	}
}

func TestAppMountOpenAPI(t *testing.T) {
	// OpenAPI route mounting is the subject under test; default-deny
	// (ADR-004) would block the /openapi.json route with 403 unless
	// the operator added it to the policy. WithOpenAuthz keeps the
	// test focused.
	a, err := New(testAppConfig(), WithOpenAuthz())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.Shutdown(context.Background())

	docProvider := func() *openapi.Document {
		doc := openapi.NewDocument("Runtime Test API", "0.2.0")
		doc.Info.Description = "Experimental runtime contract"
		doc.Servers = []openapi.Server{{URL: "/"}}
		doc.EnsurePaths()
		doc.Paths["/health"] = openapi.PathItem{
			Get: &openapi.Operation{
				OperationID: "healthCheck",
				Summary:     "Health check",
				Responses: map[string]openapi.Response{
					"200": {Description: "Healthy"},
				},
			},
		}
		return doc
	}

	if err := a.MountOpenAPIHandler("/openapi.json", openapi.Handler(docProvider)); err != nil {
		t.Fatalf("MountOpenAPIHandler failed: %v", err)
	}
	if err := a.MountOpenAPIHandler("/openapi.json", openapi.Handler(docProvider)); err != nil {
		t.Fatalf("MountOpenAPIHandler should be idempotent: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected json content type, got %q", got)
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode runtime openapi document failed: %v", err)
	}
	info, ok := doc["info"].(map[string]any)
	if !ok || info["title"] != "Runtime Test API" {
		t.Fatalf("unexpected runtime info payload: %#v", doc["info"])
	}
	if _, ok := doc["servers"].([]any); !ok {
		t.Fatalf("expected servers array in runtime document: %#v", doc["servers"])
	}
}

func TestAppRun_ContextCancel(t *testing.T) {
	a, err := New(testAppConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(120 * time.Millisecond)
		cancel()
	}()

	if err := a.Run(ctx); err != nil {
		t.Fatalf("Run should exit cleanly on context cancel: %v", err)
	}
}

func TestAppRun_InvalidAddress(t *testing.T) {
	cfg := testAppConfig()
	cfg.Port = -1 // produces an invalid host:port address

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating app: %v", err)
	}
	defer a.Shutdown(context.Background())

	err = a.Run(context.Background())
	if err == nil {
		t.Fatal("expected run error for invalid server address")
	}
}

func TestAppNew_InvalidMailDriver(t *testing.T) {
	cfg := testAppConfig()
	cfg.MailDriver = "unknown-provider"

	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for unknown mail driver")
	}
	if !strings.Contains(err.Error(), "unknown mail driver") {
		t.Fatalf("expected unknown mail driver error, got %v", err)
	}
}

func TestAppNew_SessionStoreRedisRequiresURL(t *testing.T) {
	cfg := testAppConfig()
	cfg.SessionStore = "redis"
	cfg.RedisURL = ""
	cfg.SessionRedisURL = ""

	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected redis session store config error")
	}
	if !strings.Contains(err.Error(), "session_store=redis requires") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppNew_UnsupportedSessionStore(t *testing.T) {
	cfg := testAppConfig()
	cfg.SessionStore = "unknown-store"

	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected unsupported session store error")
	}
	if !strings.Contains(err.Error(), "unsupported session_store") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppNew_SQLSessionStorePersistsAcrossRequests(t *testing.T) {
	cfg := testAppConfig()
	cfg.SessionStore = "sql"
	cfg.SessionTable = "nucleus_sessions"

	// Session-persistence behaviour is the subject under test; the
	// default-deny middleware (ADR-004) would block the synthetic /set
	// handler with 403. WithOpenAuthz removes that gate explicitly.
	a, err := New(cfg, WithOpenAuthz())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.Shutdown(context.Background())

	a.Router.Get("/set", func(c *router.Context) error {
		a.Session.Put(c.Request.Context(), "name", "alice")
		return c.NoContent()
	})
	a.Router.Get("/get", func(c *router.Context) error {
		_, _ = c.Writer.Write([]byte(a.Session.GetString(c.Request.Context(), "name")))
		return nil
	})

	setReq := httptest.NewRequest(http.MethodGet, "/set", nil)
	setRec := httptest.NewRecorder()
	a.Router.ServeHTTP(setRec, setReq)
	if setRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from /set, got %d", setRec.Code)
	}

	var sessionCookie *http.Cookie
	for _, c := range setRec.Result().Cookies() {
		if c.Name == cfg.SessionCookieName || (cfg.SessionCookieName == "" && c.Name == "session") {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie to be set")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/get", nil)
	getReq.AddCookie(sessionCookie)
	getRec := httptest.NewRecorder()
	a.Router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /get, got %d", getRec.Code)
	}
	if strings.TrimSpace(getRec.Body.String()) != "alice" {
		t.Fatalf("expected persisted session value alice, got %q", getRec.Body.String())
	}

	sqlDB, err := a.DB.SqlDB()
	if err != nil {
		t.Fatalf("sql db handle: %v", err)
	}

	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM "nucleus_sessions"`).Scan(&count); err != nil {
		t.Fatalf("count sessions failed: %v", err)
	}
	if count < 1 {
		t.Fatalf("expected at least 1 persisted session row, got %d", count)
	}
}

func TestOutboxFlavorForDatabaseURL(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "sqlite://:memory:", want: "sqlite"},
		{raw: "postgres://user:pass@localhost/db", want: "postgres"},
		{raw: "postgresql://user:pass@localhost/db", want: "postgres"},
		{raw: "mysql://user:pass@tcp(localhost:3306)/db", want: "mysql"},
	}
	for _, tt := range tests {
		if got := string(outboxFlavorForDatabaseURL(tt.raw)); got != tt.want {
			t.Fatalf("outboxFlavorForDatabaseURL(%q)=%q; want %q", tt.raw, got, tt.want)
		}
	}
}

func TestAppNew_OpensMultipleDatabaseAliases(t *testing.T) {
	cfg := testAppConfig()
	cfg.DatabaseDefault = "primary"
	cfg.Databases = map[string]DatabaseConfig{
		"primary": {
			URL: "sqlite://:memory:",
		},
		"analytics": {
			URL: "sqlite://:memory:",
		},
	}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.Shutdown(context.Background())

	if got := a.DefaultDatabaseAlias(); got != "primary" {
		t.Fatalf("expected default alias primary, got %s", got)
	}
	if len(a.DBs) != 2 {
		t.Fatalf("expected 2 database aliases, got %d", len(a.DBs))
	}
	analytics, err := a.Database("analytics")
	if err != nil {
		t.Fatalf("resolve analytics db: %v", err)
	}
	if analytics == nil {
		t.Fatal("expected analytics db handle")
	}
	if _, err := a.Database("missing"); !errors.Is(err, ErrDatabaseAliasNotFound) {
		t.Fatalf("expected ErrDatabaseAliasNotFound, got %v", err)
	}
}

func TestAppDatabaseForRequest_UsesTenantDatabaseAlias(t *testing.T) {
	cfg := testAppConfig()
	cfg.DatabaseDefault = "default"
	cfg.Databases = map[string]DatabaseConfig{
		"default":      {URL: "sqlite://:memory:"},
		"tenant_acme":  {URL: "sqlite://:memory:"},
		"tenant_omega": {URL: "sqlite://:memory:"},
	}
	cfg.MultiSite = MultiSiteConfig{
		Enabled:     true,
		DefaultSite: "main",
		Sites: map[string]SiteConfig{
			"main": {
				Hosts:                       []string{"*.site.com"},
				Database:                    "default",
				TenantDatabaseAliasTemplate: "tenant_%s",
			},
		},
	}
	cfg.MultiTenant = MultiTenantConfig{
		Enabled:  true,
		Resolver: "subdomain",
		Tenants:  map[string]TenantConfig{},
	}

	// Tenant-routing behaviour is the subject under test; default-deny
	// (ADR-004) would block the synthetic /scope handler with 403.
	a, err := New(cfg, WithOpenAuthz())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.Shutdown(context.Background())

	a.Router.Get("/scope", func(c *router.Context) error {
		scope, ok := RequestScopeFromContext(c.Request.Context())
		if !ok {
			return errors.New("scope missing")
		}
		if _, err := a.DatabaseForRequest(c.Request); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
		}
		_, _ = c.Writer.Write([]byte(scope.Site + "|" + scope.Tenant + "|" + scope.DatabaseAlias))
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/scope", nil)
	req.Host = "acme.site.com"
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "main|acme|tenant_acme" {
		t.Fatalf("unexpected scope payload: %s", rec.Body.String())
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/scope", nil)
	missingReq.Host = "unknown.site.com"
	missingRec := httptest.NewRecorder()
	a.Router.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing tenant alias, got %d", missingRec.Code)
	}
	if !strings.Contains(missingRec.Body.String(), "database alias not found") {
		t.Fatalf("expected missing alias error, got %s", missingRec.Body.String())
	}
}

func TestAppNew_MultiTenantIsolationRejectsSharedDatabaseAlias(t *testing.T) {
	cfg := testAppConfig()
	cfg.DatabaseDefault = "default"
	cfg.Databases = map[string]DatabaseConfig{
		"default": {URL: "sqlite://:memory:"},
		"shared":  {URL: "sqlite://:memory:"},
	}
	cfg.MultiSite = MultiSiteConfig{
		Enabled:     true,
		DefaultSite: "main",
		Sites: map[string]SiteConfig{
			"main": {Database: "default"},
		},
	}
	cfg.MultiTenant = MultiTenantConfig{
		Enabled:           true,
		RequireIsolatedDB: true,
		Tenants: map[string]TenantConfig{
			"tenant_a": {Site: "main", Database: "shared"},
			"tenant_b": {Site: "main", Database: "shared"},
		},
	}

	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected multitenant isolation validation error")
	}
	if !strings.Contains(err.Error(), "share database alias") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppNew_TenantIsolationRequiresTenantAwareTemplate(t *testing.T) {
	cfg := testAppConfig()
	cfg.DatabaseDefault = "default"
	cfg.Databases = map[string]DatabaseConfig{
		"default": {URL: "sqlite://:memory:"},
	}
	cfg.MultiSite = MultiSiteConfig{
		Enabled:     true,
		DefaultSite: "main",
		Sites: map[string]SiteConfig{
			"main": {
				Hosts:    []string{"*.site.com"},
				Database: "default",
			},
		},
	}
	cfg.MultiTenant = MultiTenantConfig{
		Enabled:               true,
		Resolver:              "subdomain",
		RequireIsolatedDB:     true,
		DatabaseAliasTemplate: "tenant_shared",
	}

	a, err := New(cfg)
	if err == nil {
		_ = a.Shutdown(context.Background())
		t.Fatal("expected New to fail when no tenant-isolated template is provided")
	}
	if !strings.Contains(err.Error(), "database_alias_template") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSiteFromContext(t *testing.T) {
	t.Run("with valid scope", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), requestScopeCtxKey{}, RequestScope{Site: "main"})
		if got := SiteFromContext(ctx); got != "main" {
			t.Errorf("expected main, got %s", got)
		}
	})

	t.Run("without scope", func(t *testing.T) {
		if got := SiteFromContext(context.Background()); got != "" {
			t.Errorf("expected empty string, got %s", got)
		}
	})
}

func TestTenantFromContext(t *testing.T) {
	t.Run("with valid scope", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), requestScopeCtxKey{}, RequestScope{Tenant: "tenant1"})
		if got := TenantFromContext(ctx); got != "tenant1" {
			t.Errorf("expected tenant1, got %s", got)
		}
	})

	t.Run("without scope", func(t *testing.T) {
		if got := TenantFromContext(context.Background()); got != "" {
			t.Errorf("expected empty string, got %s", got)
		}
	})
}

func TestDatabaseAliasFromContext(t *testing.T) {
	t.Run("with valid scope", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), requestScopeCtxKey{}, RequestScope{DatabaseAlias: "analytics"})
		if got := DatabaseAliasFromContext(ctx); got != "analytics" {
			t.Errorf("expected analytics, got %s", got)
		}
	})

	t.Run("without scope", func(t *testing.T) {
		if got := DatabaseAliasFromContext(context.Background()); got != "" {
			t.Errorf("expected empty string, got %s", got)
		}
	})
}
