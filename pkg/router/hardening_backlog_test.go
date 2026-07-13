package router

// Regression guards for the v1.2.0 audit backlog (NU-P1-2, NU-P1-3,
// NU-P1-5, NU-P1-7, NU-P2-1): Bind body cap, Compress Vary header,
// Recoverer header-state awareness, hardened security headers, and
// cookie-prefix validation in the CSRF options.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- NU-P1-2: Bind caps the JSON body ---

func TestBind_RejectsOversizedBody(t *testing.T) {
	big := `{"name":"` + strings.Repeat("a", maxJSONBodyBytes) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(big))

	var dst struct {
		Name string `json:"name"`
	}
	err := Bind(req, &dst)
	if err == nil {
		t.Fatal("expected oversized body to be rejected")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected a payload-too-large error, got %v", err)
	}
}

func TestBind_AcceptsBodyUnderCap(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ok"}`))
	var dst struct {
		Name string `json:"name"`
	}
	if err := Bind(req, &dst); err != nil {
		t.Fatalf("small body must bind, got %v", err)
	}
	if dst.Name != "ok" {
		t.Fatalf("bound value = %q, want ok", dst.Name)
	}
}

func TestBindMax_HonorsCustomCap(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"0123456789"}`))
	var dst struct {
		Name string `json:"name"`
	}
	if err := BindMax(req, &dst, 8); err == nil {
		t.Fatal("expected body over the custom cap to be rejected")
	}
}

// --- NU-P1-3: Compress emits Vary: Accept-Encoding ---

func TestCompress_SetsVaryAcceptEncoding(t *testing.T) {
	h := Compress(5)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))

	// Gzip-accepting client: compressed variant must carry Vary.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("compressed response must vary on Accept-Encoding, got %q", got)
	}

	// Non-gzip client: the uncompressed variant must ALSO carry Vary, or a
	// shared cache would pin one variant for everyone.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("non-gzip client must get identity encoding, got %q", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("identity response must vary on Accept-Encoding, got %q", got)
	}
}

// --- NU-P1-5: Recoverer consults the real header state ---

func TestWrapResponseWriter_TracksWroteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	ww := NewWrapResponseWriter(rec, 1)
	if ww.WroteHeader() {
		t.Fatal("fresh writer must report WroteHeader=false")
	}
	ww.WriteHeader(http.StatusTeapot)
	if !ww.WroteHeader() {
		t.Fatal("explicit WriteHeader must flip WroteHeader")
	}
}

func TestRecoverer_SkipsSecondWriteHeaderAfterMidResponsePanic(t *testing.T) {
	rec := httptest.NewRecorder()
	ww := NewWrapResponseWriter(rec, 1)

	h := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		panic("boom after headers")
	}))
	h.ServeHTTP(ww, httptest.NewRequest(http.MethodGet, "/", nil))

	// The handler's 202 must survive; Recoverer must not stomp a second
	// status line (the wrapper would swallow it, but headerWritten should
	// short-circuit before the attempt).
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (the handler's own header)", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "Internal Server Error") {
		t.Fatalf("500 body must not be appended after headers were sent, got %q", body)
	}
}

func TestRecoverer_Writes500WhenNothingWritten(t *testing.T) {
	rec := httptest.NewRecorder()
	ww := NewWrapResponseWriter(rec, 1)

	h := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom before headers")
	}))
	h.ServeHTTP(ww, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// --- NU-P1-7: hardened security headers ---

func TestSecurityHeaders_EmitsIsolationAndPermissionsPolicies(t *testing.T) {
	h := securityHeaders(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	for header, want := range map[string]string{
		"Permissions-Policy":           "camera=(), microphone=(), geolocation=()",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
}

// --- NU-P1-7: cookie-prefix preconditions on CSRF options ---

func TestCSRFOptions_CookiePrefixRequiresSecure(t *testing.T) {
	if _, err := NewCSRFMiddleware(CSRFOptions{
		CookieName:     "__Host-_csrf",
		InsecureCookie: true,
	}); err == nil {
		t.Fatal("__Host- prefix with InsecureCookie must be rejected")
	}
	if _, err := NewCSRFMiddleware(CSRFOptions{
		CookieName: "__Host-_csrf",
	}); err != nil {
		t.Fatalf("__Host- prefix with Secure cookies must be accepted, got %v", err)
	}
}
