---
sidebar_position: 6
title: Caching
---

# Caching

`pkg/cache` (experimental) is a minimal key/value cache with TTL semantics
and two backends behind one interface:

- **Memory** — in-process, for single-replica deployments.
- **SQL** — backed by the table `nucleus createcachetable` creates
  (`nucleus_cache_entries` by default), for replicas that share a database.

The contract is deliberately small: `Get` returns `(value, ok, err)` where
an expired entry is indistinguishable from an absent one; `Set` stores a
value with a required positive TTL and replaces any previous entry;
`Delete` removes one (deleting an absent key is not an error).

## In-memory

```go
import "github.com/jcsvwinston/nucleus/pkg/cache"

c := cache.NewMemory()
_ = c.Set(ctx, "user:42:profile", payload, 5*time.Minute)
value, ok, err := c.Get(ctx, "user:42:profile")
```

Expired entries are dropped lazily (on read, and by an amortised sweep on
writes); there is no background goroutine, so nothing needs shutting down.
`PruneExpired` is available for explicit maintenance.

Memory is per-process: two replicas each have their own. For shared state,
use the SQL backend.

## SQL-backed

Create the table once (or ship the printed DDL as a migration):

```bash
nucleus createcachetable            # creates nucleus_cache_entries
nucleus createcachetable --dry-run  # print the SQL instead
```

Then wire the backend over the app's managed pool:

```go
sqlDB, err := a.DB.SqlDB()
if err != nil {
    return err
}
c, err := cache.NewSQL(sqlDB, cache.SQLOptions{
    System: a.DB.System(), // "sqlite", "postgresql", "mysql", "mssql", "oracle"
})
```

`System` selects the placeholder style, the upsert form, and the UTC clock
expression per engine. `Table` defaults to the table the CLI creates; both
sides share the constant, so the command and the runtime cannot drift.

Semantics worth knowing:

- **Expiry is enforced server-side.** `Get` filters expired rows against
  the *database* clock, so a replica with a skewed process clock cannot
  resurrect an entry.
- **Expired rows cost storage, not correctness.** Run `PruneExpired` from
  a scheduled task or cron to reclaim them.
- sqlite, postgresql, and mysql use native upserts and are exercised in
  CI; mssql and oracle take a transactional delete+insert path and follow
  the exploratory posture of those database lanes.

## When to use which

| Deployment | Backend |
| --- | --- |
| Single replica | Memory |
| Multiple replicas, shared SQL database | SQL |
| Multiple replicas, latency-sensitive hot path | An external store (e.g. Redis) — no built-in backend yet |

## Current limits

`pkg/cache` is experimental: there is no Redis backend, no `GetOrSet`
helper, and no framework-level wiring (you construct the backend yourself,
as above). Values are `[]byte`; serialize with `encoding/json`, `gob`, or
whatever fits your data.
