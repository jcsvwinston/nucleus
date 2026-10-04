# Migration Assistant: storage block ignored by a WithoutDefaults() application → `WithStorage()` or no block

- ID: `MA-2026-013`
- Pairs with: `docs/deprecations/DEP-2026-013-ignored-storage-block-without-defaults.md`
- Severity: `low` before v2.0.0 (an ERROR log line and a doctor warning; the
  application behaves as before); `high` at v2.0.0 for applications that
  still carry the combination — they stop starting until it is resolved.
- Status: `current`

---

## Scope

Applications built `WithoutDefaults()` — the api starter's shape, or
`app.New(cfg, app.WithoutDefaults())` — whose configuration declares
storage (any `storage.*` key in a configuration file, or a non-empty
`NUCLEUS_STORAGE__*` variable) and that do not pass `WithStorage()`. Their
storage block has never been read; from v2.0.0 the combination refuses to
start (DEP-2026-013).

Out of scope: applications built with the defaults, applications built
`WithoutDefaults()` that declare no storage, and applications that already
pass `WithStorage()`.

## Detection

**Logs — one ERROR line at boot (this release onward):**

```
level=ERROR msg="storage block IGNORED: the configuration declares storage and this application is built WithoutDefaults() without WithStorage(), so no store is built" … deprecation="DEP-2026-013: from v2.0.0 this configuration refuses to start"
```

**Doctor — the storage check reports it as a warning:**

```bash
nucleus doctor --check storage
# ! storage   the configuration declares storage (storage.provider: s3) and main.go builds the application WithoutDefaults() without WithStorage(), so the storage block is IGNORED at boot; …
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
log line carries `install="… nucleus add <provider>"`; with `WithStorage()`
the boot then fails with the storage factory's message until it is added:

```bash
nucleus add s3   # or gcs, azure
```

## Rollback

- Before v2.0.0: removing `WithStorage()` returns the application to the
  ignored block plus the log line.
- After v2.0.0: there is no ignored state to return to; the choice is
  `WithStorage()` or no block.

## Validation

After the rewrite, boot the application and confirm:

1. no `storage block IGNORED` line in the boot log;
2. with `WithStorage()`, a `storage provider initialized provider=<name>`
   line and a non-nil `Runtime.Storage()`;
3. `nucleus doctor --check storage` no longer reports the block as ignored.
