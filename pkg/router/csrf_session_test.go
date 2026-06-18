package router

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

// runSessionCSRFFlow mounts session-mode CSRFMiddleware as GROUP (module-style)
// middleware — the fleetdesk finding #27 shape — with the session LoadAndSave
// mounted globally, then drives GET /form (which embeds CSRFToken) → POST
// /submit (carrying the token + session cookie). Returns ("", -1) when the GET
// render produced no token (no POST is sent); otherwise (token, POST status).
func runSessionCSRFFlow(t *testing.T, sessionKey string) (string, int) {
	t.Helper()
	sm := auth.NewSessionManager(auth.SessionConfig{})
	mux := NewMux()
	mux.SetSessionManager(sm)
	mux.Use(sm.Middleware()) // global LoadAndSave (outermost)

	csrf := CSRFMiddleware(CSRFOptions{UseSessionToken: true, SessionKey: sessionKey})
	mux.Group(func(sub *Mux) {
		sub.Use(csrf) // module-style group middleware
		sub.Get("/form", func(c *Context) error {
			_, err := c.Writer.Write([]byte(CSRFToken(c.Request)))
			return err
		})
		sub.Post("/submit", func(c *Context) error {
			_, err := c.Writer.Write([]byte("ok"))
			return err
		})
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/form", nil))
	token := strings.TrimSpace(rec.Body.String())
	if token == "" {
		return "", -1
	}

	form := url.Values{"_csrf_token": {token}}
	req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, ck := range rec.Result().Cookies() {
		req.AddCookie(ck)
	}
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req)
	return token, rec2.Code
}

// TestCSRFSessionMode_FromGroupMiddleware is the regression guard for fleetdesk
// finding #27: session-mode CSRF works from a module/group middleware position.
// injectDependencies wraps (is OUTER of) group middleware, so the session is in
// context when the CSRF middleware runs — the finding's "middleware never sees
// the session" diagnosis was incorrect; the real bug was the CSRFToken/
// SessionKey coupling (see TestCSRFToken_HonorsCustomSessionKey).
func TestCSRFSessionMode_FromGroupMiddleware(t *testing.T) {
	token, status := runSessionCSRFFlow(t, "") // default SessionKey ("csrf_token")
	if token == "" {
		t.Fatal("CSRFToken empty on first render — session-mode CSRF unavailable to group middleware")
	}
	if status != http.StatusOK {
		t.Fatalf("POST with session CSRF token: want 200, got %d", status)
	}
}

// TestCSRFToken_HonorsCustomSessionKey pins the actual #27 fix: with a CUSTOM
// SessionKey, CSRFToken used to return "" (it hardcoded "csrf_token" while the
// middleware stored under opts.SessionKey), breaking first-render embedding.
// The middleware now injects the resolved token into the request context and
// CSRFToken reads it, so a custom key round-trips end to end.
func TestCSRFToken_HonorsCustomSessionKey(t *testing.T) {
	token, status := runSessionCSRFFlow(t, "my_app_csrf")
	if token == "" {
		t.Fatal("CSRFToken empty with a custom SessionKey — the #27 coupling bug is back")
	}
	if status != http.StatusOK {
		t.Fatalf("POST with custom-key session CSRF token: want 200, got %d", status)
	}
}

// TestCSRFToken_AvailableOnSameOriginShortcut guards the origin-check path: when
// EnableOriginCheck allows a same-origin request immediately (Layer 1
// short-circuit), the token is still injected into context first, so a
// same-origin GET that renders a form gets it — even with a custom SessionKey.
// Before the fix the origin shortcut returned before token resolution, so
// CSRFToken fell back to the hard-coded key and returned "".
func TestCSRFToken_AvailableOnSameOriginShortcut(t *testing.T) {
	sm := auth.NewSessionManager(auth.SessionConfig{})
	mux := NewMux()
	mux.SetSessionManager(sm)
	mux.Use(sm.Middleware())

	csrf := CSRFMiddleware(CSRFOptions{
		UseSessionToken:   true,
		SessionKey:        "my_app_csrf",
		EnableOriginCheck: true,
	})
	mux.Group(func(sub *Mux) {
		sub.Use(csrf)
		sub.Get("/form", func(c *Context) error {
			_, err := c.Writer.Write([]byte(CSRFToken(c.Request)))
			return err
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/form", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin") // triggers the Layer 1 shortcut
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if got := strings.TrimSpace(rec.Body.String()); got == "" {
		t.Fatal("CSRFToken empty on a same-origin GET with EnableOriginCheck — the origin shortcut skipped token injection")
	}
}
