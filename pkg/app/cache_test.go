// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/cache"
)

// The cache is built from the `cache` block on every stack. This test
// binary does not import pkg/cache/rediscache: `cache.provider: redis` here
// is a configuration whose backend nobody installed.

func writeCacheConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nucleus.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCache_BuiltOnEveryStack_MemoryWhenNothingIsSelected(t *testing.T) {
	for name, opts := range map[string][]Option{
		"default stack":   nil,
		"WithoutDefaults": {WithoutDefaults()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			a, err := New(testAppConfig(), opts...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer func() { _ = a.Shutdown(context.Background()) }()
			if _, ok := a.Cache.(*cache.Memory); !ok {
				t.Fatalf("App.Cache = %T, want the memory cache", a.Cache)
			}
			if err := a.Cache.Set(t.Context(), "k", []byte("v"), time.Minute); err != nil {
				t.Fatalf("Set: %v", err)
			}
		})
	}
}

func TestCache_SQLUsesTheDefaultDatabase(t *testing.T) {
	cfg := testAppConfig()
	cfg.Cache.Provider = "sql"
	a, err := New(cfg, WithoutDefaults())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()
	if _, ok := a.Cache.(*cache.SQL); !ok {
		t.Fatalf("App.Cache = %T, want the SQL cache", a.Cache)
	}
	sqlDB, err := a.DB.SqlDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE "nucleus_cache_entries" ("cache_key" TEXT PRIMARY KEY, "value" BLOB NOT NULL, "expires_at" TEXT NOT NULL, "created_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := a.Cache.Set(t.Context(), "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM nucleus_cache_entries WHERE cache_key = 'k'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the default database holds %d rows for k (err=%v)", n, err)
	}
}

// Selected and not installed: the application does not start, and the
// refusal names the command — it never falls back to the memory cache.
func TestCache_RedisNotInstalled_RefusedNamingTheCommand(t *testing.T) {
	for name, opts := range map[string][]Option{
		"default stack":   nil,
		"WithoutDefaults": {WithoutDefaults()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			cfg := testAppConfig()
			cfg.Cache.Provider = "redis"
			cfg.Cache.RedisURL = "redis://127.0.0.1:1/0"
			a, err := New(cfg, opts...)
			if err == nil {
				_ = a.Shutdown(context.Background())
				t.Fatal("an application selecting a cache backend it does not link started")
			}
			for _, want := range []string{"nucleus add redis-cache", "pkg/cache/rediscache", "not linked into this binary"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not say %q:\n%v", want, err)
				}
			}
		})
	}
}

func TestCacheBlock_StrictCheck(t *testing.T) {
	cfg, err := LoadConfig(writeCacheConfig(t, "cache:\n  provider: redis\n  redis_url: redis://cache:6379/0\n  prefix: \"shop:cache:\"\n  table: shop_cache\n"))
	if err != nil {
		t.Fatalf("the cache block is refused: %v", err)
	}
	if cfg.Cache.Provider != "redis" || cfg.Cache.RedisURL != "redis://cache:6379/0" || cfg.Cache.Prefix != "shop:cache:" || cfg.Cache.Table != "shop_cache" {
		t.Fatalf("decoded %+v", cfg.Cache)
	}

	_, err = LoadConfig(writeCacheConfig(t, "cache:\n  provider: redis\n  redis_ur: redis://cache:6379/0\n"))
	if err == nil || !strings.Contains(err.Error(), "cache.redis_ur (did you mean cache.redis_url?)") {
		t.Fatalf("a misspelt key = %v, want an unknown key with the right suggestion", err)
	}
}

func TestCacheBlock_RedisNeedsAnAddress(t *testing.T) {
	_, err := LoadConfig(writeCacheConfig(t, "cache:\n  provider: redis\n"))
	if !errors.Is(err, ErrInvalidConfigReference) || !strings.Contains(err.Error(), "cache.redis_url (or redis_url)") {
		t.Fatalf("redis without an address = %v", err)
	}
	cfg, err := LoadConfig(writeCacheConfig(t, "redis_url: redis://shared:6379/0\ncache:\n  provider: redis\n"))
	if err != nil {
		t.Fatalf("redis with the application's redis_url is refused: %v", err)
	}
	if got := cfg.Cache.redisURL(cfg); got != "redis://shared:6379/0" {
		t.Fatalf("the cache resolves its server as %q, want the application's redis_url", got)
	}
}

func TestCache_DevProfileSelectsMemory(t *testing.T) {
	cfg := testAppConfig()
	cfg.Profile = "dev"
	cfg.Cache = CacheConfig{Provider: "redis", RedisURL: "redis://prod:6379/0"}
	if err := ApplyProfile(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Cache.Provider != "memory" || cfg.Cache.RedisURL != "" {
		t.Fatalf("the dev profile left the cache at %+v", cfg.Cache)
	}
}
