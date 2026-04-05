package auth

import (
	"context"
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
	sm.Cookie.Secure = cfg.Secure
	sm.Cookie.SameSite = parseSameSite(cfg.SameSite)
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
