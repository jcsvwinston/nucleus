# Migration Assistant: flat `storage_driver`/`storage_path` → nested `storage.*`

- ID: `MA-2026-005`
- Pairs with: `docs/deprecations/DEP-2026-005-legacy-flat-storage-keys.md`
- Severity: `low` — mechanical two-line key move; the flat keys keep working
  (with a one-time startup WARN) until removal. No runtime behaviour change
  for the `local` provider.
- Status: `current`

---

## Scope

Applications that configure file storage via the flat `storage_driver` /
`storage_path` keys in `nucleus.yml` (or any Nucleus-loaded YAML/TOML file,
or via `Config.StorageDriver`/`Config.StoragePath` in Go code). These keys
predate the nested `storage.*` block and only ever expressed the provider
name and the local path.

Out of scope: applications already on the nested block (`storage.provider`,
`storage.local.path`, provider sub-blocks) — the flat keys are ignored when
the nested values are set.

## Detection

**Config file — search for the legacy keys:**

```bash
# From the consumer repo root.
grep -rn "^storage_driver:\|^storage_path:" *.yml *.yaml 2>/dev/null
```

**Go source — search for the legacy fields:**

```bash
grep -rn "StorageDriver\|StoragePath" --include="*.go" .
```

**Logs — the one-time startup WARN:**

```
config: storage_driver/storage_path are deprecated, use the nested
storage.* keys (storage.provider, storage.local.path); the legacy keys
will be removed in a future release
```

Note: values equal to the defaults (`local`, `uploads/`) do not trigger the
WARN — behaviour is identical to the default configuration and no migration
is strictly required, though moving to the nested keys is still recommended
before the removal release.

## Rewrite

**YAML:**

```yaml
# before (deprecated, use storage.*)
storage_driver: local
storage_path: data/uploads/

# after
storage:
  provider: local
  local:
    path: data/uploads/
```

**Go:** assign `Config.Storage.Provider` and `Config.Storage.Local.Path`
instead of `Config.StorageDriver`/`Config.StoragePath` (the `Local` block is
an anonymous struct field of `StorageConfig` — assign the fields, not a named
literal).

The nested block additionally exposes provider sub-configs
(`storage.s3.*`, `storage.gcs.*`, `storage.azure.*`), cleanup and
circuit-breaker settings that the flat keys never had; adopting a remote
provider is a follow-on config change, not part of this mechanical move.

## Rollback

The flat keys and the nested block coexist until the removal release
(`v0.12.0` target, per DEP-2026-005). Reverting the YAML move restores the
previous state with no data impact — storage contents are untouched by
configuration shape.

## Validation

After the move, boot the app and confirm:

1. no deprecation WARN mentioning `storage_driver/storage_path` in startup
   logs;
2. `nucleus doctor --config nucleus.yml` reports the same storage provider
   as before the move;
3. uploads land under the same path.
