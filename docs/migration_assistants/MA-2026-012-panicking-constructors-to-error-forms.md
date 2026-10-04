# Migration Assistant: constructors that panic on input → their error-returning forms

- ID: `MA-2026-012`
- Pairs with: `docs/deprecations/DEP-2026-012-panicking-constructors.md`
- Severity: `low` — one call per site plus an error check; the value built
  is the same. The deprecated constructors keep working until `v2.0.0`.
- Status: `current`

## Scope

Calls to `auth.NewJWTManager`, `db.NewModuleMigrator`,
`db.NewModuleFSMigrator` and `router.CSRFMiddleware`.

## Detection

```bash
rg -n --type go 'auth\.NewJWTManager\(|db\.NewModuleMigrator\(|db\.NewModuleFSMigrator\(|router\.CSRFMiddleware\(' .

# Exact list, once the application builds against this release:
staticcheck -checks SA1019 ./... 2>&1 | grep -E 'NewJWTManager|NewModuleMigrator|NewModuleFSMigrator|CSRFMiddleware'
```

## Rewrite Plan

| Surface (before) | Surface (after) | Note |
|------------------|-----------------|------|
| `mgr := auth.NewJWTManager(secret, ttl, "iss")` | `mgr, err := auth.NewJWTManagerFromSecret(secret, ttl, "iss")` | error when the secret is shorter than 32 bytes |
| `m := db.NewModuleMigrator(d, "migrations/articles", "articles", logger)` | `m, err := db.NewMigratorFromConfig(db.MigratorConfig{DB: d, Dir: "migrations/articles", Module: "articles"}, logger)` | error for `/` or NUL in the module name |
| `m := db.NewModuleFSMigrator(d, migrationsFS, "articles", logger)` | `m, err := db.NewMigratorFromConfig(db.MigratorConfig{DB: d, FS: migrationsFS, Module: "articles"}, logger)` | error for a nil FS as well |
| `mw := router.CSRFMiddleware(opts)` | `mw, err := router.NewCSRFMiddleware(opts)` | error for an XSRF cookie without a 32-byte key |

Automatic rewrite candidates:

- the call itself, at every site above.

Manual steps:

- decide what the caller does with the error: return it (the usual case,
  in a constructor of the caller's own), or stop the process explicitly
  where it used to panic (`log.Fatal(err)` in `main`).

## Verification

```bash
go build ./...
go test ./...
```

A test that relied on the panic (`recover()` around the call) now checks the
returned error instead.

## Rollback

Both forms coexist until `v2.0.0`; reverting a call site restores the
previous behaviour. No data or configuration is involved.

## Compatibility Notes

Additive: the error forms were added beside the deprecated constructors,
which keep their behaviour, panics included, until the major.
