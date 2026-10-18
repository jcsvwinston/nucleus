# Migration Assistant: storage block ignored by a WithoutDefaults() application → `WithStorage()` or no block

- ID: `MA-2026-013`
- Pairs with: `docs/deprecations/DEP-2026-013-ignored-storage-block-without-defaults.md`
- Severity: `high` — an application that carries the combination does not
  start until it is resolved (before v2.0.0 it started, with an ERROR log
  line and a doctor warning).
- Status: `current`

---

## Scope

Applications built `WithoutDefaults()` — the api starter's shape, or
`app.New(cfg, app.WithoutDefaults())` — whose configuration declares
storage (any `storage.*` key in a configuration file, or a non-empty
`NUCLEUS_STORAGE__*` variable) and that do not pass `WithStorage()`. Their
storage block has never been read, and the combination refuses to start
(DEP-2026-013).

Out of scope: applications built with the defaults, applications built
`WithoutDefaults()` that declare no storage, and applications that already
pass `WithStorage()`.

## Detection

**Boot — the application refuses to start, and the error says why:**

```
app.New: New storage: storage block IGNORED: the configuration declares storage and this application is built WithoutDefaults() without WithStorage(), so no store is built (provider=s3; …) — add WithStorage() beside WithoutDefaults() … (DEP-2026-013: refused since v2.0.0)
```

**Doctor — the storage check reports it as an error:**

```bash
nucleus doctor --check storage
# ✗ storage   the configuration declares storage (storage.provider: s3) and main.go builds the application WithoutDefaults() without WithStorage(), so the storage block is IGNORED and the application refuses to start (DEP-2026-013); …
```

**Source — a composition root that opts out of the defaults and not back
into storage:**

```bash
# From the consumer repo root; a hit with no WithStorage in the same file is the case.
grep -n "WithoutDefaults()" main.go cmd/*/main.go 2>/dev/null
grep -n "WithStorage()" main.go cmd/*/main.go 2>/dev/null
```

## Rewrite

| Before | After | Kind |
|---|---|---|
| `WithoutDefaults()` + a declared storage block, no `WithStorage()` | `WithoutDefaults().WithStorage()` — the block is built | manual (one call) |
| the same, where the process must not use storage | the block removed from that process's configuration | manual |

Chosen by intent:

```go
// The application should have the storage its configuration describes:
nucleus.New().
    FromConfigFile("nucleus.yml").
    WithoutDefaults().
    WithStorage(). // builds the declared store; none while nothing is declared
    Start()

// Without the builder:
a, err := app.New(cfg, app.WithoutDefaults(), app.WithStorage())
```

```yaml
# The application should NOT have storage (for example a worker sharing a
# configuration file with the full application): remove the block from the
# configuration this process reads.
storage:            # ← delete
  provider: s3      # ← delete
```

When the selected provider ships as its own module and is not linked, the
error names the command that installs it (`nucleus add <provider>`); with
`WithStorage()` the boot then fails with the storage factory's message until
it is added:

```bash
nucleus add s3   # or gcs, azure
```

## Rollback

There is no ignored state to return to: the choice is `WithStorage()` or no
block (before v2.0.0, removing `WithStorage()` returned the application to
the ignored block plus the log line).

## Validation

After the rewrite, boot the application and confirm:

1. the application starts — no `storage block IGNORED` refusal;
2. with `WithStorage()`, a `storage provider initialized provider=<name>`
   line and a non-nil `Runtime.Storage()`;
3. `nucleus doctor --check storage` no longer reports the block as ignored.
