// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package sessionstore is the contract a third-party session store
// implements — and nothing else.
//
// Like the authentication and storage contracts before it, it was
// extracted so implementing three methods does not mean importing a
// package that links 115 third-party ones. This package links ZERO: its
// parameters are typed, so it does not even need the configuration
// decoder the other two contracts use.
//
// The names remain available from pkg/auth as aliases, so nothing that
// already compiled stops compiling.
package sessionstore

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Params carries what a session-store factory may need to build
// itself. Every field is optional from the factory's point of view: a
// backend uses what applies to it and ignores the rest.
//
// It deliberately exposes stdlib and first-party types only, so a
// third-party store never forces a dependency into this package's public
// surface (ADR-015).
type Params struct {
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
type Store interface {
	// Find returns the data for a session token, and whether it exists and
	// has not expired.
	Find(token string) (data []byte, found bool, err error)
	// Commit stores the data for a token with an absolute expiry.
	Commit(token string, data []byte, expiry time.Time) error
	// Delete removes a token.
	Delete(token string) error
}

// Factory builds a session store.
//
// The second return value is an optional shutdown hook — a store holding a
// connection pool returns one, an in-memory store returns nil. The
// framework calls it during graceful shutdown.
type Factory func(params Params) (Store, func(context.Context) error, error)

var (
	storesMu sync.RWMutex
	stores   = map[string]Factory{}
)

func Register(name string, factory Factory) error {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return fmt.Errorf("auth: session store name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("auth: session store %q: factory cannot be nil", normalized)
	}

	storesMu.Lock()
	defer storesMu.Unlock()
	if _, exists := stores[normalized]; exists {
		return fmt.Errorf("auth: session store %q is already registered", normalized)
	}
	stores[normalized] = factory
	return nil
}

// RegisteredSessionStores returns every selectable store name, sorted.
func Registered() []string {
	storesMu.RLock()
	defer storesMu.RUnlock()

	names := make([]string, 0, len(stores))
	for name := range stores {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Lookup returns the factory registered under name.
func Lookup(name string) (Factory, bool) {
	storesMu.RLock()
	defer storesMu.RUnlock()
	f, ok := stores[strings.ToLower(strings.TrimSpace(name))]
	return f, ok
}

// Unregister removes a registered store. For tests that register a fake
// and must not leak it into the next one.
func Unregister(name string) {
	storesMu.Lock()
	defer storesMu.Unlock()
	delete(stores, strings.ToLower(strings.TrimSpace(name)))
}
