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

// TestWithCORSCredentials_DefaultEmitsHeader confirms the historical default
// (credentials enabled) is preserved when WithCORSCredentials is not passed.
func TestWithCORSCredentials_DefaultEmitsHeader(t *testing.T) {
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

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("the default should emit ACAC: true, got %q", got)
	}
}
