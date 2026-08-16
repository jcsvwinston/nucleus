// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Regression tests for QCD-FW-1 (quantum-coverage-demo): the global
// default-deny authorization layer could never see JWT claims. Nothing
// decoded the bearer before buildDefaultAuthzMiddleware ran (builder
// middleware applies after app.New, and module JWT middleware runs inside
// the global enforcement), so every subject was `anonymous` and the
// role-based CSV policies AUTH_GUIDE documents were unreachable at the
// global layer — consumers had to punch an anonymous gate row and
// re-implement RBAC per module.
//
// Contract fixed here: when the app has JWT signing material configured,
// a valid bearer is decoded before global enforcement and the request is
// allowed if ANY of these subjects passes: the token's user id, the
// token's role, or `anonymous` (so an authenticated caller never loses
// access the bootstrap allow-list grants everyone).
package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/router"
)

func newJWTAuthzApp(t *testing.T) *App {
	t.Helper()
	cfg := testAppConfig()
	cfg.JWTSecret = "qcd-fw-1-test-secret-0123456789abcdef"

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	if a.JWT == nil {
		t.Fatal("App.JWT is nil despite jwt_secret being configured")
	}

	a.Router.Get("/api/admin/reports", func(c *router.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "true"})
	})
	a.Router.Get("/api/mine", func(c *router.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "true"})
	})
	return a
}

func bearerGet(t *testing.T, a *App, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	return rec
}

// A CSV-style role policy (`p, admin, /api/admin/*, read, allow`) must open
// the route for a bearer whose token carries role=admin — the exact
// composition AUTH_GUIDE documents for default-deny with JWT roles.
func TestGlobalAuthzSeesRoleClaim(t *testing.T) {
	a := newJWTAuthzApp(t)
	if err := a.Authorizer.AddPolicy("admin", "/api/admin/*", "read"); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	token, err := a.JWT.Generate("user123", "alice", "admin")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if rec := bearerGet(t, a, "/api/admin/reports", token); rec.Code != http.StatusOK {
		t.Fatalf("role-based policy unreachable at the global layer: got %d body=%s", rec.Code, rec.Body.String())
	}
	// Without a token the same route must stay denied.
	if rec := bearerGet(t, a, "/api/admin/reports", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("anonymous must stay denied: got %d body=%s", rec.Code, rec.Body.String())
	}
}

// A per-user policy keyed by the token's user id must also work.
func TestGlobalAuthzSeesUserIDClaim(t *testing.T) {
	a := newJWTAuthzApp(t)
	if err := a.Authorizer.AddPolicy("user123", "/api/mine", "read"); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	token, err := a.JWT.Generate("user123", "alice", "viewer")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if rec := bearerGet(t, a, "/api/mine", token); rec.Code != http.StatusOK {
		t.Fatalf("user-id policy unreachable at the global layer: got %d body=%s", rec.Code, rec.Body.String())
	}

	// A different user's token must not open it.
	other, err := a.JWT.Generate("user999", "mallory", "viewer")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if rec := bearerGet(t, a, "/api/mine", other); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign user id must stay denied: got %d body=%s", rec.Code, rec.Body.String())
	}
}

// An authenticated caller must never LOSE what the bootstrap allow-list
// grants everyone: a bearer on /healthz keeps responding 200 even though
// the resolved subject is no longer `anonymous`.
func TestGlobalAuthzBearerKeepsAnonymousGrants(t *testing.T) {
	a := newJWTAuthzApp(t)
	token, err := a.JWT.Generate("user123", "alice", "viewer")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if rec := bearerGet(t, a, "/healthz", token); rec.Code != http.StatusOK {
		t.Fatalf("bearer must not lose bootstrap grants on /healthz: got %d body=%s", rec.Code, rec.Body.String())
	}
}
