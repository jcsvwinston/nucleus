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
// The route is registered because the gate only judges routes that exist
// (ADR-033): an unregistered /admin path would answer 404 whatever the
// allow-list says.
func TestAppNew_DefaultDeny_BootstrapAllowListDoesNotCoverAdmin(t *testing.T) {
	a, err := New(testAppConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	a.Router.Get("/admin/api/models", func(c *router.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "true"})
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/api/models", nil)
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on /admin/api/models (no admin allow row in core), got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAppNew_DefaultDeny_UnknownPathsAnswer404 pins the routing-aware gate
// (ADR-033): the default-deny middleware runs after the route is resolved,
// so a path no route serves answers the mux's 404 — GET or POST, with or
// without a policy row granting it — while a registered route the policy
// does not grant still answers 403. The pre-routing gate used to answer a
// uniform 403 for every unknown path, so a mistyped URL and a missing
// policy row were the same symptom.
func TestAppNew_DefaultDeny_UnknownPathsAnswer404(t *testing.T) {
	cfg := testAppConfig()
	cfg.CSRFEnabled = true // the CSRF gate sits in the same pre-routing chain
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	a.Router.Get("/api/widgets", func(c *router.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "true"})
	})
	a.Router.Post("/api/widgets", func(c *router.Context) error {
		return c.JSON(http.StatusCreated, map[string]string{"ok": "true"})
	})
	// A grant for a path nothing serves: the policy is not what answers.
	if err := a.Authorizer.AddPolicy(authz.BootstrapSubject, "/granted/*", "*"); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	cases := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"unknown GET", http.MethodGet, "/nope", http.StatusNotFound},
		{"unknown POST (CSRF on, no token)", http.MethodPost, "/nope", http.StatusNotFound},
		{"granted by policy but unregistered", http.MethodGet, "/granted/thing", http.StatusNotFound},
		{"registered and denied", http.MethodGet, "/api/widgets", http.StatusForbidden},
		{"registered, denied, POST without a CSRF token", http.MethodPost, "/api/widgets", 419},
		// A method the path is not registered for is a routing miss too:
		// the mux answers 405 with its Allow header, before any gate.
		{"registered path, unregistered method", http.MethodDelete, "/api/widgets", http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			a.Router.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != tc.want {
				t.Fatalf("%s %s: status = %d, want %d; body=%s", tc.method, tc.path, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
