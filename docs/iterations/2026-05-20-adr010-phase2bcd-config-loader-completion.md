# Iteration archive — 2026-05-20 ADR-010 Phase 2b / 2c / 2d: config loader + merge engine completion

> Archived 2026-05-20 as part of the session-end `/handoff`. Three
> ADR-010 §2 sub-iterations (2b / 2c / 2d) were executed sequentially
> in a single session at the owner's direction ("A, B y C en orden,
> al final de cada fase comitea y empuja"). Each shipped as its own
> feature PR following the convention that feature PRs do not touch
> state files (#61 / #64 / #68 / #70 / #73 precedent). This archive
> records all three together; the state-close itself is this
> `/handoff`.

## Goal

Complete the four-phase slicing of ADR-010 §2 ("Config loading +
merge engine"). Phase 2a (single-file YAML loader) landed 2026-05-16
as PR #73. This session landed the remaining three:

- **Phase 2b** — multi-file merge engine + TOML/JSON parsers +
  `_append`/`_remove` operators + null semantics + non-nullable
  security keys + `WithConfigStrict`.
- **Phase 2c** — `WithUnknownFields` mode selector +
  `NUCLEUS_ENV=production` strict override + startup WARN guards.
- **Phase 2d** — module migration namespacing in `pkg/db`.

## Status

### Done (2026-05-20)

- **ADR-010 Phase 2b — multi-file merge engine** (PR #74, merged as
  `7bb1c51`). `AppBuilder.FromConfigFile(path1, path2, ...)` now
  loads/validates/merges any number of files in left-to-right
  precedence (`defaults < file[0] < … < file[N-1]`). TOML
  (`koanf/parsers/toml/v2`) and JSON (`koanf/parsers/json`) parsers
  wired; the Phase 2a Phase-2b sentinel removed. ADR-010 §3 merge
  semantics: scalars replace, maps deep-merge, lists replace by
  default; `<key>_append` / `<key>_remove` suffix operators (stripped
  before strict-schema check; canonical JSON-marshalled keys for
  deterministic struct-element matching). `null` reverts to the
  `app.DefaultConfig()` value via a `defaultsK` snapshot; non-nullable
  security keys (active set: `jwt_secret`; the four ADR-010 §14
  forward-compat slots documented but deliberately not active until
  their subsystems wire in) reject `null` with `ErrSecurityKeyNotNullable`.
  `AppBuilder.WithConfigStrict(true)` rejects mixed-format file lists
  with `ErrMixedConfigFormats` (default: WARN + proceed). Misorder
  guard on `WithConfigStrict`. Phase 2a wildcard-matcher regression
  fixed: `keyMatchesAny` now recognises `<alias>`/`<site>`/`<tenant>`
  placeholders alongside `*`, and strips the `[]` slice suffix during
  pattern compilation. New exported `pkg/app.NormalizeRuntimeConfig`
  (called from `loadFromFiles` so the returned `*Config` matches the
  env-var path). Freeze baseline +4. New deps: `koanf/parsers/toml/v2`,
  `koanf/parsers/json`, `koanf/providers/confmap`.

- **ADR-010 Phase 2c — strict-mode startup guard** (PR #75, merged as
  `4032771`). `AppBuilder.WithUnknownFields(mode)` accepts the new
  `UnknownFieldsStrict` (default) / `UnknownFieldsWarn` constants;
  warn mode emits a WARN-level slog event listing the offending keys,
  strips them so they don't leak into the merged config, and proceeds.
  `NUCLEUS_ENV=production` (case-insensitive, whitespace-trimmed;
  constant `EnvProduction`) forces strict regardless of code-level
  configuration and emits a structured WARN recording the override.
  Activating warn mode outside production emits a "do not deploy to
  production" startup WARN. Both WARN events use structured slog
  attributes. Misuse guards: invalid mode → `ErrInvalidUnknownFieldsMode`;
  calling after `FromConfigFile` → misorder deferred error. Internal
  `nucleusEnv` function-var indirection wraps `os.Getenv("NUCLEUS_ENV")`
  for test injection. Freeze baseline +5.

- **ADR-010 Phase 2d — module migration namespacing** (PR #76, merged
  as `af1fcc0`). `pkg/db.NewModuleMigrator(db, path, moduleName, logger)`
  records applied-migration and checksum rows under `<moduleName>/<file-id>`
  storage keys in BOTH `nucleus_schema_migrations` and
  `nucleus_schema_migration_checksums` (ADR-010 §16 named only the
  checksums table; a half-namespaced design would still collide on
  the applied-migrations primary key — ADR §16 clarified). Closes the
  cross-module filename-collision class. `NewMigrator` unchanged
  (legacy raw IDs; zero-churn upgrade; promotion to scoped needs a
  one-time manual `UPDATE`). `Migrator.Drift` is ownership-aware:
  unscoped ignores `/`-containing rows, module-scoped only reports its
  own. `Status`/`Drift` return raw file IDs (operators see filenames).
  `applyMigration`/`rollbackMigration` refactored to methods of
  `*Migrator`. Constructor panics on empty / `/`-containing /
  NUL-containing moduleName. `loadMigrations` rejects files whose
  entire name is the `.up.sql`/`.down.sql` suffix. 7 new tests.

### CI

Every PR passed all 11 checks SUCCESS, including the four live-DB
lanes (PostgreSQL + MySQL required; MSSQL + Oracle exploratory).
Semver bump hint across all three: minor (`v0.8.0`) — purely additive.

### Iteration loop

Each sub-phase ran the full 9-subagent loop (architect → code →
security → contract → test → examples-maintainer → doc-updater →
changelog-writer → governance). All returned green-or-WARN/NITS;
findings addressed inline before each commit. Notable resolved
findings:

- Phase 2b: `WithConfigStrict` misorder guard; forward-compat
  security keys trimmed to the active set; `applyRemove` switched to
  JSON-canonical keys (deterministic struct-element matching);
  `pkg/app.NormalizeRuntimeConfig` exported and called from the
  loader (security-auditor MED-2); empty-baseKey operator guard.
- Phase 2c: slog structured-attribute conversion; `nuclearEnv` →
  `nucleusEnv` rename; doc-updater touched `website/docs/concepts/configuration.md`
  + `docs/guides/DEPLOYMENT_GUIDE.md`.
- Phase 2d: ADR-010 §16 both-tables clarification; `ownsStorageID`
  bounds + sub-module-rejection comments; empty-ID guard in
  `loadMigrations`; `Down()`-with-module test; NUL char added to the
  moduleName forbidden set; doc-updater added a
  `MODELING_MULTI_DATABASE.md` §2.6 + a `models-and-database.md`
  website section.

### In progress

- (none)

### Blocked

- (none)

## Carry-forward follow-ups (still open after this session)

1. **Freeze-scanner constructor gap** (surfaced during Phase 2d
   contract-guardian + governance review). `contracts/freeze_test.go::exportedSymbolsForPackage`
   iterates `docPkg.Funcs` but not `typ.Funcs`; `go/doc.New` files
   `NewXxx` constructors (those returning a value of type `Xxx`)
   under the type, so they are invisible to the baseline. `NewMigrator`,
   `NewModuleMigrator`, and many other `pkg/* NewXxx` constructors are
   not tracked. Pre-existing bug, not introduced by Phase 2d.
   Recommended: file a GitHub issue; fix is a mechanical
   `for _, fn := range typ.Funcs` loop followed by a baseline reseed.

2. **`session_cookie_secure` defaults to `false`** (Phase 2b
   security-auditor MED-1). Pre-existing; the non-nullable mechanism
   does not cover it because the default is already permissive. Right
   moment to address: a dedicated security iteration, or fold into the
   `pkg/admin` DDL fix iteration. Consider flipping the default to
   `true` (breaking for local-dev plain HTTP) or adding it to the
   non-nullable set.

3. **ADR-010 §2 layer 3 (range/enum semantic validation).** The
   five-layer validator's layer 3 (field-semantic ranges/enums/parseable
   durations) is out of the four-phase slicing — a standalone follow-up.
   Layers 1 (syntactic) and 2 (schema) are complete.

4. **Phase 1 carry-forwards still open** (none touched by Phase 2):
   service-shutdown timeout, `Lifecycle.OnShutdown` context deadline,
   `routerAdapter.joinPath` double-slash collapse.

## Files of interest

- `pkg/nucleus/config.go` — the Phase 2a→2c loader (size cap, merge
  engine, operators, null, security keys, strict/warn modes).
- `pkg/nucleus/nucleus.go` — `AppBuilder` chain (`FromConfigFile`,
  `WithConfigStrict`, `WithUnknownFields`).
- `pkg/db/migrate.go` — `NewModuleMigrator` + namespacing.
- `docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md` — Status records
  Phases 1–2d landed; Phase 3 / Phase 4 remain.

## Notes / decisions log

- 2026-05-20 — ADR-010 §2 four-phase slicing complete. Phases 2b
  (#74 `7bb1c51`), 2c (#75 `4032771`), 2d (#76 `af1fcc0`) merged in a
  single session; each its own feature PR with the full iteration
  loop. The config loader / merge engine surface is now feature-
  complete for Phase 2; Phase 3 (`/_/config` + `nucleus config print
  --effective`) is the next ADR-010 phase.
- 2026-05-20 — ADR-010 §16 clarified: migration namespacing applies
  to both tracking tables, not only `migrationsChecksumsTable` as the
  original prescriptive text implied.
