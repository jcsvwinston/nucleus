// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package catalogbench

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/jcsvwinston/nucleus/internal/testredis"
)

// EN-06 — redis-cache. The entry is a package of the framework that
// registers the Redis backend of pkg/cache by import, and a configuration
// block that selects it; what it is for is a cache every instance of the
// application shares. So the wiring check starts TWO instances of the
// starter `nucleus add redis-cache` left, writes a value through the
// framework's cache in one and reads it in the other. The memory cache
// would answer the first instance and not the second: two processes reading
// each other's values is what only a shared backend does.
//
// The server is the one NUCLEUS_CACHE_REDIS_URL names — the "Module Jobs
// (real Redis)" lane sets it — and miniredis otherwise, in a process of
// its own (internal/testredis).

// redisCacheImport is the package `nucleus add redis-cache` imports.
const redisCacheImport = "github.com/jcsvwinston/nucleus/pkg/cache/rediscache"

// redisCachePlaceholder is the server the recipe writes; the person points
// it at theirs, and so does the probe.
const redisCachePlaceholder = "redis_url: redis://localhost:6379/0"

// benchCacheModule is the code a person writes once the cache is there: a
// module that takes it in OnStart and caches through it. No catalog entry
// can write it — what is cached is the application's.
const benchCacheModule = `package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/cache"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

func benchCache() nucleus.ModuleSpec {
	var c cache.Cache
	return nucleus.Module[struct{}]{
		Name:   "benchcache",
		Prefix: "/bench-cache",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			got, ok := nucleus.CacheFrom(rt)
			if !ok {
				return errors.New("benchcache: the application has no cache")
			}
			c = got
			return nil
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Put("/{key}", func(ctx *nucleus.Context) error {
				body, err := io.ReadAll(ctx.Request.Body)
				if err != nil {
					return err
				}
				if err := c.Set(ctx.Request.Context(), ctx.Param("key"), body, time.Minute); err != nil {
					return err
				}
				return ctx.NoContent()
			})
			r.Get("/{key}", func(ctx *nucleus.Context) error {
				value, ok, err := c.Get(ctx.Request.Context(), ctx.Param("key"))
				if err != nil {
					return err
				}
				if !ok {
					return ctx.String(http.StatusNotFound, "absent")
				}
				return ctx.String(http.StatusOK, string(value))
			})
		},
	}.Build()
}
`

func addBenchCache(t *testing.T, dir string) {
	must(t, os.WriteFile(filepath.Join(dir, "benchcache.go"), []byte(benchCacheModule), 0o644))
	mainGo := filepath.Join(dir, "main.go")
	src, err := os.ReadFile(mainGo)
	must(t, err)
	edited := strings.Replace(string(src), "\t\tStart(); err != nil", "\t\tMount(benchCache()).\n\t\tStart(); err != nil", 1)
	if edited == string(src) {
		t.Fatalf("the starter's main.go has no Start() to mount the cache module before:\n%s", src)
	}
	must(t, os.WriteFile(mainGo, []byte(edited), 0o644))
}

// standInRedis is the server the probe points the starter at, and a client
// that reads it directly.
func standInRedis(t *testing.T) (url string, client *goredis.Client, kind string) {
	t.Helper()
	if real := strings.TrimSpace(os.Getenv("NUCLEUS_CACHE_REDIS_URL")); real != "" {
		opts, err := goredis.ParseURL(real)
		if err != nil {
			t.Fatalf("NUCLEUS_CACHE_REDIS_URL: %v", err)
		}
		client = goredis.NewClient(opts)
		t.Cleanup(func() { _ = client.Close() })
		return real, client, "a real Redis (NUCLEUS_CACHE_REDIS_URL)"
	}
	srv := testredis.Run(t)
	client = goredis.NewClient(&goredis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return "redis://" + srv.Addr() + "/0", client, "miniredis (internal/testredis)"
}

func probeEntryRedisCache(t *testing.T, e *env) verdict {
	var (
		redisURL, kind string
		client         *goredis.Client
	)
	c := coreEntry{
		name: "redis-cache",
		code: addBenchCache,
		// What the person does after `nucleus add redis-cache`: point the
		// cache at their server.
		edit: func(t *testing.T, _ *env, config string) string {
			redisURL, client, kind = standInRedis(t)
			if !strings.Contains(config, redisCachePlaceholder) {
				t.Logf("nucleus.yml carries no %q to replace; booting it as written", redisCachePlaceholder)
				return config
			}
			return strings.Replace(config, redisCachePlaceholder, "redis_url: "+redisURL, 1)
		},
		check: func(t *testing.T, _ *env, run coreRun) (bool, string) {
			return cacheSharedBetweenInstances(t, run, client, kind)
		},
	}
	return probeCoreEntry(t, e, c, func(t *testing.T, _ *env) bool {
		// Before the entry, there is no Redis backend to wire by hand.
		t.Logf("pkg/cache has no Redis backend to wire by hand")
		return false
	})
}

// cacheSharedBetweenInstances is EN-06's wiring check: a value written
// through the framework's cache in one instance is read in a second
// instance started from the same binary and configuration, and the key is
// in the Redis server under the cache's namespace.
func cacheSharedBetweenInstances(t *testing.T, run coreRun, client *goredis.Client, kind string) (bool, string) {
	if !strings.Contains(run.output(), `msg="cache provider initialized" provider=redis`) {
		return false, "the boot log does not say the redis cache was initialized:\n" + firstLines(run.output(), 12)
	}
	key := fmt.Sprintf("bench-%d", time.Now().UnixNano())
	value := "written by the first instance"
	status := request(http.MethodPut, run.port, "/bench-cache/"+key, value)
	if status != http.StatusNoContent {
		return false, fmt.Sprintf("PUT /bench-cache/%s on the first instance answered %d", key, status)
	}

	var secondStatus int
	var secondBody string
	second := bootIn(t, t.TempDir(), run.bin, run.config, nil, func(port int, _ func() string) {
		secondStatus, secondBody = get(port, "/bench-cache/"+key)
	})
	if !second.listening {
		return false, fmt.Sprintf("the second instance does not start: %v\n%s", second.exitErr, firstLines(second.output, 10))
	}
	stored, err := client.Get(context.Background(), "nucleus:cache:"+key).Result()
	evidence := fmt.Sprintf("against %s: PUT on the first instance answered 204; GET on a second instance answered %d %q; "+
		"the server holds %q under nucleus:cache:%s (err=%v)", kind, secondStatus, secondBody, stored, key, err)
	return secondStatus == http.StatusOK && secondBody == value && stored == value, evidence
}

// request sends a body to an application on port and returns the status.
func request(method string, port int, path, body string) int {
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), strings.NewReader(body))
	if err != nil {
		return 0
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return 0
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}
