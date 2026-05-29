package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCORSMiddleware_WildcardWithCredentialsReflectsOrigin is the FW-6
// regression guard: when AllowedOrigins is ["*"] AND AllowCredentials is
// true, the middleware must NOT emit the literal `Access-Control-Allow-Origin: *`
// (browsers reject `*` + credentials). It must reflect the request Origin and
// add `Vary: Origin` instead.
func TestCORSMiddleware_WildcardWithCredentialsReflectsOrigin(t *testing.T) {
	mw := CORSMiddleware(CORSOptions{
		AllowedOrigins:   []string{"*"},
		AllowCredentials: true,
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	req.Header.Set("Origin", "https://app.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("expected reflected origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("expected credentials header true, got %q", got)
	}
	if vary := rec.Header().Get("Vary"); !strings.Contains(vary, "Origin") {
		t.Fatalf("expected Vary to contain Origin, got %q", vary)
	}
}

// TestCORSMiddleware_WildcardWithoutCredentialsEmitsStar confirms the
// credential-less path is unchanged: `*` is still emitted and no Vary/credentials
// headers are set.
func TestCORSMiddleware_WildcardWithoutCredentialsEmitsStar(t *testing.T) {
	mw := CORSMiddleware(CORSOptions{
		AllowedOrigins:   []string{"*"},
		AllowCredentials: false,
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	req.Header.Set("Origin", "https://app.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected wildcard origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("expected no credentials header, got %q", got)
	}
}

// TestCORSMiddleware_AllowListWithCredentialsReflectsOrigin confirms the
// non-wildcard credentialed path still reflects the specific allowed origin
// and sets Vary (behaviour was already correct; this pins it).
func TestCORSMiddleware_AllowListWithCredentialsReflectsOrigin(t *testing.T) {
	mw := CORSMiddleware(CORSOptions{
		AllowedOrigins:   []string{"https://allowed.example"},
		AllowCredentials: true,
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	req.Header.Set("Origin", "https://allowed.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://allowed.example" {
		t.Fatalf("expected reflected allowed origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("expected credentials header true, got %q", got)
	}
	if vary := rec.Header().Get("Vary"); !strings.Contains(vary, "Origin") {
		t.Fatalf("expected Vary to contain Origin, got %q", vary)
	}
}
