package router

import (
	"crypto/tls"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- H-N3: trusted proxies / RealIP ---

func TestRealIP_UntrustedPeerIgnoresForwardingHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.9:41000"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "5.6.7.8")

	// No trusted proxies: forwarding headers must be ignored (returns "").
	if got := realIPFromRequest(req, newTrustedProxyMatcher(nil)); got != "" {
		t.Fatalf("untrusted peer must ignore forwarding headers, got %q", got)
	}
}

func TestRealIP_TrustedPeerHonorsRightmostUntrustedXFF(t *testing.T) {
	m := newTrustedProxyMatcher([]string{"10.0.0.0/8"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:41000" // trusted proxy
	// Client 9.9.9.9 as seen by the outermost trusted hop 10.0.0.7.
	req.Header.Set("X-Forwarded-For", "9.9.9.9, 10.0.0.7")

	if got := realIPFromRequest(req, m); got != "9.9.9.9" {
		t.Fatalf("trusted peer must return rightmost untrusted XFF entry, got %q", got)
	}
}

func TestRealIP_TrustedPeerFallsBackToXRealIP(t *testing.T) {
	m := newTrustedProxyMatcher([]string{"10.0.0.5"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:41000"
	req.Header.Set("X-Real-IP", "7.7.7.7")

	if got := realIPFromRequest(req, m); got != "7.7.7.7" {
		t.Fatalf("trusted peer with no XFF must fall back to X-Real-IP, got %q", got)
	}
}

// TestRateLimitClientIP_IgnoresSpoofedXFF is the regression guard for H-N3: the
// rate-limit key must not change when a client rotates X-Forwarded-For.
func TestRateLimitClientIP_IgnoresSpoofedXFF(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.23:6001"
	req.Header.Set("X-Forwarded-For", "10.0.0.1")

	if got := clientIP(req); got != "198.51.100.23" {
		t.Fatalf("clientIP must derive from RemoteAddr, not XFF; got %q", got)
	}
}

// --- H-N5: HSTS ---

func TestSecurityHeaders_HSTSOnlyWhenTLSOrForced(t *testing.T) {
	call := func(mw func(http.Handler) http.Handler, tlsConn bool) string {
		h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if tlsConn {
			req.TLS = &tls.ConnectionState{}
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Header().Get("Strict-Transport-Security")
	}

	if got := call(securityHeaders(false), false); got != "" {
		t.Fatalf("plain HTTP without force must not emit HSTS, got %q", got)
	}
	if got := call(securityHeaders(false), true); got != hstsHeaderValue {
		t.Fatalf("TLS request must emit HSTS, got %q", got)
	}
	if got := call(securityHeaders(true), false); got != hstsHeaderValue {
		t.Fatalf("forced HSTS must emit even over plain HTTP, got %q", got)
	}
}

// --- H-N7: CORS allow-all + credentials fail-fast ---

func TestCORS_AllowAllWithCredentialsCoercedSafe(t *testing.T) {
	r := New(discardLogger(), WithCORSOrigins(), WithCORSCredentials(true))
	r.Get("/ping", func(c *Context) error { return c.JSON(http.StatusOK, "pong") })

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("allow-all must never emit credentials, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow-all with credentials coerced off must emit wildcard, not a reflected origin, got %q", got)
	}
}

// --- H-N7: Context.File anti-traversal ---

func TestValidateFilePath_RejectsTraversal(t *testing.T) {
	if _, err := validateFilePath("../../etc/passwd"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
	if _, err := validateFilePath("foo/../../bar"); err == nil {
		t.Fatal("expected embedded traversal to be rejected")
	}

	dir := t.TempDir()
	f := filepath.Join(dir, "ok.txt")
	if err := os.WriteFile(f, []byte("hi"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := validateFilePath(f); err != nil {
		t.Fatalf("clean absolute path must be accepted, got %v", err)
	}
}
