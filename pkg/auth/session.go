package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
)

// SessionManager wraps alexedwards/scs for server-side session management.
// Sessions can be backed by in-memory storage (default), SQL, or Redis.
type SessionManager struct {
	scs *scs.SessionManager
}

// SessionConfig configures the session manager.
type SessionConfig struct {
	Lifetime    time.Duration // Session lifetime (default: 72h)
	IdleTimeout time.Duration // Optional inactivity timeout (default: disabled)
	Secure      bool          // Cookie Secure flag (set true in production)
	Path        string        // Cookie path (default: "/")
	Domain      string        // Cookie domain (default: host-only)
	CookieName  string        // Cookie name (default: "session")
	SameSite    string        // Cookie SameSite: lax|strict|none (default: lax)
}

// Flash data has a life cycle the session middleware drives (NU-9): what a
// request flashes is readable in the NEXT request only, and gone after it.
//
//	_flash_next:k  written by this request, promoted at the start of the next
//	_flash:k       readable in this request (promoted from the previous one),
//	               removed at the start of the request after
//	_flash_now:k   this request only, swept when it ends
//
// Reflash and Keep copy readable keys back into _flash_next, buying them one
// more request. Before this cycle existed nothing ever removed a flashed key
// and the "old" prefix was written but never read.
const (
	flashDataKeyPrefix = "_flash:"
	flashNextKeyPrefix = "_flash_next:"
	flashNowKeyPrefix  = "_flash_now:"
)

// NewSessionManager creates a session manager with the given configuration.
// By default it uses in-memory storage. Call SetStore to use Redis, SQL, or another backend.
func NewSessionManager(cfg SessionConfig) *SessionManager {
	sm := scs.New()

	if cfg.Lifetime > 0 {
		sm.Lifetime = cfg.Lifetime
	} else {
		sm.Lifetime = 72 * time.Hour
	}

	if cfg.IdleTimeout > 0 {
		sm.IdleTimeout = cfg.IdleTimeout
	}

	sm.Cookie.HttpOnly = true

	sameSite := parseSameSite(cfg.SameSite)
	secure := cfg.Secure
	// FW-4: a SameSite=None cookie without the Secure attribute is silently
	// dropped by every modern browser, so the session would never persist.
	// pkg/app.buildSessionManager rejects this combo at startup; this is the
	// defence-in-depth path for callers constructing a SessionManager
	// directly. We coerce Secure=true (the only value the browser will
	// honour) rather than ship a cookie that is guaranteed to be discarded,
	// and emit a WARN so the override is visible in operational telemetry.
	if sameSite == http.SameSiteNoneMode && !secure {
		secure = true
		slog.Default().Warn(
			"session: SameSite=None requires Secure; forcing Secure=true " +
				"(browsers drop SameSite=None cookies without the Secure attribute). " +
				"Set session_cookie_secure=true to silence this warning.",
		)
	}

	sm.Cookie.Secure = secure
	sm.Cookie.SameSite = sameSite
	sm.Cookie.Domain = cfg.Domain
	if cfg.Path != "" {
		sm.Cookie.Path = cfg.Path
	} else {
		sm.Cookie.Path = "/"
	}
	if cfg.CookieName != "" {
		sm.Cookie.Name = cfg.CookieName
	}

	return &SessionManager{scs: sm}
}

func parseSameSite(raw string) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	case "lax", "":
		return http.SameSiteLaxMode
	default:
		return http.SameSiteLaxMode
	}
}

// Middleware returns the session middleware that must be applied to the router
// for session handling to work.
func (s *SessionManager) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.scs.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			s.ageFlash(ctx)
			// The session library commits the session when the response
			// header goes out, so Now() data has to be swept BEFORE the
			// first write, not after the handler returns.
			sw := &flashSweepWriter{ResponseWriter: w, sweep: func() { s.sweepFlashNow(ctx) }}
			next.ServeHTTP(sw, r)
			sw.sweepOnce()
		}))
	}
}

// flashSweepWriter runs the Now() sweep once, right before the response
// header is written (the moment the session is committed).
type flashSweepWriter struct {
	http.ResponseWriter
	sweep func()
	done  bool
}

