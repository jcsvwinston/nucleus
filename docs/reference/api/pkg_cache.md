# pkg/cache — contract

Lifecycle: `experimental`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

`Cache` interface (`Get`, `Set`, `Delete`), `NewMemory` (`Memory`, incl.
`PruneExpired`), `NewSQL`/`SQLOptions` (`SQL`, incl. `PruneExpired`),
`DefaultTableName`, `ErrEmptyKey`, `ErrNonPositiveTTL`.

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
matching those CI lanes' exploratory posture. Experimental: a Redis
backend and `GetOrSet` helpers may still land before the surface freezes.
Pure stdlib.
