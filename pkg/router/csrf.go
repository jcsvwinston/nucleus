package router

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

// CSRFOptions configures the CSRF protection middleware.
type CSRFOptions struct {
	// ExemptPaths are URL path prefixes that skip CSRF validation (e.g. "/api/").
	ExemptPaths []string
	// CookieName is the name of the CSRF cookie (default: "_csrf").
	CookieName string
	// HeaderName is the HTTP header checked for the token (default: "X-CSRF-Token").
	HeaderName string
	// FormField is the form field name checked for the token (default: "_csrf_token").
	FormField string
	// Secure sets the cookie Secure flag (default: false, should be true in production).
	Secure bool
}

func (o *CSRFOptions) defaults() {
	if o.CookieName == "" {
		o.CookieName = "_csrf"
	}
	if o.HeaderName == "" {
		o.HeaderName = "X-CSRF-Token"
	}
	if o.FormField == "" {
		o.FormField = "_csrf_token"
	}
}

type csrfCtxKey struct{}

// CSRFMiddleware returns middleware that protects against cross-site request forgery.
// It generates a random token stored in a cookie and validates it on state-changing
// methods (POST, PUT, PATCH, DELETE). Safe methods (GET, HEAD, OPTIONS) are exempt.
func CSRFMiddleware(opts CSRFOptions) func(http.Handler) http.Handler {
	opts.defaults()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if the path is exempt
			for _, prefix := range opts.ExemptPaths {
				if strings.HasPrefix(r.URL.Path, prefix) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Ensure a CSRF token cookie exists
			cookie, err := r.Cookie(opts.CookieName)
			if err != nil || cookie.Value == "" {
				token := generateCSRFToken()
				http.SetCookie(w, &http.Cookie{
					Name:     opts.CookieName,
					Value:    token,
					Path:     "/",
					HttpOnly: false, // JS must read this to include in requests
					Secure:   opts.Secure,
					SameSite: http.SameSiteLaxMode,
				})
				cookie = &http.Cookie{Value: token}
			}

			// Safe methods don't need validation
			method := r.Method
			if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Validate token from header or form field
			submitted := r.Header.Get(opts.HeaderName)
			if submitted == "" {
				submitted = r.FormValue(opts.FormField)
			}

			if submitted == "" || submitted != cookie.Value {
				http.Error(w, `{"error":{"code":"CSRF_FAILED","message":"CSRF token missing or invalid"}}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CSRFToken extracts the CSRF token from the request cookie.
// Templates can use this to inject the token into forms.
func CSRFToken(r *http.Request) string {
	cookie, err := r.Cookie("_csrf")
	if err != nil {
		return ""
	}
	return cookie.Value
}

func generateCSRFToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
