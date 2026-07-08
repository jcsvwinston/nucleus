# Deprecation Notice: legacy flat `storage_driver`/`storage_path` config keys

- ID: `DEP-2026-005`
- Status: `active`
- Announced in: `Unreleased` (v1 gate A-2 / slice 5 preparation)
- Earliest removal: `v0.12.0` (aligned with the v0.11 → v0.12 → v1.0 release
  train fixed by DEP-2026-004, per `docs/governance/COMPATIBILITY_SLO.md`)
- Scope: `config`
- Affected lifecycle tag: `stable` (marked `deprecated` in the registry since
  the nested `storage.*` block shipped)
- Owner: `@jcsvwinston`

## Summary

The flat `storage_driver`/`storage_path` keys predate the nested `storage.*`
configuration block (`storage.provider`, `storage.local.path`, provider
sub-blocks, cleanup, circuit breaker). The registry has marked them
`deprecated` since the nested block shipped, but until now the framework
consumed them **silently**: the `storage_path` fallback in
`Config.toStorageConfig` and the CLI reads in `doctor`/`health` emitted no
signal. This notice formalizes the deprecation and adds the missing WARN.

`pkg/app` now emits a one-time startup `WARN` through the application logger
when either legacy key is actively configured:

```
config: storage_driver/storage_path are deprecated, use the nested
storage.* keys (storage.provider, storage.local.path); the legacy keys
will be removed in a future release
```

Detection nuance: `DefaultConfig` pre-populates both legacy keys (`"local"`,
`"uploads/"`), so presence alone is not a signal — the WARN fires only when a
value **deviates from those defaults**, i.e. the deployment is really using
the deprecated keys. A deployment that explicitly sets the legacy keys to the
default values is not detected; its behaviour is identical to the default, so
the residual is harmless. The WARN is implemented in `warnLegacyStorageKeys`
(`pkg/app/app.go`) via a `sync.Once`-guarded logger call.

## Affected Surfaces

- `storage_driver` — YAML / environment-variable config key.
  `Config.StorageDriver string` (`koanf:"storage_driver"`), `pkg/app/config.go`.
- `storage_path` — YAML / environment-variable config key.
  `Config.StoragePath string` (`koanf:"storage_path"`), same file.
- Internal consumers that must migrate in the removal release:
  the `storage_path` fallback in `Config.toStorageConfig`, the `DefaultConfig`
  seeding of both fields, and the CLI reads in `internal/cli/doctor.go` and
  `internal/cli/health.go` (both still consult `cfg.StorageDriver`).

## Migration Path

- Replacement: `storage_driver: X` → `storage.provider: X`;
  `storage_path: Y` → `storage.local.path: Y`.
- Behavior differences: none for `local`. The nested block additionally
  unlocks provider sub-configs (`storage.s3.*`, `storage.gcs.*`,
  `storage.azure.*`), cleanup and circuit-breaker settings that the flat keys
  never exposed.
- Required app changes: a two-line YAML move. Go code setting
  `Config.StorageDriver`/`Config.StoragePath` moves to
  `Config.Storage.Provider` and `Config.Storage.Local.Path` (the `Local`
  block is an anonymous struct field of `StorageConfig`; assign the fields,
  not a named literal — see `pkg/app/config.go`).

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-005-flat-storage-keys-to-nested.md`
- Detection rule: presence of `storage_driver` or `storage_path` in any
  YAML/TOML config file with a non-default value, use of
  `Config.StorageDriver`/`Config.StoragePath` in Go source, or startup `WARN`
  matching `"storage_driver/storage_path are deprecated"` in application logs.
- Suggested rewrite: mechanical two-line move (YAML) / struct-field move (Go).

## Validation

- Compatibility tests updated: `n/a at announcement` — the keys remain
  registered and functional until removal; the removal release rebaselines
  `contracts/baseline/config_key_patterns.txt` deliberately.
- Release note updated: pending merge (conventional commit feeds
  release-please notes).
- Rollback plan documented: `yes` — the nested block and the flat keys
  coexist until removal; reverting the YAML move restores the prior state.

## Timeline

- Announcement date: `2026-07-07`
- Review checkpoint: at `v0.11.0` release prep — confirm the WARN ships and
  measure known consumers still setting non-default legacy values.
- Removal decision date: `v0.12.0` release prep — remove the keys, the
  `toStorageConfig` fallback, the `DefaultConfig` seeding, and migrate the
  `doctor`/`health` CLI reads; rebaseline deliberately (v1 gate A-2).
