// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/internal/testredis"
	"github.com/jcsvwinston/nucleus/pkg/cache"
	_ "github.com/jcsvwinston/nucleus/pkg/cache/rediscache"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// cacheModule takes the application's cache in OnStart, the way the
// documentation tells a module to.
type cacheModule struct{ got cache.Cache }

func (m *cacheModule) spec() nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name: "cachebench",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			c, ok := nucleus.CacheFrom(rt)
			if !ok {
				return errors.New("cachebench: no cache")
			}
			m.got = c
			return nil
		},
	}.Build()
}

func startWithCache(t *testing.T, config string) (*nucleustest.Server, cache.Cache) {
	t.Helper()
	m := &cacheModule{}
	srv := nucleustest.Start(t, nucleus.New().
		FromConfigFile(config).
		WithOpenAuthz().
		WithoutDefaults().
		Mount(m.spec()))
	t.Cleanup(srv.Stop)
	if m.got == nil {
		t.Fatal("the module did not get the cache in OnStart")
	}
	if c, ok := nucleus.CacheFrom(srv.Runtime()); !ok || c != m.got {
		t.Fatalf("CacheFrom(runtime) = %T ok=%v, not the cache the module got", c, ok)
	}
	return srv, m.got
}

// With nothing configured every application has a cache, and it is its own:
// a second instance does not see it.
func TestCacheFrom_MemoryByDefault(t *testing.T) {
	_, a := startWithCache(t, starterConfig(t, ""))
	_, b := startWithCache(t, starterConfig(t, ""))
	if _, ok := a.(*cache.Memory); !ok {
		t.Fatalf("the default cache is %T, want the memory cache", a)
	}
	if err := a.Set(t.Context(), "k", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := b.Get(t.Context(), "k"); ok {
		t.Fatal("two instances share the memory cache")
	}
}

// cache.provider: redis — what `nucleus add redis-cache` writes — gives two
// instances one cache: what one module writes the other reads, under the
// configured prefix, and /healthz reports the server.
func TestCacheFrom_RedisSharedBetweenInstances(t *testing.T) {
	redis := testredis.Run(t)
	block := "cache:\n  provider: redis\n  redis_url: redis://" + redis.Addr() + "/0\n  prefix: \"shop:\"\n"
	srvA, a := startWithCache(t, starterConfig(t, block))
	_, b := startWithCache(t, starterConfig(t, block))

	if err := a.Set(t.Context(), "price:42", []byte("9.99"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, ok, err := b.Get(t.Context(), "price:42"); err != nil || !ok || string(got) != "9.99" {
		t.Fatalf("the second instance reads %q ok=%v err=%v", got, ok, err)
	}
	if !redis.Exists("shop:price:42") {
		t.Fatalf("the key is not under the configured prefix: %v", redis.Keys())
	}

	health := srvA.Get("/healthz")
	if !strings.Contains(health.String(), `"cache"`) {
		t.Fatalf("/healthz does not report the cache: %d %s", health.Status, health.String())
	}
	redis.Close()
	if health := srvA.Get("/healthz"); health.Status != 503 {
		t.Fatalf("/healthz answers %d with the cache server gone, want 503: %s", health.Status, health.String())
	}
}
