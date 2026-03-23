package auth

import (
	"context"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
)

// SessionManager wraps alexedwards/scs for server-side session management.
// Sessions can be backed by in-memory storage (default) or Redis.
type SessionManager struct {
	scs *scs.SessionManager
}

// SessionConfig configures the session manager.
type SessionConfig struct {
	Lifetime time.Duration // Session lifetime (default: 72h)
	Secure   bool          // Cookie Secure flag (set true in production)
	Path     string        // Cookie path (default: "/")
}

// NewSessionManager creates a session manager with the given configuration.
// By default it uses in-memory storage. Call SetStore to use Redis or another backend.
func NewSessionManager(cfg SessionConfig) *SessionManager {
	sm := scs.New()

	if cfg.Lifetime > 0 {
		sm.Lifetime = cfg.Lifetime
	} else {
		sm.Lifetime = 72 * time.Hour
	}

	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = cfg.Secure
	sm.Cookie.SameSite = http.SameSiteLaxMode
	if cfg.Path != "" {
		sm.Cookie.Path = cfg.Path
	} else {
		sm.Cookie.Path = "/"
	}

	return &SessionManager{scs: sm}
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

// SCS returns the underlying scs.SessionManager for advanced configuration
// (e.g. setting a custom store for Redis).
func (s *SessionManager) SCS() *scs.SessionManager {
	return s.scs
}
