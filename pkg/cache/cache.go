// Package cache is a minimal key/value cache with TTL semantics and the
// runtime counterpart of the `nucleus createcachetable` CLI command: an
// in-memory backend for single-process deployments, a SQL backend wired to
// the table that command creates (`nucleus_cache_entries` by default) for
// replicas that share a database, and a registry other backends join by
// import — the Redis one is pkg/cache/rediscache.
//
// The framework builds one cache per application from the `cache` block of
// its configuration (Open, with cache.provider selecting the backend) and
// hands it to modules through nucleus.CacheFrom.
//
// Lifecycle: experimental (see docs/reference/API_CONTRACT_INVENTORY.md).
// The surface may still grow (GetOrSet helpers) before it freezes. The
// package itself links only the standard library and the framework's
// catalog; a backend with a client library lives in a package of its own.
package cache

import (
	"context"
	"errors"
	"time"
)

// DefaultTableName is the table `nucleus createcachetable` creates when no
// --table override is given, and the table NewSQL uses when
// SQLOptions.Table is empty. The CLI and this package share the constant on
// purpose: the command and the runtime must not drift apart.
const DefaultTableName = "nucleus_cache_entries"

var (
	// ErrEmptyKey is returned when a cache operation receives an empty key.
	ErrEmptyKey = errors.New("cache: key cannot be empty")
	// ErrNonPositiveTTL is returned by Set when ttl <= 0. The backing table
	// requires an expiry for every entry; an entry that must never expire
	// does not belong in a cache.
	ErrNonPositiveTTL = errors.New("cache: ttl must be greater than zero")
)

// Cache is the minimal contract both backends implement.
//
// Get returns (value, true, nil) for a live entry and (nil, false, nil) for
// a missing or expired one — an expired entry is indistinguishable from an
// absent one on purpose. Set stores value under key for ttl; a second Set
// on the same key replaces the value and its expiry. Delete removes an
// entry; deleting an absent key is not an error.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}
