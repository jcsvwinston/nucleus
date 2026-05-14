package router

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

// csrfEncryptionKeySize is the required EncryptionKey length when
// EnableXSRFCookie is set — 32 bytes for AES-256.
const csrfEncryptionKeySize = 32

// ErrCSRFEncryptionKey is returned by NewCSRFMiddleware when
// EnableXSRFCookie is true but EncryptionKey is not exactly
// csrfEncryptionKeySize bytes. See ADR-006.
var ErrCSRFEncryptionKey = errors.New("router: CSRFOptions.EncryptionKey must be exactly 32 bytes (AES-256) when EnableXSRFCookie is set")

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

	// Origin verification options (Laravel-style two-layer approach)
	EnableOriginCheck bool // Enable Sec-Fetch-Site header verification (zero value: false; router.WithCSRF sets it true)
	OriginOnly        bool // Use only origin verification, disable token fallback (zero value: false)
	AllowSameSite     bool // Allow same-site requests in addition to same-origin (zero value: false)

	// Session-based token storage (more secure than cookie)
	UseSessionToken bool   // Store token in session instead of cookie (default: false)
	SessionKey      string // Session key for token storage (default: "csrf_token")

	// X-XSRF-TOKEN encrypted cookie for JavaScript frameworks
	EnableXSRFCookie bool   // Enable encrypted XSRF-TOKEN cookie for JS frameworks (default: false)
	XSRFCookieName   string // XSRF-TOKEN cookie name (default: "XSRF-TOKEN")
	EncryptionKey    string // AES key for encrypting XSRF-TOKEN (32 bytes for AES-256)

	// Token rotation
	RotateToken bool // Regenerate token after each successful validation (default: false)
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
	if o.SessionKey == "" {
		o.SessionKey = "csrf_token"
	}
	if o.XSRFCookieName == "" {
		o.XSRFCookieName = "XSRF-TOKEN"
	}
	// EncryptionKey is intentionally NOT defaulted. Deriving it from the
	// cookie name (the historical behaviour) produced a globally-
	// predictable AES key — see ADR-006. When EnableXSRFCookie is set the
	// key is now mandatory and validated by validate().
}

// validate checks the options for misconfigurations that must fail loud
// rather than degrade silently. Currently the only such check is the
// EncryptionKey requirement when EnableXSRFCookie is set (ADR-006).
func (o *CSRFOptions) validate() error {
	if o.EnableXSRFCookie && len(o.EncryptionKey) != csrfEncryptionKeySize {
		return fmt.Errorf("%w (got %d bytes)", ErrCSRFEncryptionKey, len(o.EncryptionKey))
	}
	return nil
}

// NewCSRFMiddleware builds the CSRF protection middleware, returning an
// error on a misconfiguration instead of panicking. Use this constructor
// when the caller wants to surface configuration errors through its own
// validation path; use CSRFMiddleware for the panic-on-misconfiguration
// (regexp.MustCompile-style) variant.
//
// The error case today is EnableXSRFCookie set without a 32-byte
// EncryptionKey — see ADR-006.
func NewCSRFMiddleware(opts CSRFOptions) (func(http.Handler) http.Handler, error) {
	opts.defaults()
	if err := opts.validate(); err != nil {
		return nil, err
	}
	return buildCSRFMiddleware(opts), nil
}

// CSRFMiddleware returns middleware that protects against cross-site request forgery.
// It implements a two-layer approach (Laravel-style):
// 1. Origin verification via Sec-Fetch-Site header (if enabled)
// 2. Traditional CSRF token validation as fallback
//
// Features:
// - Origin verification for modern browsers
// - Session-based or cookie-based token storage
// - Encrypted X-XSRF-TOKEN cookie for JavaScript frameworks
// - Token rotation for enhanced security
// - Configurable origin-only mode and same-site allowance
//
// CSRFMiddleware panics on a misconfiguration (the regexp.MustCompile
// pattern) — a bad CSRF config is a deployment error that should crash
// the process at startup, not serve requests with a weak key. The panic
// fires once, at middleware-chain construction, never on the request
// path. Use NewCSRFMiddleware for a non-panicking, error-returning
// alternative. See ADR-006.
func CSRFMiddleware(opts CSRFOptions) func(http.Handler) http.Handler {
	mw, err := NewCSRFMiddleware(opts)
	if err != nil {
		panic("router.CSRFMiddleware: " + err.Error())
	}
	return mw
}

