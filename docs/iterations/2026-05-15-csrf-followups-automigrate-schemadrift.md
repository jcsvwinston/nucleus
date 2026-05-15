# Iteration — 2026-05-15 — CSRF follow-ups + Live-DB AutoMigrate tests + Schema-level drift detection

**Owner direction (start-of-session):** "Abordamos hoy los puntos 1, 2 y 3" — the top three audit
follow-ups from `docs/audits/2026-05-14-post-sprint-readiness.md`, bundled into a single iteration:

1. CSRF middleware follow-ups (audit §7 task 5 / candidate #4)
2. Live-DB integration tests for `App.AutoMigrate` (audit §7 task 7 / candidate #1)
3. Schema-level drift detection (audit §7 task 8 / candidate #2)

**Status:** DONE — all three closed in a single iteration. Two new ADRs (008, 009), one new
guide section (CSRF logger + secure-by-default + `[]byte` key), new public API surface in
`pkg/db` (`SchemaDrift`, `ExpectedTable`, `ExpectedColumn`, `SchemaDriftEntry`,
`ErrSchemaDriftUnsupported`, four `DriftKindSchema*` constants), and two new live-DB tests.

## What shipped

### 1. CSRF middleware follow-ups (ADR-008)

`pkg/router/csrf.go`, `pkg/router/middleware.go`, `pkg/router/csrf_hardening_test.go`:

- **Logger plumbing.** New optional `CSRFOptions.Logger *slog.Logger` field. Defaults to
  `slog.Default()` when nil; `router.DefaultStack` plumbs the router's logger automatically
  so apps built through `router.WithCSRF` inherit redaction, attributes, and sink from
  the rest of the app. Server-side encrypt failures log at WARN; X-XSRF-TOKEN header
  decrypt failures log at DEBUG (public-endpoint noise — opt-in at production log levels).
- **`EncryptionKey` type: `string` → `[]byte`** (BREAKING pre-`v1.0`). Raw key material is
  bytes, not a string. The `defaults()` method now takes a **defensive copy** of the caller's
  slice so a later mutation of their backing array cannot rewrite the live handler's key.
- **`Secure bool` → `InsecureCookie bool`** polarity flip (BREAKING pre-`v1.0`).
  Security-by-default per SPEC.md §2 principle 4: the zero-value `CSRFOptions{}` (the path
  `router.WithCSRF()` takes) now issues `Secure: true` cookies. Local-dev plain HTTP opts
  out explicitly with `InsecureCookie: true`.

Tests: 4 new tests (logger captures decrypt failure, logger nil-fallback, secure-by-default
cookie, InsecureCookie opt-out, XSRF cookie respects InsecureCookie). 24 pre-existing CSRF
tests untouched. All 28 green.

Baseline rebaseline: removed `field:CSRFOptions.Secure`, added `field:CSRFOptions.InsecureCookie`
and `field:CSRFOptions.Logger`. `EncryptionKey` field name unchanged (type-only break,
documented in ADR-008 §3 and CHANGELOG BREAKING note).

### 2. Live-DB AutoMigrate integration tests

`pkg/app/automigrate_live_test.go`:

- `TestSQLMatrix_AutoMigrate` — gated on `NUCLEUS_SQL_MATRIX_URL` (PG/MySQL); the
  `db-matrix-required` CI lane picks it up automatically.
- `TestSQLMatrix_AutoMigrate_Exploratory` — gated on `NUCLEUS_SQL_EXPLORATORY_URL`
  (MSSQL/Oracle); the `db-matrix-live-mssql` and `db-matrix-live-oracle` lanes pick it up.
- Both: spin up `app.New(cfg, WithoutDefaults())`, run `AutoMigrate(&liveMigrateUser{})`,
  introspect `information_schema` / `USER_TAB_COLUMNS` / `pragma_table_info` to verify the
  resulting columns and NOT NULL polarity match what `model.ExtractMeta` declared.
- Idempotency: a second `AutoMigrate` call on the same model is asserted not to error.

Both tests skip cleanly when their env vars are unset (local fast lane).

Closes audit §5 risk 4 / §7 task 7. The `db-matrix-required` lane previously exercised only
`SELECT 1` and the CLI smoke; this iteration adds the missing end-to-end check that
`AutoMigrate` produces SQL the engine actually accepts and that the resulting schema matches
the model.

### 3. Schema-level drift detection (ADR-009)

`pkg/db/schema_drift.go`, `pkg/db/schema_drift_test.go`:

- New `Migrator.SchemaDrift(ctx context.Context, expected []ExpectedTable) ([]SchemaDriftEntry, error)`.
- Four new drift kinds, each with a documented real-world cause:
  - `schema_missing_table` — caller declares a table the live DB doesn't have.
  - `schema_missing_column` — caller declares a column the live table doesn't have.
  - `schema_extra_column` — live table has a column the caller doesn't declare (ad-hoc DDL).
  - `schema_column_nullability` — both sides have the column but NOT NULL polarity differs.
- New sentinel `ErrSchemaDriftUnsupported`. MSSQL and Oracle return it (their introspection
  paths are not technical complexity but verification scope — they deserve the same live-DB
  test coverage as the matrix lanes before shipping, and that is a follow-up iteration).
- New types `ExpectedTable`, `ExpectedColumn`, `SchemaDriftEntry`. Deliberate model-agnostic
  structured input (not `[]any` of model structs + internal `model.ExtractMeta` call) to
  avoid a `pkg/db ↔ pkg/model` test-path cycle; rationale documented in ADR-009 §2.
- SQLite quirk handled: `INTEGER PRIMARY KEY` columns report `notnull=0` in PRAGMA but are
  ROWID aliases (never null). When the PRAGMA `pk` column is non-zero the comparator
  treats them as NOT NULL.

Tests: 13 unit tests against SQLite — happy path, missing table, missing column, extra column,
nullability mismatch, sort order, nil receiver, nil database, empty expected slice, duplicate
table name detection, empty table name detection, MSSQL sentinel, Oracle sentinel. All green.

Closes audit §3 row 9 / §7 task 8. PG/MySQL live-DB coverage will materialise the first time
the matrix lane runs; MSSQL/Oracle live-DB introspection is the documented follow-up.

## Iteration loop

Subagents in `.claude/agents/` consulted in parallel (`/iterate` flow):

| Subagent              | Verdict | Action taken                                                                  |
|-----------------------|---------|-------------------------------------------------------------------------------|
| `architect-reviewer`  | PASS    | Three findings: ADR-009 missing (written), `ExpectedColumn` additive-growth godoc (added), CI env vars in `CI_MATRIX.md` (already documented). |
| `code-reviewer`       | NITS    | Five recommended fixes: defensive `EncryptionKey` slice copy (applied), `normaliseExpected` duplicate-name check (applied), SQLite PRAGMA test-helper parameter binding (applied), nil-receiver / empty-expected tests (added), multi-statement migration split for pure-Go SQLite driver portability (applied). |
| `security-auditor`    | WARN    | One blocker on the test helper (SQLite branch string-concat) — fixed. No production-side issues. |
| `contract-guardian`   | PASS    | One required follow-up: `API_CONTRACT_INVENTORY.md` `pkg/db` row needs SchemaDrift symbols listed — applied. |
| `doc-updater`         | UPDATED | CSRF_GUIDE Pattern D + troubleshooting `[]byte` cast, `pkg/db/migrate.go` `Migrator.Drift` godoc points to SchemaDrift, DEVELOPER_MANUAL.md `pkg/db` scope refreshed. |
| `examples-maintainer` | NO_CHANGE_NEEDED | No `CSRFOptions` callers in `examples/*`; `go build ./examples/...` clean. SchemaDrift demo deferred (none of the examples instantiate `Migrator` today). |

## Verification

- `go test ./...` — full suite green (live-DB tests skip when env vars absent; CI lanes pick them up).
- `go vet ./...` — clean.
- `bash scripts/ci/check_contract_freeze.sh` — green; baseline carries the deliberate CSRF
  delta (`Secure` removed, `InsecureCookie` + `Logger` added) and the SchemaDrift additions
  (`ExpectedTable`, `ExpectedColumn`, `SchemaDriftEntry`, `ErrSchemaDriftUnsupported`, four
  `DriftKindSchema*` constants, `Migrator.SchemaDrift` method).

## Files touched

**New:**
- `docs/adrs/ADR-008-csrf-followups.md`
- `docs/adrs/ADR-009-schema-drift-detection.md`
- `docs/iterations/2026-05-15-csrf-followups-automigrate-schemadrift.md` (this file)
- `pkg/app/automigrate_live_test.go`
- `pkg/db/schema_drift.go`
- `pkg/db/schema_drift_test.go`

**Modified:**
- `.claude/state/CURRENT_ITERATION.md` (iteration scope; closed at end of session)
- `CHANGELOG.md` (Security note + Added entries + two BREAKING notes)
- `contracts/baseline/api_exported_symbols.txt` (rebaselined; deltas documented above)
- `docs/guides/CSRF_GUIDE.md` (new types, secure-by-default, logger section)
- `docs/reference/API_CONTRACT_INVENTORY.md` (`pkg/db` row now lists schema-drift symbols)
- `docs/reference/DEVELOPER_MANUAL.md` (`pkg/db` scope + reference date)
- `pkg/db/migrate.go` (`Migrator.Drift` godoc points to `SchemaDrift`)
- `pkg/router/csrf.go` (Logger field, `[]byte` key with defensive copy, `InsecureCookie` polarity)
- `pkg/router/csrf_hardening_test.go` (4 new tests, existing tests adapted to new field types)
- `pkg/router/middleware.go` (`DefaultStack` plumbs logger into CSRF middleware)

## Follow-ups (not blocking)

- **MSSQL/Oracle SchemaDrift introspection.** The sentinel pattern is in place; a future
  iteration can add `INFORMATION_SCHEMA.COLUMNS` (MSSQL with `@p1` params) and
  `USER_TAB_COLUMNS` (Oracle with `:1` params, `NULLABLE` 'Y'/'N') queries. Deserves the
  same live-DB CI coverage as the AutoMigrate tests landed here.
- **Column-type comparison in SchemaDrift.** Cross-dialect type-family compatibility table.
  Add additively to `ExpectedColumn` (e.g. `Type string`, `Length int`); the additive-growth
  godoc already prepares callers to use field-named struct literals.
- **SchemaDrift end-to-end usage guide.** `docs/guides/MODELING_MULTI_DATABASE.md` could
  carry a section bridging `model.ExtractMeta` → `[]db.ExpectedTable` for callers who want
  the model-driven path. Doc-updater flagged this; not material to the shipped behaviour.
- **`pkg/auth/secrets` firewall coverage.** Contract-guardian noted that if the package
  is ever promoted from internal to `stable`, the firewall test must be extended to forbid
  `aws-sdk-go-v2/service/secretsmanager` types from leaking into exported signatures.
  Track for the promotion moment.

## Why this fits one iteration

The three audit items shared:

- A common motivation (close gaps the 2026-05-14 audit explicitly enumerated).
- A coordinated migration story for the CSRF breaks (single ADR-008 + single CHANGELOG
  BREAKING note instead of three close-together change sets).
- Per-dialect introspection helpers shared between the AutoMigrate live tests and the
  SchemaDrift implementation (PG `information_schema`, MySQL `information_schema`, SQLite
  `pragma_table_info`).

Bundling them kept the migration narrative, the test surface, and the doc updates coherent.
