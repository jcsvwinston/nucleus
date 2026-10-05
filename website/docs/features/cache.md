---
sidebar_position: 6
title: Caching
covers:
  - pkg/nucleus.CacheFrom
  - pkg/nucleus.CacheSource
  - pkg/app.CacheConfig
  - pkg/app.App.Cache
config_keys:
  - cache.provider
  - cache.redis_url
  - cache.prefix
  - cache.table
---

# Caching

Every application has a cache. The framework builds it from the `cache`
block of `nucleus.yml` — on every stack, `WithoutDefaults()` included — and
a module reaches it through `nucleus.CacheFrom(rt)`. Three backends sit
behind one interface (`pkg/cache`, experimental):

- **Memory** — the default. In the process, so each instance has its own.
- **SQL** — the table `nucleus createcachetable` creates, in the default
  database: shared by every instance that uses that database.
- **Redis** — a package of the framework that registers itself when
  imported; `nucleus add redis-cache` writes the import and the block.
  Shared by every instance pointed at the same server.

The contract is deliberately small: `Get` returns `(value, ok, err)`, and
an expired entry is indistinguishable from an absent one; `Set` stores a
value with a required positive TTL and replaces any previous entry;
`Delete` removes one (deleting an absent key is not an error). Values are
`[]byte`: serialize with `encoding/json`, `gob`, or whatever fits your data.

## Use it from a module

```go
func Module() nucleus.ModuleSpec {
	var c cache.Cache // github.com/jcsvwinston/nucleus/pkg/cache
	return nucleus.Module[struct{}]{
		Name:   "prices",
		Prefix: "/prices",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			got, ok := nucleus.CacheFrom(rt)
			if !ok {
				return errors.New("prices: the application has no cache")
			}
			c = got
			return nil
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/{sku}", func(ctx *nucleus.Context) error {
				key := "price:" + ctx.Param("sku")
				if v, ok, err := c.Get(ctx.Request.Context(), key); err == nil && ok {
					return ctx.String(http.StatusOK, string(v))
				}
				price := lookUpPrice(ctx.Param("sku")) // the expensive part
				_ = c.Set(ctx.Request.Context(), key, []byte(price), time.Minute)
				return ctx.String(http.StatusOK, price)
			})
		},
	}.Build()
}
```

The cache exists from the moment the application is built, so `OnStart`
can take it. Code that holds the `*app.App` reads `App.Cache`. Which
backend it is is the configuration's decision, not the module's: the same
module caches per process on a laptop and shares its entries in a
deployment that selects Redis.

## Share it between instances: `nucleus add redis-cache`

```bash
nucleus add redis-cache
```

writes the import that registers the backend into `main.go` and a block
into `nucleus.yml`:

```go
import _ "github.com/jcsvwinston/nucleus/pkg/cache/rediscache"
```

```yaml
cache:
  provider: redis
  redis_url: redis://localhost:6379/0
```

Point `cache.redis_url` at your server — or leave it out, and the cache
uses the application's `redis_url`. There is nothing to fetch: the backend
is part of the framework module, and its client library, go-redis, is
already linked by every application (the session store and the health
check use it), so adding it adds no module to the build.

What to expect:

- **Keys are namespaced.** Every key is written under `cache.prefix`
  (default `nucleus:cache:`), so the cache can share a server with the
  session store (`nucleus:sessions:`) and a job queue. Quote a prefix in
  YAML — `prefix: "shop:cache:"` — because a value ending in `:` is
  otherwise read as the start of a mapping.
- **The expiry is the server's.** Entries are stored with `SET … PX`, so an
  entry disappears from every instance at the same moment.
- **The server must answer at startup.** The backend pings it before the
  application starts, and an address that does not answer is a boot error,
  not a first-request one.
- **`/healthz` reports it** under `cache`, and answers 503 when the server
  stops answering.
- **Selected and not installed is refused.** `cache.provider: redis` in a
  binary without the import does not start, and the error names
  `nucleus add redis-cache`. It never falls back to the memory cache: a
  cache that silently stops being shared is how one instance serves what
  another invalidated.

## SQL-backed

Create the table once (or ship the printed DDL as a migration), then select
it:

```bash
nucleus createcachetable            # creates nucleus_cache_entries
nucleus createcachetable --dry-run  # print the SQL instead
```

```yaml
cache:
  provider: sql
  # table: nucleus_cache_entries    # the default
```

Semantics worth knowing:

- **Expiry is enforced server-side.** `Get` filters expired rows against
  the *database* clock, so a replica with a skewed process clock cannot
  resurrect an entry.
- **Expired rows cost storage, not correctness.** Run `PruneExpired` from
  a scheduled job to reclaim them.
- sqlite, postgresql, and mysql use native upserts and are exercised in
  CI; mssql and oracle take a transactional delete+insert path and follow
  the exploratory posture of those database lanes.

## Building a backend yourself

Outside an application — a command, a test — the backends are plain
constructors:

```go
import (
	"github.com/jcsvwinston/nucleus/pkg/cache"
	"github.com/jcsvwinston/nucleus/pkg/cache/rediscache"
)

mem := cache.NewMemory()

shared, err := rediscache.Open(ctx, "redis://cache:6379/0", rediscache.Options{Prefix: "reports:"})
if err != nil {
	return err
}
defer shared.Close()
```

`cache.NewSQL(db, cache.SQLOptions{System: "postgresql"})` builds the SQL
backend over a `*sql.DB` you hold. A backend of your own registers with
`cache.RegisterProvider` from an `init` function, and `cache.provider`
selects it by that name.

## When to use which

| Deployment | Backend |
| --- | --- |
| Single instance | Memory (the default) |
| Several instances, a shared SQL database, modest traffic | SQL |
| Several instances, a hot path | Redis (`nucleus add redis-cache`) |

## Current limits

`pkg/cache` is experimental. There is no `GetOrSet` helper, so nothing
guards against several requests computing the same missing entry at once;
there is no invalidation by prefix or by tag, only by key. The memory
backend drops expired entries lazily (on read, and by an amortised sweep on
writes) and runs no goroutine; `PruneExpired` is there for explicit
maintenance.
