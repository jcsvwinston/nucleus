# pkg/cache — contract

Lifecycle: `experimental`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

`Cache` interface (`Get`, `Set`, `Delete`), `NewMemory` (`Memory`, incl.
`PruneExpired`), `NewSQL`/`SQLOptions` (`SQL`, incl. `PruneExpired`),
`DefaultTableName`, `ErrEmptyKey`, `ErrNonPositiveTTL`; the registry —
`Open`, `Config`, `Factory`, `RegisterProvider`, `MustRegisterProvider`,
`RegisteredProviders`,
`ProviderMemory`, `ProviderSQL`, `DefaultKeyPrefix`.

`pkg/cache/rediscache`: `Open`, `Options`, `Cache` (`Get`, `Set`,
`Delete`, `Ping`, `Close`), `ProviderName`.

## Notes

Runtime counterpart of the `createcachetable` CLI command (audit NF-5):
the SQL backend reads and writes the table that command creates
(`nucleus_cache_entries` by default; the CLI's default aliases
`cache.DefaultTableName` so the two cannot drift). Get treats expired rows
as absent — expiry is compared server-side against the database clock —
and `PruneExpired` reclaims their storage. The memory backend is
per-process; the SQL backend is the shared-state option for multi-replica
deployments (see `docs/guides/DEPLOYMENT_GUIDE.md`). Native upserts on
sqlite/postgresql/mysql; transactional delete+insert on mssql/oracle,
matching those CI lanes' exploratory posture. Experimental: `GetOrSet`
helpers may still land before the surface freezes.

The framework builds one cache per application from the `cache` block
(`cache.provider`: `memory` by default, `sql` over the default database,
or a registered name) on every stack, `WithoutDefaults()` included, and
hands it to modules through `nucleus.CacheFrom(rt)` (`App.Cache` for code
holding the `*app.App`). `Open` refuses a name it does not know, and for a
name this project publishes and the binary does not link (`redis` without
`pkg/cache/rediscache`) names `nucleus add redis-cache`; it never falls
back to the memory cache.

`pkg/cache/rediscache` registers `redis` from `init`. `Open` parses the
URL with go-redis (`redis://`, `rediss://`), pings the server with a 5 s
bound and refuses one that does not answer, so a wrong address is a boot
error. Keys are written under `Options.Prefix` (`cache.prefix`, default
`nucleus:cache:`) with `SET … PX`, so the expiry is the server's and every
instance sees an entry disappear at once. A backend with `Ping` is probed
by `/healthz` under `cache`. The client stays behind `cache.Cache`; the
package is in the third-party firewall. `pkg/cache` itself links the
standard library and the framework's catalog only.
