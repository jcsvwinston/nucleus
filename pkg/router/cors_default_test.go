package router

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCORSDefault_UnconfiguredDenies pins the v1.0.0 default (ADR-013 R4 /
// DEP-2026-007): a router constructed without any CORS option emits NO CORS
// headers for a cross-origin request — deny is the default posture.
func TestCORSDefault_UnconfiguredDenies(t *testing.T) {
	r := New(slog.Default())
	r.Get("/ping", func(c *Context) error { return c.JSON(http.StatusOK, "pong") })

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unconfigured router must not emit Access-Control-Allow-Origin, got %q", got)
	}
}

// TestCORSDefault_ExplicitEmptyAllowsAll pins the documented WithCORSOrigins
// semantics, unchanged by the v1.0.0 flip: an explicit call with an empty
// list allows all origins.
func TestCORSDefault_ExplicitEmptyAllowsAll(t *testing.T) {
	r := New(slog.Default(), WithCORSOrigins())
	r.Get("/ping", func(c *Context) error { return c.JSON(http.StatusOK, "pong") })

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://app.example")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("WithCORSOrigins() (explicit empty) must allow all origins, got %q", got)
	}
}

// TestCORSDefault_ExplicitWildcardAllowsAll pins the migration escape hatch:
// cors_origins: ["*"] reproduces the pre-v1.0.0 allow-all behavior exactly.
func TestCORSDefault_ExplicitWildcardAllowsAll(t *testing.T) {
	r := New(slog.Default(), WithCORSOrigins("*"))
	r.Get("/ping", func(c *Context) error { return c.JSON(http.StatusOK, "pong") })

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://app.example")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf(`WithCORSOrigins("*") must allow all origins, got %q`, got)
	}
}

// TestCORSDefault_AllowListRestricts pins the allow-list path: a listed
// origin is reflected, an unlisted one gets nothing.
func TestCORSDefault_AllowListRestricts(t *testing.T) {
	r := New(slog.Default(), WithCORSOrigins("https://app.example"))
	r.Get("/ping", func(c *Context) error { return c.JSON(http.StatusOK, "pong") })

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://app.example")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("listed origin must be allowed, got %q", got)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req2.Header.Set("Origin", "https://other.example")
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if got := rec2.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unlisted origin must get no CORS headers, got %q", got)
	}
}
