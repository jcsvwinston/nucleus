// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
)

// The provider names the framework builds itself. Every other name is a
// registered backend: "redis" registers when
// github.com/jcsvwinston/nucleus/pkg/cache/rediscache is imported.
const (
	ProviderMemory = "memory"
	ProviderSQL    = "sql"
)

// DefaultKeyPrefix is the namespace a shared backend puts every key under
// when Config.Prefix is empty, so a cache sharing a Redis with the session
// store ("nucleus:sessions:") and the job queue never reads their keys.
const DefaultKeyPrefix = "nucleus:cache:"

// Config is the `cache` block of the application's configuration, resolved:
// what Open builds a backend from, and what a registered provider receives.
type Config struct {
	// Provider selects the backend: "memory" (the default when empty),
	// "sql", or the name a backend registered with RegisterProvider
	// ("redis").
	Provider string
	// URL is the address of the store a remote backend talks to — for
	// redis, cache.redis_url (or redis_url when that is empty).
	URL string
	// Prefix namespaces every key in a shared store. Empty means
	// DefaultKeyPrefix. The memory and SQL backends do not use it: the
	// first is the process's own, the second has a table of its own.
	Prefix string
	// DB and System are the database the sql provider keeps its table in
	// and the SQL system it speaks ("sqlite", "postgresql", …, the values
	// (*db.DB).System() returns). Ignored by every other provider.
	DB     *sql.DB
	System string
	// Table is the sql provider's table; empty means DefaultTableName.
	Table string
}

// Factory builds a backend from the resolved configuration. A factory that
// opens a connection checks it before returning: Open is called at boot,
// and an unreachable store is a boot error, not a first-request one.
type Factory func(ctx context.Context, cfg Config) (Cache, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// RegisterProvider makes a backend selectable by name in `cache.provider`.
// It refuses an empty name, a nil factory, a name the framework builds
// itself ("memory", "sql") and a name already registered: two packages
// claiming the same name is a build mistake, not something to resolve at
// run time. A backend registers from an init function of its package, the
// way database/sql drivers do — MustRegisterProvider is the form for that.
func RegisterProvider(name string, factory Factory) error {
	name = normalizeProvider(name)
	if name == "" {
		return errors.New("cache: a provider needs a name")
	}
	if factory == nil {
		return fmt.Errorf("cache: provider %q: factory cannot be nil", name)
	}
	if name == ProviderMemory || name == ProviderSQL {
		return fmt.Errorf("cache: provider %q is built by the framework and cannot be registered", name)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[name]; dup {
		return fmt.Errorf("cache: provider %q is already registered", name)
	}
	registry[name] = factory
	return nil
}

// MustRegisterProvider is RegisterProvider for an init function: it panics
// with RegisterProvider's error.
func MustRegisterProvider(name string, factory Factory) {
	if err := RegisterProvider(name, factory); err != nil {
		panic(err)
	}
}

// RegisteredProviders returns every name `cache.provider` accepts in this
// binary — the built-in ones and the registered ones — sorted.
func RegisteredProviders() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := []string{ProviderMemory, ProviderSQL}
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func normalizeProvider(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// Open builds the backend Config selects.
//
// A name this project publishes and the binary does not link — "redis"
// without the rediscache import — is refused with the command that adds it,
// never replaced by the memory backend: a cache that silently stops being
// shared between instances is how one instance serves what another
// invalidated.
func Open(ctx context.Context, cfg Config) (Cache, error) {
	name := normalizeProvider(cfg.Provider)
	switch name {
	case "", ProviderMemory:
		return NewMemory(), nil
	case ProviderSQL:
		if cfg.DB == nil {
			return nil, errors.New("cache: provider \"sql\" needs a database, and the application has none configured")
		}
		return NewSQL(cfg.DB, SQLOptions{Table: cfg.Table, System: cfg.System})
	}
	registryMu.RLock()
	factory, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		if p, ours := knownproviders.CacheBackend(name); ours {
			return nil, fmt.Errorf("cache: provider %q ships with this framework as %s and is not linked into this binary (registered: %s).\n\n"+
				"\tAdd it to your build:\n\n%s",
				name, p.ImportPath(), strings.Join(RegisteredProviders(), ", "), p.InstallHint())
		}
		return nil, fmt.Errorf("cache: unsupported provider %q (registered: %s) — a backend registers itself with cache.RegisterProvider when its package is imported",
			cfg.Provider, strings.Join(RegisteredProviders(), ", "))
	}
	if strings.TrimSpace(cfg.Prefix) == "" {
		cfg.Prefix = DefaultKeyPrefix
	}
	cfg.Provider = name
	c, err := factory(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("cache: provider %q: %w", name, err)
	}
	return c, nil
}
