// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package rediscache is the Redis backend of pkg/cache: entries every
// instance of an application pointed at the same server shares, with the
// expiry kept by Redis itself.
//
// Importing it registers the backend under the name "redis", which is what
// `cache.provider: redis` selects; `nucleus add redis-cache` writes the
// import and the configuration block. Code that wants a cache of its own
// outside the application's opens one directly:
//
//	c, err := rediscache.Open(ctx, "redis://cache:6379/0", rediscache.Options{Prefix: "reports:"})
//
// It lives in the framework module because its one dependency, go-redis, is
// already linked by every application (the session store, the health check
// and the asynq queue import it), and in a package of its own so that an
// application that does not import it does not link it. The client stays
// behind the cache.Cache interface: no go-redis type is part of the API.
//
// Lifecycle: experimental, like pkg/cache.
package rediscache

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/jcsvwinston/nucleus/pkg/cache"
)

// ProviderName is the name the backend registers under: the value of
// cache.provider that selects it.
const ProviderName = "redis"

// pingTimeout bounds the reachability check a backend opened from
// configuration makes before the application starts.
const pingTimeout = 5 * time.Second

func init() {
	cache.MustRegisterProvider(ProviderName, func(ctx context.Context, cfg cache.Config) (cache.Cache, error) {
		if strings.TrimSpace(cfg.URL) == "" {
			return nil, errors.New("no Redis address: set cache.redis_url (or redis_url)")
		}
		return Open(ctx, cfg.URL, Options{Prefix: cfg.Prefix})
	})
}

// Options configures a Redis cache.
type Options struct {
	// Prefix is put in front of every key, so a cache sharing a server
	// with the session store or a job queue never touches their keys.
	// Empty means cache.DefaultKeyPrefix.
	Prefix string
}

// Cache is a cache.Cache kept in Redis. It is safe for concurrent use.
type Cache struct {
	client goredis.UniversalClient
	prefix string
}

var _ cache.Cache = (*Cache)(nil)

func newCache(client goredis.UniversalClient, opts Options) *Cache {
	prefix := opts.Prefix
	if strings.TrimSpace(prefix) == "" {
		prefix = cache.DefaultKeyPrefix
	}
	return &Cache{client: client, prefix: prefix}
}

// Open connects to the server rawURL names (redis://[user:password@]host:port/db,
// or rediss:// for TLS), checks that it answers, and returns a cache over
// that connection. Close closes it.
func Open(ctx context.Context, rawURL string, opts Options) (*Cache, error) {
	options, err := goredis.ParseURL(strings.TrimSpace(rawURL))
	if err != nil {
		// The URL may carry a password, and net/url quotes the whole URL
		// in its errors: keep the reason, not the URL.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("rediscache: parse the Redis URL: %w", err)
	}
	c := newCache(goredis.NewClient(options), opts)
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := c.Ping(pingCtx); err != nil {
		_ = c.client.Close()
		return nil, fmt.Errorf("rediscache: Redis at %s does not answer: %w", options.Addr, err)
	}
	return c, nil
}

func (c *Cache) key(key string) string { return c.prefix + key }

// Get implements cache.Cache.
func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if strings.TrimSpace(key) == "" {
		return nil, false, cache.ErrEmptyKey
	}
	value, err := c.client.Get(ctx, c.key(key)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cache: get %q: %w", key, err)
	}
	return value, true, nil
}

// Set implements cache.Cache. The expiry is Redis's own (SET with PX), so an
// entry disappears from every instance at once.
func (c *Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if strings.TrimSpace(key) == "" {
		return cache.ErrEmptyKey
	}
	if ttl <= 0 {
		return cache.ErrNonPositiveTTL
	}
	if value == nil {
		value = []byte{}
	}
	if err := c.client.Set(ctx, c.key(key), value, ttl).Err(); err != nil {
		return fmt.Errorf("cache: set %q: %w", key, err)
	}
	return nil
}

// Delete implements cache.Cache.
func (c *Cache) Delete(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return cache.ErrEmptyKey
	}
	if err := c.client.Del(ctx, c.key(key)).Err(); err != nil {
		return fmt.Errorf("cache: delete %q: %w", key, err)
	}
	return nil
}

// Ping reports whether the server answers. The framework's readiness check
// calls it.
func (c *Cache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Close closes the connection.
func (c *Cache) Close() error {
	return c.client.Close()
}