func (w *flashSweepWriter) sweepOnce() {
	if !w.done {
		w.done = true
		w.sweep()
	}
}

func (w *flashSweepWriter) WriteHeader(status int) {
	w.sweepOnce()
	w.ResponseWriter.WriteHeader(status)
}

func (w *flashSweepWriter) Write(b []byte) (int, error) {
	w.sweepOnce()
	return w.ResponseWriter.Write(b)
}

func (w *flashSweepWriter) Flush() {
	w.sweepOnce()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *flashSweepWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ageFlash runs at the start of a request: the keys the previous request
// could read have had their one request and go; the keys it flashed become
// readable now.
func (s *SessionManager) ageFlash(ctx context.Context) {
	for _, key := range s.scs.Keys(ctx) {
		if strings.HasPrefix(key, flashDataKeyPrefix) {
			s.scs.Remove(ctx, key)
		}
	}
	for _, key := range s.scs.Keys(ctx) {
		if strings.HasPrefix(key, flashNextKeyPrefix) {
			s.scs.Put(ctx, flashDataKeyPrefix+strings.TrimPrefix(key, flashNextKeyPrefix), s.scs.Get(ctx, key))
			s.scs.Remove(ctx, key)
		}
	}
}

// sweepFlashNow runs when the request ends: Now() data never outlives it.
func (s *SessionManager) sweepFlashNow(ctx context.Context) {
	for _, key := range s.scs.Keys(ctx) {
		if strings.HasPrefix(key, flashNowKeyPrefix) {
			s.scs.Remove(ctx, key)
		}
	}
}

// Put stores a string value in the session.
func (s *SessionManager) Put(ctx context.Context, key, value string) {
	s.scs.Put(ctx, key, value)
}

// GetString retrieves a string value from the session.
func (s *SessionManager) GetString(ctx context.Context, key string) string {
	return s.scs.GetString(ctx, key)
}

// PutInt stores an int value in the session.
func (s *SessionManager) PutInt(ctx context.Context, key string, value int) {
	s.scs.Put(ctx, key, value)
}

// GetInt retrieves an int value from the session.
func (s *SessionManager) GetInt(ctx context.Context, key string) int {
	return s.scs.GetInt(ctx, key)
}

// PutBool stores a bool value in the session.
func (s *SessionManager) PutBool(ctx context.Context, key string, value bool) {
	s.scs.Put(ctx, key, value)
}

// GetBool retrieves a bool value from the session.
func (s *SessionManager) GetBool(ctx context.Context, key string) bool {
	return s.scs.GetBool(ctx, key)
}

// Exists checks if a key exists in the session.
func (s *SessionManager) Exists(ctx context.Context, key string) bool {
	return s.scs.Exists(ctx, key)
}

// Remove deletes a key from the session.
func (s *SessionManager) Remove(ctx context.Context, key string) {
	s.scs.Remove(ctx, key)
}

// Destroy deletes the entire session.
func (s *SessionManager) Destroy(ctx context.Context) error {
	return s.scs.Destroy(ctx)
}

// RenewToken generates a new session ID while preserving data.
// Should be called after login to prevent session fixation.
func (s *SessionManager) RenewToken(ctx context.Context) error {
	return s.scs.RenewToken(ctx)
}

// SetStore sets a custom SCS store implementation (Redis, SQL, etc).
func (s *SessionManager) SetStore(store scs.Store) {
	if s == nil || s.scs == nil || store == nil {
		return
	}
	s.scs.Store = store
}

// SCS returns the underlying scs.SessionManager for advanced configuration
// (e.g. setting a custom store for Redis).
func (s *SessionManager) SCS() *scs.SessionManager {
	return s.scs
}

// Flash stores a key-value pair in the session that will be available
// in the next request only. After the next request, the data is automatically deleted.
// This is useful for status messages (e.g., "Task completed successfully").
func (s *SessionManager) Flash(ctx context.Context, key, value string) {
	s.scs.Put(ctx, flashNextKeyPrefix+key, value)
}

// FlashInt stores an int value in flash data.
func (s *SessionManager) FlashInt(ctx context.Context, key string, value int) {
	s.scs.Put(ctx, flashNextKeyPrefix+key, value)
}

// FlashBool stores a bool value in flash data.
func (s *SessionManager) FlashBool(ctx context.Context, key string, value bool) {
	s.scs.Put(ctx, flashNextKeyPrefix+key, value)
}

// flashKey resolves which stored key answers a read: Now() data of this
// request, then what the previous request flashed, then what THIS request
// flashed — a value is readable in the request that flashes it too, as it
// is in Laravel, so a handler can flash and render in one go.
func (s *SessionManager) flashKey(ctx context.Context, key string) string {
	for _, prefix := range []string{flashNowKeyPrefix, flashDataKeyPrefix, flashNextKeyPrefix} {
		if s.scs.Exists(ctx, prefix+key) {
			return prefix + key
		}
	}
	return flashDataKeyPrefix + key
}

// GetFlash retrieves a flash value for the current request.
func (s *SessionManager) GetFlash(ctx context.Context, key string) string {
	return s.scs.GetString(ctx, s.flashKey(ctx, key))
}

// GetFlashInt retrieves a flash int value for the current request.
func (s *SessionManager) GetFlashInt(ctx context.Context, key string) int {
	return s.scs.GetInt(ctx, s.flashKey(ctx, key))
}

// GetFlashBool retrieves a flash bool value for the current request.
func (s *SessionManager) GetFlashBool(ctx context.Context, key string) bool {
	return s.scs.GetBool(ctx, s.flashKey(ctx, key))
}

// Reflash keeps all flash data readable now for one more request.
func (s *SessionManager) Reflash(ctx context.Context) {
	for _, key := range s.scs.Keys(ctx) {
		if strings.HasPrefix(key, flashDataKeyPrefix) {
			s.scs.Put(ctx, flashNextKeyPrefix+strings.TrimPrefix(key, flashDataKeyPrefix), s.scs.Get(ctx, key))
		}
	}
}

// Keep keeps specific flash data keys for one more request.
func (s *SessionManager) Keep(ctx context.Context, keys []string) {
	for _, key := range keys {
		if s.scs.Exists(ctx, flashDataKeyPrefix+key) {
			s.scs.Put(ctx, flashNextKeyPrefix+key, s.scs.Get(ctx, flashDataKeyPrefix+key))
		}
	}
}

// Now stores a key-value pair that is only available in the current request:
// the middleware sweeps it when the request ends.
func (s *SessionManager) Now(ctx context.Context, key, value string) {
	s.scs.Put(ctx, flashNowKeyPrefix+key, value)
}

// Pull retrieves a value from the session and deletes it in one operation.
func (s *SessionManager) Pull(ctx context.Context, key string) string {
	value := s.scs.GetString(ctx, key)
	s.scs.Remove(ctx, key)
	return value
}

// PullInt retrieves an int value from the session and deletes it in one operation.
func (s *SessionManager) PullInt(ctx context.Context, key string) int {
	value := s.scs.GetInt(ctx, key)
	s.scs.Remove(ctx, key)
	return value
}

// PullBool retrieves a bool value from the session and deletes it in one operation.
func (s *SessionManager) PullBool(ctx context.Context, key string) bool {
	value := s.scs.GetBool(ctx, key)
	s.scs.Remove(ctx, key)
	return value
}

// Forget removes multiple keys from the session in one operation.
func (s *SessionManager) Forget(ctx context.Context, keys []string) {
	for _, key := range keys {
		s.scs.Remove(ctx, key)
	}
}

// Invalidate regenerates the session ID and removes all data from the session.
// This is useful for logout or when you want to completely reset the session.
func (s *SessionManager) Invalidate(ctx context.Context) error {
	if err := s.scs.RenewToken(ctx); err != nil {
		return err
	}
	// Clear all session data
	for _, key := range s.scs.Keys(ctx) {
		s.scs.Remove(ctx, key)
	}
	return nil
}
