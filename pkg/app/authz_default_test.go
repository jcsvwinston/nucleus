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
// rbac_policy_file gets a 403 on any business route, while the
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
	// caller; WithOpenAuthz only skips the framework-wide middleware mount.
	if a.Authorizer == nil {
		t.Fatal("Authorizer should still be constructed under WithOpenAuthz")
	}
}

// TestAppNew_DefaultDeny_BootstrapAllowListDoesNotCoverAdmin verifies that,
// after the admin clean break, the framework no longer seeds any `/admin`
// allow rows: a business route under `/admin/*` is denied like any other.
func TestAppNew_DefaultDeny_BootstrapAllowListDoesNotCoverAdmin(t *testing.T) {
	a, err := New(testAppConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/admin/api/models", nil)
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on /admin/api/models (no admin allow row in core), got %d body=%s", rec.Code, rec.Body.String())
	}
}
