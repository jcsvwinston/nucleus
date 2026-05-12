# Deprecation Notice: Legacy plugin discovery prefixes (`goframe-plugin-*`, `goframe-mail-*`)

- ID: `DEP-2026-001`
- Status: `removed`
- Announced in: `v0.6.0`
- Earliest removal: `v0.6.0`
- Scope: `plugin`
- Affected lifecycle tag: `transitional`
- Owner: `@jcsvwinston`

## Summary

Filed retroactively to document the removal of the GoFrame-era plugin binary discovery prefixes as part of the GoFrame → Nucleus rename (ADR-003). Two distinct discovery surfaces were collapsed into a single canonical prefix:

- the generic plugin host previously scanned `PATH` for executables named `goframe-plugin-<provider>`;
- the mail subsystem had a transitional fallback that additionally scanned for `goframe-mail-<driver>` executables (the "legacy mail bridge"), exposed in code as the `pkg/plugins.LegacyMailBinaryPrefix` constant.

Both are removed. `pkg/plugins` recognises only `nucleus-plugin-<provider>` (see `pkg/plugins.GenericBinaryPrefix`). The framework has no published consumers (pre-v1, single-maintainer), so the removal was executed in a single release without an extended deprecation window. This notice exists to keep the documentation surface honest and to exercise the format defined by `docs/governance/DEPRECATION_TEMPLATE.md` against a real artifact.

## Affected Surfaces

- Binary discovery prefix `goframe-plugin-<provider>` (any `PATH` executable matching that pattern is no longer detected).
- Binary discovery prefix `goframe-mail-<driver>` (legacy mail bridge; no longer detected).
- Constant `pkg/plugins.LegacyMailBinaryPrefix` (removed from the package).
- CLI commands `nucleus plugin list` and `nucleus mailproviders` no longer enumerate executables under the legacy prefixes.

## Migration Path

- Replacement: rename plugin binaries to `nucleus-plugin-<provider>`. This is the only recognised discovery prefix.
- Behavior differences: discovery is identical in shape (`PATH` scan, longest-match on the prefix, provider name derived by trimming the prefix and any platform `.exe` suffix). The protocol contract between host and plugin (`pkg/plugins/runtime.go`, `pkg/plugins/schema.go`) is unchanged.
- Required app changes: for any out-of-tree plugin executable, rename the binary and the corresponding install/packaging artifacts. No source-level API change is required if the plugin already targets the current `pkg/plugins` protocol.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-001-legacy-plugin-prefix-to-nucleus-plugin.md`
- Detection rule: `rg -n 'goframe-(plugin|mail)-' .` against the consumer's repo and a `PATH` scan for matching binary names.
- Suggested rewrite: mechanical binary rename `goframe-plugin-<provider>` → `nucleus-plugin-<provider>`; `goframe-mail-<driver>` → `nucleus-plugin-<driver>` (the mail subsystem resolves mail drivers through the same `nucleus-plugin-*` namespace).

## Validation

- Compatibility tests updated: `yes` — `pkg/plugins/plugins_test.go` and `pkg/plugins/helpers_test.go` assert that `GenericBinaryPrefix == "nucleus-plugin-"` and reject other prefixes.
- Release note updated: `yes` — see `CHANGELOG.md` entry for `v0.6.0` under `Removed`.
- Rollback plan documented: `yes` (this notice, "Rollback" in the paired MA).

## Timeline

- Announcement date: `2026-05-09`
- Review checkpoint: `2026-05-09` (retroactive single-step lifecycle: removal coincided with announcement under ADR-003 §"Compatibility policy").
- Removal decision date: `2026-05-09`

## Notes

ADR-003 §50 states that no migration assistant is produced for the overall `GoFrame → Nucleus` rename, because the framework has no external consumers and a sweeping rename-migration MA would have no audience. This deprecation notice and its paired MA are intentionally scoped to a narrower surface — the legacy plugin/mail binary discovery prefixes — which is documentation-worthy on its own merits and is the smallest concrete instance of the rename's binary-naming impact.
