package auth

import (
	"context"
	"time"
)

const (
	sessionCacheKeyPrefix = "_cache:"
	sessionCacheExpiryKey = "_cache_expiry:"
)

// SessionCache provides a cache mechanism scoped to an individual user session.
// Unlike the global application cache, session cache data is automatically isolated
// per session and cleaned up when the session expires or is destroyed.
// This is perfect for storing temporary, user-specific data like form data,
// temporary calculations, API responses, or any other ephemeral data.
type SessionCache struct {
	sm *SessionManager
}

// NewSessionCache creates a new session cache instance.
func NewSessionCache(sm *SessionManager) *SessionCache {
	return &SessionCache{sm: sm}
}

// Get retrieves a value from the session cache.
// Returns the value and a boolean indicating if the key exists and is not expired.
func (c *SessionCache) Get(ctx context.Context, key string) (string, bool) {
	if c.isExpired(ctx, key) {
		c.Forget(ctx, key)
		return "", false
	}
	value := c.sm.GetString(ctx, sessionCacheKeyPrefix+key)
	return value, value != ""
}

// GetInt retrieves an int value from the session cache.
func (c *SessionCache) GetInt(ctx context.Context, key string) (int, bool) {
	if c.isExpired(ctx, key) {
		c.Forget(ctx, key)
		return 0, false
	}
	value := c.sm.GetInt(ctx, sessionCacheKeyPrefix+key)
	return value, c.sm.Exists(ctx, sessionCacheKeyPrefix+key)
}

// GetBool retrieves a bool value from the session cache.
func (c *SessionCache) GetBool(ctx context.Context, key string) (bool, bool) {
	if c.isExpired(ctx, key) {
		c.Forget(ctx, key)
		return false, false
	}
	value := c.sm.GetBool(ctx, sessionCacheKeyPrefix+key)
	return value, c.sm.Exists(ctx, sessionCacheKeyPrefix+key)
}

// Put stores a value in the session cache with an optional TTL.
// If ttl is 0, the value will persist until the session expires.
func (c *SessionCache) Put(ctx context.Context, key string, value string, ttl time.Duration) {
	c.sm.Put(ctx, sessionCacheKeyPrefix+key, value)
	if ttl > 0 {
		expiry := time.Now().UTC().Add(ttl)
		c.sm.Put(ctx, sessionCacheExpiryKey+key, expiry.Format(time.RFC3339))
	}
}

// PutInt stores an int value in the session cache with an optional TTL.
func (c *SessionCache) PutInt(ctx context.Context, key string, value int, ttl time.Duration) {
	c.sm.PutInt(ctx, sessionCacheKeyPrefix+key, value)
	if ttl > 0 {
		expiry := time.Now().UTC().Add(ttl)
		c.sm.Put(ctx, sessionCacheExpiryKey+key, expiry.Format(time.RFC3339))
	}
}

// PutBool stores a bool value in the session cache with an optional TTL.
func (c *SessionCache) PutBool(ctx context.Context, key string, value bool, ttl time.Duration) {
	c.sm.PutBool(ctx, sessionCacheKeyPrefix+key, value)
	if ttl > 0 {
		expiry := time.Now().UTC().Add(ttl)
		c.sm.Put(ctx, sessionCacheExpiryKey+key, expiry.Format(time.RFC3339))
	}
}

// Remember retrieves a value from the session cache. If the key does not exist,
// it executes the provided function and stores the result with the given TTL.
func (c *SessionCache) Remember(ctx context.Context, key string, ttl time.Duration, fn func() (string, error)) (string, error) {
	if value, ok := c.Get(ctx, key); ok {
		return value, nil
	}
	value, err := fn()
	if err != nil {
		return "", err
	}
	c.Put(ctx, key, value, ttl)
	return value, nil
}

// RememberInt retrieves an int value from the session cache. If the key does not exist,
// it executes the provided function and stores the result with the given TTL.
func (c *SessionCache) RememberInt(ctx context.Context, key string, ttl time.Duration, fn func() (int, error)) (int, error) {
	if value, ok := c.GetInt(ctx, key); ok {
		return value, nil
	}
	value, err := fn()
	if err != nil {
		return 0, err
	}
	c.PutInt(ctx, key, value, ttl)
	return value, nil
}

// RememberBool retrieves a bool value from the session cache. If the key does not exist,
// it executes the provided function and stores the result with the given TTL.
func (c *SessionCache) RememberBool(ctx context.Context, key string, ttl time.Duration, fn func() (bool, error)) (bool, error) {
	if value, ok := c.GetBool(ctx, key); ok {
		return value, nil
	}
	value, err := fn()
	if err != nil {
		return false, err
	}
	c.PutBool(ctx, key, value, ttl)
	return value, nil
}

// Forget removes a value from the session cache.
func (c *SessionCache) Forget(ctx context.Context, key string) {
	c.sm.Remove(ctx, sessionCacheKeyPrefix+key)
	c.sm.Remove(ctx, sessionCacheExpiryKey+key)
}

// Flush removes all values from the session cache.
func (c *SessionCache) Flush(ctx context.Context) {
	for _, key := range c.sm.SCS().Keys(ctx) {
		if key == sessionCacheKeyPrefix || key == sessionCacheExpiryKey {
			continue
		}
		if len(key) > len(sessionCacheKeyPrefix) {
			if key[:len(sessionCacheKeyPrefix)] == sessionCacheKeyPrefix ||
				key[:len(sessionCacheExpiryKey)] == sessionCacheExpiryKey {
				c.sm.Remove(ctx, key)
			}
		}
	}
}

// Has checks if a key exists in the session cache and is not expired.
func (c *SessionCache) Has(ctx context.Context, key string) bool {
	if c.isExpired(ctx, key) {
		c.Forget(ctx, key)
		return false
	}
	return c.sm.Exists(ctx, sessionCacheKeyPrefix+key)
}

// isExpired checks if a cache key has expired.
func (c *SessionCache) isExpired(ctx context.Context, key string) bool {
	expiryRaw := c.sm.GetString(ctx, sessionCacheExpiryKey+key)
	if expiryRaw == "" {
		return false // No expiry set, persists with session
	}
	expiry, err := time.Parse(time.RFC3339, expiryRaw)
	if err != nil {
		return true // Invalid expiry, treat as expired
	}
	return time.Now().UTC().After(expiry)
}
