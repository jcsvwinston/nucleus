package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// TestAppNew_DefaultDeny_NoPolicyFile is the headline acceptance test
// for ADR-004: an operator who calls app.New(cfg) without setting
// admin_rbac_policy_file gets a 403 on any business route, while the
// framework-owned bootstrap routes (/healthz, /metrics) still respond
// 200.
func TestAppNew_DefaultDeny_NoPolicyFile(t *testing.T) {
	a, err := New(testAppConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	// Register a synthetic user route. With no policy file, the
	// default-deny middleware must refuse it.
	a.Router.Get("/api/widgets", func(c *router.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "true"})
	})

	// Business route → 403.
	{
		req := httptest.NewRequest(http.MethodGet, "/api/widgets", nil)
		rec := httptest.NewRecorder()
		a.Router.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 on /api/widgets without policy, got %d body=%s", rec.Code, rec.Body.String())
		}
	}

	// Bootstrap route → 200.
	{
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		a.Router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 on /healthz from bootstrap allow-list, got %d body=%s", rec.Code, rec.Body.String())
		}
	}
}

// TestAppNew_DefaultDeny_AllowOpensRoute proves the deny is targeted —
// once the operator adds an `anonymous` allow for a path, that path
// stops returning 403.
func TestAppNew_DefaultDeny_AllowOpensRoute(t *testing.T) {
	a, err := New(testAppConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	a.Router.Get("/api/widgets", func(c *router.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "true"})
	})

	if err := a.Authorizer.AddPolicy(authz.BootstrapSubject, "/api/widgets", "*"); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/widgets", nil)
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after anonymous allow, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAppNew_WithOpenAuthz_BypassesMiddleware verifies the escape
// hatch in ADR-004 §Decision: WithOpenAuthz skips the middleware
// entirely, so every route responds without authorization.
func TestAppNew_WithOpenAuthz_BypassesMiddleware(t *testing.T) {
	a, err := New(testAppConfig(), WithOpenAuthz())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	a.Router.Get("/api/widgets", func(c *router.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "true"})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/widgets", nil)
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 under WithOpenAuthz, got %d body=%s", rec.Code, rec.Body.String())
	}

	// The Enforcer itself is still constructed and available to the
	// caller (e.g. for the admin panel's RBAC paths); WithOpenAuthz
	// only skips the framework-wide middleware mount.
	if a.Authorizer == nil {
		t.Fatal("Authorizer should still be constructed under WithOpenAuthz")
	}
}

// TestAppNew_DefaultDeny_AdminPrefixCustomizable verifies the
// dynamic admin-prefix allow added when the operator overrides
// Config.AdminPrefix. The default `/admin` bootstrap entries would
// not cover a custom prefix; pkg/app adds them explicitly.
func TestAppNew_DefaultDeny_AdminPrefixCustomizable(t *testing.T) {
	cfg := testAppConfig()
	cfg.AdminPrefix = "/backoffice"

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	// The framework default-deny must let the prefix subtree AND the
	// bare prefix (net/http's canonical redirect to /backoffice/)
	// through to admin's own auth middleware.
	for _, path := range []string{"/backoffice/", "/backoffice"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		a.Router.ServeHTTP(rec, req)
		if rec.Code == http.StatusForbidden {
			t.Fatalf("default-deny must not block %s; got 403 body=%s", path, rec.Body.String())
		}
	}
}

// TestAppNew_DefaultDeny_BareAdminPrefixAllowed pins the fix for the
// bug where GET /admin (no trailing slash — the URL the quickstart
// documents) answered 403: the bootstrap allow-list seeded only
// `/admin/*`, and casbin keyMatch does not extend a `prefix/*` pattern
// to the bare prefix, so the canonical redirect at /admin never ran.
func TestAppNew_DefaultDeny_BareAdminPrefixAllowed(t *testing.T) {
	a, err := New(testAppConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	// Bare prefix → must reach the router, never the 403. The router
	// mounts a canonical redirect handler at the bare admin pattern
	// (pkg/router Mount), so anything outside 3xx — including a 404 —
	// means the request was answered by the wrong layer.
	{
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		rec := httptest.NewRecorder()
		a.Router.ServeHTTP(rec, req)
		if rec.Code == http.StatusForbidden {
			t.Fatalf("expected /admin to escape default-deny, got 403 body=%s", rec.Body.String())
		}
		if rec.Code < 300 || rec.Code >= 400 {
			t.Fatalf("expected the router's canonical redirect on bare /admin, got %d body=%s", rec.Code, rec.Body.String())
		}
	}

	// The exact-match row must not overmatch sibling paths.
	{
		req := httptest.NewRequest(http.MethodGet, "/administrator", nil)
		rec := httptest.NewRecorder()
		a.Router.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 on /administrator (default deny), got %d body=%s", rec.Code, rec.Body.String())
		}
	}
}
