// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/cache"
	"github.com/jcsvwinston/nucleus/pkg/db"
)

// CacheConfig is the `cache` block: the backend of the application's cache.
//
//	cache:
//	  provider: redis                    # memory (default) | sql | redis | a registered name
//	  redis_url: redis://cache:6379/0    # redis: the server; empty uses redis_url
//	  prefix: "myapp:cache:"             # redis: the namespace of every key (default nucleus:cache:)
//	  table: nucleus_cache_entries       # sql: the table `nucleus createcachetable` creates
//
// Nothing set is the in-memory cache, which every instance keeps for itself.
// `redis` is registered by importing
// github.com/jcsvwinston/nucleus/pkg/cache/rediscache, which `nucleus add
// redis-cache` writes together with this block.
type CacheConfig struct {
	// Provider selects the backend: "memory" (the default), "sql" (the
	// default database, in the table `nucleus createcachetable` creates),
	// or a name a backend registered with cache.RegisterProvider ("redis").
	Provider string `koanf:"provider"`
	// RedisURL is the server of the redis backend. Empty falls back to the
	// application's redis_url.
	RedisURL string `koanf:"redis_url"`
	// Prefix namespaces every key the redis backend writes, so it can share
	// a server with the session store and a job queue. Empty means
	// "nucleus:cache:".
	Prefix string `koanf:"prefix"`
	// Table is the sql backend's table. Empty means nucleus_cache_entries.
	Table string `koanf:"table"`
}

// provider is the selected backend's name, normalised, with the default
// filled in.
func (c CacheConfig) provider() string {
	p := strings.ToLower(strings.TrimSpace(c.Provider))
	if p == "" {
		return cache.ProviderMemory
	}
	return p
}

// redisURL is cache.redis_url, or the application's redis_url when that is
// empty.
func (c CacheConfig) redisURL(cfg *Config) string {
	if u := strings.TrimSpace(c.RedisURL); u != "" {
		return u
	}
	return strings.TrimSpace(cfg.RedisURL)
}

// validateCacheReference is the cross-field rule of the block: a remote
// backend needs the address of its server, and the error names the keys
// rather than surfacing later as a connection failure.
func validateCacheReference(cfg *Config) error {
	if cfg.Cache.provider() == "redis" && cfg.Cache.redisURL(cfg) == "" {
		return fmt.Errorf("%w: cache.provider \"redis\" requires cache.redis_url (or redis_url)", ErrInvalidConfigReference)
	}
	return nil
}

// attachCache builds the cache the configuration selects. It runs on every
// stack, WithoutDefaults() included: the in-memory default costs a map, and
// a selected backend that an option could switch off would be ignored
// without a word, the way a storage block once was (NU-99).
func attachCache(a *App, cfg *Config, dbConn *db.DB) error {
	selected := cfg.Cache.provider()
	opened := cache.Config{
		Provider: selected,
		URL:      cfg.Cache.redisURL(cfg),
		Prefix:   cfg.Cache.Prefix,
		Table:    cfg.Cache.Table,
	}
	if selected == cache.ProviderSQL && dbConn != nil {
		sqlDB, err := dbConn.SqlDB()
		if err != nil {
			return fmt.Errorf("cache: the default database: %w", err)
		}
		opened.DB, opened.System = sqlDB, dbConn.System()
	}
	c, err := cache.Open(context.Background(), opened)
	if err != nil {
		return err
	}
	a.Cache = c
	if closer, ok := c.(io.Closer); ok {
		a.OnShutdown(func(context.Context) error { return closer.Close() })
	}
	if selected != cache.ProviderMemory {
		a.Logger.Info("cache provider initialized", "provider", selected)
	}
	return nil
}
