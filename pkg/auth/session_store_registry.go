// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"

	"fmt"
	"github.com/jcsvwinston/nucleus/pkg/auth/sessionstore"
	"strings"
	"time"
)

// The contract a third-party session store implements now lives in
// pkg/auth/sessionstore, a leaf package that links ZERO third-party
// packages — its parameters are typed, so unlike the authentication and
// storage contracts it does not even need the configuration decoder.
//
// The names below are ALIASES, so everything that compiled still compiles.
// A new store should import pkg/auth/sessionstore and pay for what it uses.
type (
	// SessionStore is the interface a session backend implements. See
	// sessionstore.Store.
	SessionStore = sessionstore.Store
	// SessionStoreParams carries what a store needs to build itself.
	SessionStoreParams = sessionstore.Params
	// SessionStoreFactory builds a session store plus an optional shutdown
	// hook.
	SessionStoreFactory = sessionstore.Factory
)

func init() {
	// "memory" is registered as a factory returning a nil store: the
	// session manager falls back to its in-memory default. The built-ins
	// register through the same public door as anyone else — and now from
	// OUTSIDE the package that owns the registry, which makes that door the
	// only one there is.
	mustRegisterSessionStore("memory", func(SessionStoreParams) (SessionStore, func(context.Context) error, error) {
		return nil, nil, nil
	})
	mustRegisterSessionStore("sql", newSQLSessionStoreFromParams)
	mustRegisterSessionStore("redis", newRedisSessionStoreFromParams)
}

func mustRegisterSessionStore(name string, factory SessionStoreFactory) {
	if err := sessionstore.Register(name, factory); err != nil {
		panic("auth: registering built-in session store: " + err.Error())
	}
}

// RegisterSessionStore makes a session backend selectable by name from
// configuration (`session_store`). It delegates to sessionstore.Register.
func RegisterSessionStore(name string, factory SessionStoreFactory) error {
	return sessionstore.Register(name, factory)
}

// RegisteredSessionStores returns every selectable store name, sorted.
func RegisteredSessionStores() []string { return sessionstore.Registered() }

// BuildSessionStore resolves a configured store name and builds it. It
// returns the store (nil means "keep the manager's in-memory default"), an
// optional shutdown hook, and an error naming the registered stores when
// the name is unknown.
func BuildSessionStore(name string, params SessionStoreParams) (SessionStore, func(context.Context) error, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		normalized = "memory"
	}

	factory, ok := sessionstore.Lookup(normalized)
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
func unregisterSessionStoreForTest(name string) { sessionstore.Unregister(name) }

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
