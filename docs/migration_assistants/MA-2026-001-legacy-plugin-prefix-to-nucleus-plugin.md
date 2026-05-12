# Migration Assistant: Legacy plugin prefix → `nucleus-plugin-*`

- ID: `MA-2026-001`
- Pairs with: `docs/deprecations/DEP-2026-001-legacy-plugin-prefixes.md`
- Severity: `low`
- Status: `current` (paired deprecation is `removed` as of `v0.6.0`)

## Scope

Plugin discovery binaries on `PATH` consumed by the Nucleus plugin host (`pkg/plugins`). Affects:

- generic plugins previously named `goframe-plugin-<provider>`;
- mail drivers previously discovered via the transitional bridge `goframe-mail-<driver>`.

Both are migrated to the single canonical prefix `nucleus-plugin-<provider>` recognised by `pkg/plugins.GenericBinaryPrefix`.

This MA is intentionally scoped to the binary-naming surface only. It does **not** cover the broader `GoFrame → Nucleus` rename (module path, CLI command name, config filename, package rename), which per ADR-003 §"Compatibility policy" is shipped without transition aliases and without a sweeping rename MA.

## Detection

Run, from the consumer repo root and any directory on `PATH` used for plugin distribution:

```
# 1. Source / config references to the legacy prefixes
rg -n 'goframe-(plugin|mail)-' .

# 2. Installed binaries on PATH that match the legacy prefixes
ls $(echo "$PATH" | tr ':' '\n') 2>/dev/null \
  | grep -E '^goframe-(plugin|mail)-' || true

# 3. Nucleus's own view (post-migration this should list providers under the new names)
nucleus plugin list --config nucleus.yml
nucleus mailproviders --config nucleus.yml
```

A consumer is impacted if any of the three commands surfaces a `goframe-(plugin|mail)-*` name.

## Rewrite Plan

Mechanical binary rename. No source-level API change is required if the plugin already targets the current `pkg/plugins` protocol (`pkg/plugins/runtime.go`, `pkg/plugins/schema.go`); the host/plugin handshake is unchanged.

| Before (legacy)             | After (canonical)              | Note                                                        |
|-----------------------------|--------------------------------|-------------------------------------------------------------|
| `goframe-plugin-<provider>` | `nucleus-plugin-<provider>`    | Generic plugin executable.                                  |
| `goframe-mail-<driver>`     | `nucleus-plugin-<driver>`      | Mail drivers now resolve through the unified plugin prefix. |

Automatic rewrite candidates:

- Binary filenames in install scripts, `Makefile`/`justfile` targets, Dockerfiles, and CI artifact uploads.
- Source-level string literals matching `^goframe-(plugin|mail)-` (verify the match is a binary name, not a historical reference in a CHANGELOG/ADR).

Manual steps:

- Package metadata (Homebrew formulas, deb/rpm package names, container image tags) that embed the binary name.
- Any external documentation referencing the legacy prefix.

## Verification

After renaming all impacted binaries and updating their installers:

```
# Fail-fast: the host must not detect any legacy-named binary.
ls $(echo "$PATH" | tr ':' '\n') 2>/dev/null \
  | grep -E '^goframe-(plugin|mail)-' && { echo "legacy binary still on PATH"; exit 1; } || true

# Positive: each expected provider appears under the new name.
nucleus plugin list --config nucleus.yml
nucleus mailproviders --config nucleus.yml

# If the consumer has plugin smoke tests, re-run them. The host/plugin
# protocol is unchanged, so existing tests apply without modification.
go test ./...
```

## Rollback

The legacy prefix is removed code-side in `v0.6.0`; pinning the framework to `v0.5.x` is the only rollback path and re-introduces the pre-rename module path (`github.com/jcsvwinston/GoFrame`). Practically, no consumer should need this — the framework had no published consumers when the legacy prefix was retired.

If a consumer must coexist with a partially-renamed deployment during a staged rollout, the safe transitional step is to ship both binaries simultaneously (`goframe-plugin-<provider>` and `nucleus-plugin-<provider>` as hard links or copies). Only the `nucleus-plugin-*` name is detected by `v0.6.0+`; the `goframe-*` copy is inert from the framework's perspective and exists purely to satisfy external tooling that has not yet been updated. Remove the legacy copy as soon as the external tooling is updated.

## Compatibility Notes

- Additive-first: renaming the binary is non-destructive when done by copy; deleting the legacy binary is the only destructive step and is safe once verification passes.
- Reproducible in CI: the detection and verification steps are scriptable with no interactive prompts. They are appropriate for inclusion in a release-readiness job for any consumer maintaining external plugins.