func buildCSRFMiddleware(opts CSRFOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if the path is exempt
			for _, prefix := range opts.ExemptPaths {
				if strings.HasPrefix(r.URL.Path, prefix) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Layer 1: Origin verification (Laravel-style)
			if opts.EnableOriginCheck {
				if isSameOrigin(r) {
					// Same-origin: allow immediately
					next.ServeHTTP(w, r)
					return
				}
				if opts.AllowSameSite && isSameSite(r) {
					// Same-site allowed: allow immediately
					next.ServeHTTP(w, r)
					return
				}
				// If origin-only mode and verification failed, reject
				if opts.OriginOnly {
					http.Error(w, `{"error":{"code":"ORIGIN_VERIFICATION_FAILED","message":"Request origin verification failed"}}`, http.StatusForbidden)
					return
				}
			}

			// Get or generate CSRF token
			var token string
			var tokenSource string // "session" or "cookie"

			if opts.UseSessionToken {
				// Session-based token storage
				sess := getSessionFromContext(r)
				if sess != nil {
					token = getSessionToken(sess, r, opts.SessionKey)
					if token == "" {
						token = generateCSRFToken()
						setSessionToken(sess, r, opts.SessionKey, token)
					}
					tokenSource = "session"
				} else {
					// Fallback to cookie if session not available
					token = getCookieToken(r, opts.CookieName)
					if token == "" {
						token = generateCSRFToken()
						setCSRFCookie(w, opts, token)
					}
					tokenSource = "cookie"
				}
			} else {
				// Cookie-based token storage (original behavior)
				token = getCookieToken(r, opts.CookieName)
				if token == "" {
					token = generateCSRFToken()
					setCSRFCookie(w, opts, token)
				}
				tokenSource = "cookie"
			}

			// Set X-XSRF-TOKEN encrypted cookie for JavaScript frameworks
			if opts.EnableXSRFCookie {
				encryptedToken, err := encryptToken(token, opts.EncryptionKey)
				if err == nil {
					http.SetCookie(w, &http.Cookie{
						Name:     opts.XSRFCookieName,
						Value:    encryptedToken,
						Path:     "/",
						HttpOnly: false,
						Secure:   opts.Secure,
						SameSite: http.SameSiteLaxMode,
					})
				}
			}

			// Safe methods don't need validation
			method := r.Method
			if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Layer 2: Token validation
			submitted := r.Header.Get(opts.HeaderName)
			// The X-XSRF-TOKEN header is only meaningful when the encrypted
			// XSRF cookie feature is enabled — that is the only path that
			// issues such a token. When EnableXSRFCookie is false the
			// header is ignored entirely rather than treated as a raw
			// candidate token (which would always fail the compare anyway,
			// but reading it would be needlessly confusing).
			if submitted == "" && opts.EnableXSRFCookie {
				if encrypted := r.Header.Get("X-XSRF-TOKEN"); encrypted != "" {
					// Decrypt X-XSRF-TOKEN. A decrypt failure (tampered or
					// malformed header) leaves submitted empty, so the
					// request falls through to the form-field check and is
					// ultimately rejected by the constant-time compare.
					if decrypted, err := decryptToken(encrypted, opts.EncryptionKey); err == nil {
						submitted = decrypted
					}
				}
			}
			if submitted == "" {
				submitted = r.FormValue(opts.FormField)
			}

			// Constant-time comparison against the expected token. The
			// `submitted == ""` guard is a non-constant-time pre-check —
			// it only tests whether the client sent anything at all and
			// leaks nothing about the secret. The secret-bearing
			// comparison itself is constant-time so the response latency
			// does not reveal how many leading bytes an attacker guessed
			// correctly. ConstantTimeCompare returns 1 when the byte
			// slices are equal, 0 otherwise. See ADR-006.
			if submitted == "" || subtle.ConstantTimeCompare([]byte(submitted), []byte(token)) != 1 {
				// OriginOnly mode answers 403; token-fallback mode uses
				// Laravel's 419 ("CSRF token mismatch").
				statusCode := 419
				if opts.OriginOnly {
					statusCode = http.StatusForbidden
				}
				http.Error(w, `{"error":{"code":"CSRF_FAILED","message":"CSRF token missing or invalid"}}`, statusCode)
				return
			}

			// Token rotation: regenerate after successful validation
			if opts.RotateToken {
				newToken := generateCSRFToken()
				if tokenSource == "session" {
					sess := getSessionFromContext(r)
					if sess != nil {
						setSessionToken(sess, r, opts.SessionKey, newToken)
					}
				} else {
					setCSRFCookie(w, opts, newToken)
				}
				// Update X-XSRF-TOKEN if enabled
				if opts.EnableXSRFCookie {
					encryptedToken, err := encryptToken(newToken, opts.EncryptionKey)
					if err == nil {
						http.SetCookie(w, &http.Cookie{
							Name:     opts.XSRFCookieName,
							Value:    encryptedToken,
							Path:     "/",
							HttpOnly: false,
							Secure:   opts.Secure,
							SameSite: http.SameSiteLaxMode,
						})
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CSRFToken extracts the CSRF token from the request cookie or session.
// Templates can use this to inject the token into forms.
func CSRFToken(r *http.Request) string {
	// Try cookie first
	cookie, err := r.Cookie("_csrf")
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// Try session
	sess := getSessionFromContext(r)
	if sess != nil {
		return getSessionToken(sess, r, "csrf_token")
	}

	return ""
}

// Helper functions for origin verification
func isSameOrigin(r *http.Request) bool {
	return r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

func isSameSite(r *http.Request) bool {
	return r.Header.Get("Sec-Fetch-Site") == "same-site"
}

// Helper functions for session-based token storage
func getSessionFromContext(r *http.Request) *auth.SessionManager {
	if sess, ok := r.Context().Value(sessionKey).(*auth.SessionManager); ok {
		return sess
	}
	return nil
}

func getSessionToken(sess *auth.SessionManager, r *http.Request, key string) string {
	if sess == nil {
		return ""
	}
	return sess.GetString(r.Context(), key)
}

func setSessionToken(sess *auth.SessionManager, r *http.Request, key, value string) {
	if sess != nil {
		sess.Put(r.Context(), key, value)
	}
}

// Helper functions for cookie-based token storage
func getCookieToken(r *http.Request, cookieName string) string {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return ""
	}
	return cookie.Value
}

func setCSRFCookie(w http.ResponseWriter, opts CSRFOptions, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     opts.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // JS must read this to include in requests
		Secure:   opts.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Helper functions for token encryption/decryption.
//
// The key is passed straight to aes.NewCipher rather than sliced with
// key[:32]: the slice form panics on a short key (a request-time DoS
// from a config typo) and silently truncates a long one. aes.NewCipher
// instead returns an error for any non-{16,24,32}-byte key. Construction
// -time validation (CSRFOptions.validate) already guarantees a 32-byte
// key reaches here when EnableXSRFCookie is set; this is the defensive
// second line. See ADR-006.
func encryptToken(plaintext, key string) (string, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

func decryptToken(ciphertext, key string) (string, error) {
	data, err := base64.URLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		// The original code did `return "", err` here — but err is the
		// (nil) result of the preceding aes.NewCipher / cipher.NewGCM
		// calls, so a too-short ciphertext silently decrypted to "" with
		// no error. Return a real error instead.
		return "", errors.New("router: CSRF token ciphertext is shorter than the GCM nonce")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

func generateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read never returns an error on supported platforms,
		// but if it ever does, a half-filled token must not be issued —
		// panic rather than hand out a low-entropy CSRF token.
		panic("router: crypto/rand failed generating a CSRF token: " + err.Error())
	}
	return hex.EncodeToString(b)
}
