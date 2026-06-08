package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// TestWithCORSCredentials_DisabledOmitsHeader proves WithCORSCredentials(false)
// wires through New() → routerOpts → CORSMiddleware (R4 / ADR-013): an
// allow-listed origin is still reflected, but no Access-Control-Allow-Credentials
// header is emitted.
func TestWithCORSCredentials_DisabledOmitsHeader(t *testing.T) {
	logger := observe.NewLogger("error", "text")
	r := New(logger, WithCORSOrigins("https://allowed.example"), WithCORSCredentials(false))
	r.Get("/test", func(c *Context) error {
		c.Writer.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://allowed.example")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://allowed.example" {
		t.Fatalf("expected ACAO for the allowed origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("WithCORSCredentials(false) must omit ACAC, got %q", got)
	}
}

// TestWithCORSCredentials_DefaultOmitsHeader is the SEC-1 guard: credentials are
// OFF by default, so even with an explicit allow-list a router that never calls
// WithCORSCredentials must NOT emit Access-Control-Allow-Credentials.
func TestWithCORSCredentials_DefaultOmitsHeader(t *testing.T) {
	logger := observe.NewLogger("error", "text")
	r := New(logger, WithCORSOrigins("https://allowed.example"))
	r.Get("/test", func(c *Context) error {
		c.Writer.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://allowed.example")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://allowed.example" {
		t.Fatalf("expected ACAO for the allowed origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("credentials are off by default (SEC-1); ACAC must be empty, got %q", got)
	}
}

// TestWithCORSCredentials_ExplicitOptInEmitsHeader confirms the opt-in path still
// works: an explicit allow-list paired with WithCORSCredentials(true) emits ACAC.
func TestWithCORSCredentials_ExplicitOptInEmitsHeader(t *testing.T) {
	logger := observe.NewLogger("error", "text")
	r := New(logger, WithCORSOrigins("https://allowed.example"), WithCORSCredentials(true))
	r.Get("/test", func(c *Context) error {
		c.Writer.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://allowed.example")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Per the Fetch standard, credentials must be paired with a reflected origin,
	// never `*` — guard against a regression that emits both.
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://allowed.example" {
		t.Fatalf("expected ACAO to reflect the explicit allow-list origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("explicit opt-in should emit ACAC: true, got %q", got)
	}
}

// TestCORS_ZeroConfigAllowAllOmitsCredentials is the core SEC-1 scenario: a
// default router (no CORS options) reflects/allows any origin for credential-less
// requests but must NEVER emit credentials — otherwise any site could read
// authenticated cross-origin responses under SameSite=None cookies.
func TestCORS_ZeroConfigAllowAllOmitsCredentials(t *testing.T) {
	logger := observe.NewLogger("error", "text")
	r := New(logger) // zero CORS config → allow-all default
	r.Get("/test", func(c *Context) error {
		c.Writer.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://any-evil.example")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("zero-config allow-all must NOT emit credentials (SEC-1), got ACAC=%q", got)
	}
}
