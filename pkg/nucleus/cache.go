// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import "github.com/jcsvwinston/nucleus/pkg/cache"

// Cache satisfies CacheSource: the cache the framework built from the
// `cache` block, or nil on an unbacked runtime.
func (rt runtime) Cache() cache.Cache {
	if rt.core == nil {
		return nil
	}
	return rt.core.Cache
}

// CacheSource is implemented by a Runtime that can hand out the
// application's cache. Like TaskInspectorSource it is an optional interface
// rather than a method on Runtime, which is published and does not grow
// before the major (QADR-0010).
type CacheSource interface {
	// Cache returns the application's cache, or nil when there is none.
	Cache() cache.Cache
}

// CacheFrom returns the application's cache — the one the framework built
// from the `cache` block of the configuration — and whether there is one.
//
// The backend is the configuration's choice, not the module's: the
// in-memory cache when nothing is set, which each instance keeps for
// itself, and with `cache.provider: redis` (`nucleus add redis-cache`) one
// that every instance pointed at the same server shares. The cache exists
// from app.New on, so a module can take it in OnStart:
//
//	OnStart: func(_ context.Context, rt nucleus.Runtime, _ Config) error {
//	        c, ok := nucleus.CacheFrom(rt)
//	        if !ok {
//	                return errors.New("prices: needs the application's cache")
//	        }
//	        m.cache = c
//	        return nil
//	},
//	// in a handler:
//	if v, ok, err := m.cache.Get(ctx, "price:"+sku); err == nil && ok { ... }
//	_ = m.cache.Set(ctx, "price:"+sku, body, time.Minute)
func CacheFrom(rt Runtime) (cache.Cache, bool) {
	source, ok := rt.(CacheSource)
	if !ok {
		return nil, false
	}
	c := source.Cache()
	if c == nil {
		return nil, false
	}
	return c, true
}
