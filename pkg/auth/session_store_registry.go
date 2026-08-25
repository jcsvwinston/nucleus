// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// SessionStoreParams carries what a session-store factory may need to build
// itself. Every field is optional from the factory's point of view: a
// backend uses what applies to it and ignores the rest.
//
// It deliberately exposes stdlib and first-party types only, so a
// third-party store never forces a dependency into this package's public
// surface (ADR-015).
type SessionStoreParams struct {
	// DB is the application's managed handle, nil when no database is
	// configured. A SQL-backed store must return an error rather than
	// panic when it needs one and finds nil.
	DB *sql.DB
	// DatabaseURL is the DSN behind DB, for stores that need to know the
	// engine (the built-in SQL store picks its dialect from it).
	DatabaseURL string
	// TableName is the configured session table (`session_table`).
	TableName string
	// RedisURL is `session_redis_url`, falling back to `redis_url`.
	RedisURL string
	// KeyPrefix is `session_redis_prefix`.
	KeyPrefix string
}

// SessionStore is the contract a session backend implements: the three
// operations a session manager needs, in stdlib types only.
//
// It exists rather than re-exporting the session library's own interface
// because a plugin author must be able to write a store against THIS
// framework, not against whatever library it happens to use inside
// (ADR-015). The dependency firewall caught exactly that leak when this
// registry first returned the library type — the guard doing its job on
// the change that introduced it.
type SessionStore interface {
	// Find returns the data for a session token, and whether it exists and
	// has not expired.
	Find(token string) (data []byte, found bool, err error)
	// Commit stores the data for a token with an absolute expiry.
	Commit(token string, data []byte, expiry time.Time) error
	// Delete removes a token.
	Delete(token string) error
}

// SessionStoreFactory builds a session store.
//
// The second return value is an optional shutdown hook — a store holding a
// connection pool returns one, an in-memory store returns nil. The
// framework calls it during graceful shutdown.
type SessionStoreFactory func(params SessionStoreParams) (SessionStore, func(context.Context) error, error)

var (
	sessionStoresMu sync.RWMutex
	sessionStores   = map[string]SessionStoreFactory{}
)

func init() {
	// "memory" is registered as a factory returning a nil store: the
	// SessionManager already defaults to in-memory, and expressing that as
	// "no store to set" keeps one code path instead of two.
	mustRegisterSessionStore("memory", func(SessionStoreParams) (SessionStore, func(context.Context) error, error) {
		return nil, nil, nil
	})
	mustRegisterSessionStore("sql", newSQLSessionStoreFromParams)
	mustRegisterSessionStore("redis", newRedisSessionStoreFromParams)
}

func mustRegisterSessionStore(name string, factory SessionStoreFactory) {
	if err := RegisterSessionStore(name, factory); err != nil {
		panic("auth: registering built-in session store: " + err.Error())
	}
}

// RegisterSessionStore makes a session backend selectable by name from
// configuration (`session_store`).
//
// Same shape as storage.RegisterProvider and mail.RegisterProvider: the
// built-ins register through this same public call, and a name already
// taken is an error rather than a silent replacement — two packages
// claiming "redis" would otherwise make the effective store depend on
// import order.
//
//	package dynamosessions
//
//	func init() {
//	    auth.RegisterSessionStore("dynamodb", New)
//	}
func RegisterSessionStore(name string, factory SessionStoreFactory) error {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return fmt.Errorf("auth: session store name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("auth: session store %q: factory cannot be nil", normalized)
	}

	sessionStoresMu.Lock()
	defer sessionStoresMu.Unlock()
	if _, exists := sessionStores[normalized]; exists {
		return fmt.Errorf("auth: session store %q is already registered", normalized)
	}
	sessionStores[normalized] = factory
	return nil
}

// RegisteredSessionStores returns every selectable store name, sorted.
func RegisteredSessionStores() []string {
	sessionStoresMu.RLock()
	defer sessionStoresMu.RUnlock()

	names := make([]string, 0, len(sessionStores))
	for name := range sessionStores {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// BuildSessionStore resolves a configured store name and builds it. It
// returns the store (nil means "keep the manager's in-memory default"), an
// optional shutdown hook, and an error naming the registered stores when
// the name is unknown.
func BuildSessionStore(name string, params SessionStoreParams) (SessionStore, func(context.Context) error, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		normalized = "memory"
	}

	sessionStoresMu.RLock()
	factory, ok := sessionStores[normalized]
	sessionStoresMu.RUnlock()
	if !ok {
		return nil, nil, fmt.Errorf("auth: unsupported session_store %q (registered: %s) — register a third-party store with auth.RegisterSessionStore",
			name, strings.Join(RegisteredSessionStores(), ", "))
	}
	return factory(params)
}

func newSQLSessionStoreFromParams(p SessionStoreParams) (SessionStore, func(context.Context) error, error) {
	if p.DB == nil {
		return nil, nil, fmt.Errorf("session_store=sql requires database")
	}
	store, err := NewSQLSessionStore(p.DB, SQLSessionStoreConfig{
		DatabaseURL: p.DatabaseURL,
		TableName:   p.TableName,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("session_store=sql initialize store: %w", err)
	}
	return store, nil, nil
}

func newRedisSessionStoreFromParams(p SessionStoreParams) (SessionStore, func(context.Context) error, error) {
	if strings.TrimSpace(p.RedisURL) == "" {
		return nil, nil, fmt.Errorf("session_store=redis requires session_redis_url or redis_url")
	}
	store, client, err := NewRedisSessionStoreFromURL(p.RedisURL, p.KeyPrefix)
	if err != nil {
		return nil, nil, fmt.Errorf("session_store=redis initialize store: %w", err)
	}
	return store, func(context.Context) error { return client.Close() }, nil
}

// unregisterSessionStoreForTest removes a store. Test-only, for the same
// reason storage has one: a running application swapping its session
// backend is not something the public API should be able to express.
func unregisterSessionStoreForTest(name string) {
	sessionStoresMu.Lock()
	defer sessionStoresMu.Unlock()
	delete(sessionStores, strings.ToLower(strings.TrimSpace(name)))
}

// scsAdapter bridges a framework SessionStore to the session library's own
// interface. It lives here, on the framework side of the firewall, so a
// third-party store never has to know the library exists.
type scsAdapter struct{ store SessionStore }

func (a scsAdapter) Find(token string) ([]byte, bool, error) { return a.store.Find(token) }
func (a scsAdapter) Commit(token string, b []byte, expiry time.Time) error {
	return a.store.Commit(token, b, expiry)
}
func (a scsAdapter) Delete(token string) error { return a.store.Delete(token) }

// SetSessionStore installs a framework SessionStore on the manager,
// adapting it to the underlying library.
func (s *SessionManager) SetSessionStore(store SessionStore) {
	if store == nil {
		return
	}
	s.SetStore(scsAdapter{store: store})
}
