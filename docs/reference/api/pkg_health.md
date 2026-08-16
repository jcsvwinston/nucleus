# pkg/health — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

`Prober` interface, `Run(ctx, probes, timeout)` aggregator, `NewDBProbe`, `NewRedisProbe`, `NewStorageProbe`, `NewMailProbe`, `SupportsMailProbe`, `Result`

## Notes

Dependency probe abstraction consumed by `App.handleHealthz`. Keeps `github.com/redis/go-redis/v9` wrapped per firewall rules.
