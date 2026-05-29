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

const (
	flashDataKeyPrefix = "_flash:"
	flashOldKeyPrefix  = "_flash_old:"
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
	return s.scs.LoadAndSave
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
	s.scs.Put(ctx, flashDataKeyPrefix+key, value)
}

// FlashInt stores an int value in flash data.
func (s *SessionManager) FlashInt(ctx context.Context, key string, value int) {
	s.scs.Put(ctx, flashDataKeyPrefix+key, value)
}

// FlashBool stores a bool value in flash data.
func (s *SessionManager) FlashBool(ctx context.Context, key string, value bool) {
	s.scs.Put(ctx, flashDataKeyPrefix+key, value)
}

// GetFlash retrieves a flash value for the current request.
func (s *SessionManager) GetFlash(ctx context.Context, key string) string {
	return s.scs.GetString(ctx, flashDataKeyPrefix+key)
}

// GetFlashInt retrieves a flash int value for the current request.
func (s *SessionManager) GetFlashInt(ctx context.Context, key string) int {
	return s.scs.GetInt(ctx, flashDataKeyPrefix+key)
}

// GetFlashBool retrieves a flash bool value for the current request.
func (s *SessionManager) GetFlashBool(ctx context.Context, key string) bool {
	return s.scs.GetBool(ctx, flashDataKeyPrefix+key)
}

// Reflash keeps all flash data for an additional request.
func (s *SessionManager) Reflash(ctx context.Context) {
	// Move current flash data to old flash data
	for _, key := range s.scs.Keys(ctx) {
		if strings.HasPrefix(key, flashDataKeyPrefix) {
			value := s.scs.GetString(ctx, key)
			oldKey := flashOldKeyPrefix + strings.TrimPrefix(key, flashDataKeyPrefix)
			s.scs.Put(ctx, oldKey, value)
		}
	}
}

// Keep keeps specific flash data keys for an additional request.
func (s *SessionManager) Keep(ctx context.Context, keys []string) {
	for _, key := range keys {
		if value := s.scs.GetString(ctx, flashDataKeyPrefix+key); value != "" {
			s.scs.Put(ctx, flashOldKeyPrefix+key, value)
		}
	}
}

// Now stores a key-value pair that is only available in the current request.
// This is similar to Flash but the data is not persisted to the next request.
func (s *SessionManager) Now(ctx context.Context, key, value string) {
	s.scs.Put(ctx, flashDataKeyPrefix+key, value)
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
