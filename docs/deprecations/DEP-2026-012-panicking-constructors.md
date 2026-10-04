# Deprecation Notice: constructors that panic on input → their error-returning forms

- ID: `DEP-2026-012`
- Status: `active`
- Announced in: `Unreleased` (the minor after `v1.30.1`; A10 S9, 2026-10-04)
- Earliest removal: `v2.0.0`, no earlier than 2027-01-02 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md`; the suite groups every breaking
  change into the one major at the close of its A12 arc
- Scope: `api`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

Four exported constructors panic on input that comes from configuration or
the environment, while each type has (or now has) a constructor that returns
the same check as an error. Two constructors for one type with two contracts
is the inconsistency the constructor style guide
(`docs/governance/CONSTRUCTOR_STYLE.md`) closes: bad input is an error, never
a panic. The panicking forms keep working, unchanged, until the major.

## Affected Surfaces

| Deprecated | Panics on | Use instead |
|---|---|---|
| `auth.NewJWTManager(secret, expiry, issuer...)` | a secret shorter than 32 bytes | `auth.NewJWTManagerFromSecret(secret, expiry, issuer...) (*JWTManager, error)` — new |
| `db.NewModuleMigrator(db, dir, module, logger)` | an empty module name; `/` or NUL in it | `db.NewMigratorFromConfig(db.MigratorConfig{DB: db, Dir: dir, Module: module}, logger) (*Migrator, error)` — new |
| `db.NewModuleFSMigrator(db, fsys, module, logger)` | a nil `fs.FS`; an empty module name; `/` or NUL in it | `db.NewMigratorFromConfig(db.MigratorConfig{DB: db, FS: fsys, Module: module}, logger)` — new |
| `router.CSRFMiddleware(opts)` | `EnableXSRFCookie` without a 32-byte `EncryptionKey`; a `__Host-`/`__Secure-` cookie name with `InsecureCookie` | `router.NewCSRFMiddleware(opts) (func(http.Handler) http.Handler, error)` — already present |

## Migration Path

- Replacement: the table above. The new forms take the same arguments (or
  the same values in a config struct) and build the same value.
- Behavior differences: on bad input they return an error instead of
  panicking. `db.NewMigratorFromConfig` also accepts an empty `Module` with
  either source (the unscoped ledger `nucleus migrate` uses), and returns an
  error when neither or both of `Dir` and `FS` are set.
- Required app changes: handle the returned error where the old call was.
  An application that wanted the crash keeps it explicitly:
  `if err != nil { log.Fatal(err) }`.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-012-panicking-constructors-to-error-forms.md`
- Detection rule: calls to the four names; gopls and `staticcheck` (SA1019)
  flag them once the application is on this release.
- Suggested rewrite: mechanical per call site, plus the error check.

## Validation

- Compatibility tests updated: `yes` — `NewJWTManagerFromSecret`,
  `NewMigratorFromConfig` and `MigratorConfig` are listed in
  `contracts/baseline/api_exported_symbols.txt` (additions only); the
  deprecated forms keep their tests, panics included. The API bench control
  `DI-07` scans `pkg/` for exported functions that panic without a declared
  error form and calls the error forms with bad input.
- Release note updated: through the squash commit, which feeds the
  release-please notes.
- Rollback plan documented: `yes` — both forms coexist until `v2.0.0`.

## Timeline

- Announcement date: `2026-10-04`
- Review checkpoint: the set that certifies the release carrying this notice.
- Removal decision date: the `v2.0.0` major.

## Notes

The framework's own callers moved in the same change: `pkg/app` builds its
JWT manager with `NewJWTManagerFromSecret`, `Runtime.ApplyModuleMigrations`
uses `NewMigratorFromConfig`, the kit's `MintToken` uses the error form, and
the router's default stack builds its CSRF middleware without the panicking
wrapper. ADR-006 chose the panicking posture for `CSRFMiddleware` and
documented `NewCSRFMiddleware` beside it; this notice keeps that choice
available until the major and makes the error form the one to use.
