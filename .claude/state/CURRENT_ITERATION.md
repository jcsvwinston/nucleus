# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

No active iteration. The 2026-05-15 sweep — CSRF middleware follow-ups
(ADR-008), live-DB AutoMigrate integration tests, and schema-level drift
detection (ADR-009) — is complete and archived at
`docs/iterations/2026-05-15-csrf-followups-automigrate-schemadrift.md`.
Awaiting owner direction for the next iteration.

## Scope

- in: (TBD — owner to confirm from the queue below)
- out: (TBD)

## Acceptance criteria

- [ ] (TBD)

## Status

### Done

- v0.7.0 released (PRs #56–#59); CSRF hardening (ADR-006, PR #60); slog
  secret redaction (ADR-007, PR #62) — see archived iterations.
- **2026-05-15 sweep — three audit follow-ups bundled in one iteration:**
  - **CSRF middleware follow-ups** (ADR-008): logger plumbed via
    `CSRFOptions.Logger *slog.Logger` (defaults to `slog.Default()`;
    `router.DefaultStack` plumbs the router's logger automatically);
    `EncryptionKey` type changed `string` → `[]byte` with a defensive
    slice copy in `defaults()`; `Secure bool` replaced by
    `InsecureCookie bool` (polarity flipped — zero-value is now the
    secure path).
  - **Live-DB AutoMigrate integration tests** in
    `pkg/app/automigrate_live_test.go`:
    `TestSQLMatrix_AutoMigrate` (PG/MySQL required lane) and
    `TestSQLMatrix_AutoMigrate_Exploratory` (MSSQL/Oracle exploratory
    lane). Both call `app.AutoMigrate` then introspect the live
    schema (`information_schema` / `USER_TAB_COLUMNS` /
    `pragma_table_info`) to verify columns and NOT NULL polarity.
  - **Schema-level drift detection** (ADR-009): new
    `Migrator.SchemaDrift(ctx, []ExpectedTable) ([]SchemaDriftEntry, error)`
    method, four new drift kinds (`schema_missing_table`,
    `schema_missing_column`, `schema_extra_column`,
    `schema_column_nullability`), new sentinel
    `ErrSchemaDriftUnsupported`. SQLite/PG/MySQL supported; MSSQL and
    Oracle return the sentinel pending live-DB-verified introspection
    in a future iteration.
- Iteration loop run end to end:
  - `architect-reviewer`: PASS (3 findings actioned — ADR-009 written,
    `ExpectedColumn` additive-growth godoc added, CI env vars
    confirmed already in `CI_MATRIX.md`).
  - `code-reviewer`: NITS (5 recommended fixes all applied —
    defensive `EncryptionKey` slice copy, `normaliseExpected`
    duplicate-name check, SQLite PRAGMA test-helper parameter
    binding, nil-receiver / empty-expected tests, multi-statement
    migration split for pure-Go SQLite driver portability).
  - `security-auditor`: WARN on a test-only string-concat (fixed); no
    production-side issues.
  - `contract-guardian`: PASS; `API_CONTRACT_INVENTORY.md` `pkg/db`
    row refreshed with the new SchemaDrift symbols.
  - `doc-updater`: UPDATED CSRF_GUIDE Pattern D, `pkg/db/migrate.go`
    `Migrator.Drift` godoc, `DEVELOPER_MANUAL.md` `pkg/db` scope.
  - `examples-maintainer`: NO_CHANGE_NEEDED (no `CSRFOptions`
    callers in `examples/*`; SchemaDrift demo deferred since no
    example currently instantiates `Migrator`).
- `go test ./...`, `go vet ./...`, contract freeze — all green.

### In progress

- (none)

### Blocked

- (none)

## Candidate next steps (priority order, pending owner confirmation)

1. **MSSQL/Oracle SchemaDrift introspection.** Sentinel pattern is in
   place; complete `INFORMATION_SCHEMA.COLUMNS` (MSSQL with `@p1`
   params) and `USER_TAB_COLUMNS` (Oracle with `:1` params,
   `NULLABLE` 'Y'/'N') queries. Should land with the same live-DB CI
   coverage as the AutoMigrate live tests this iteration shipped.
2. **Column-type comparison in SchemaDrift.** Cross-dialect
   type-family compatibility table. Add additively to
   `ExpectedColumn` (the additive-growth godoc already documents the
   forward-compat intent).
3. **SchemaDrift end-to-end usage guide.** Bridge `model.ExtractMeta`
   → `[]db.ExpectedTable` documented in
   `docs/guides/MODELING_MULTI_DATABASE.md`. Doc-updater flagged this
   as not material to shipped behaviour.
4. **`pkg/storage` in the contract baseline.** Audit §3 row item from
   2026-05-14 that did not land this round.
5. **`go mod tidy` unblock** — fix the `admin/proto` replace-directive
   issue so the AWS SDK modules carry correct `// direct` annotations.
6. **Phase 4 — AWS SDK opt-in** — build tag / plugin so `pkg/app` does
   not link the AWS SDK unconditionally (~3-5 MB).
7. **`tasks.Manager` struct→interface DEP** — optional DEP-2026-004
   for the binary-incompatible type-identity change
   (contract-guardian advisory from the v0.7.0 release prep).
8. **503 path test for `/healthz`**, endpoints-parity doc-parsing,
   `pkg/health/{db,redis,storage}.go` individual tests — smaller
   audit §7 items.

## Files of interest

- `docs/iterations/2026-05-15-csrf-followups-automigrate-schemadrift.md`
  — archived sweep iteration.
- `docs/adrs/ADR-008-csrf-followups.md` — CSRF logger / `[]byte` key /
  `InsecureCookie` polarity flip rationale.
- `docs/adrs/ADR-009-schema-drift-detection.md` — `SchemaDrift` API
  design (model-agnostic `[]ExpectedTable` input, drift-kind taxonomy,
  MSSQL/Oracle sentinel rationale).
- `pkg/db/schema_drift.go` — the new schema-drift implementation;
  follow-up #1 extends it to MSSQL/Oracle.
- `pkg/app/automigrate_live_test.go` — the live-DB AutoMigrate
  introspection harness; the pattern follow-up #1 can re-use.

## Notes / decisions log

- 2026-05-15 — three audit follow-ups bundled into one iteration. ADRs
  numbered 008 (CSRF) and 009 (SchemaDrift). Two pre-`v1.0` BREAKING
  changes recorded in CHANGELOG (`CSRFOptions.EncryptionKey` type and
  `CSRFOptions.Secure` → `InsecureCookie` polarity flip). One
  required contract-guardian follow-up applied
  (`API_CONTRACT_INVENTORY.md`); one architect follow-up applied
  (ADR-009 written). Bundling kept the migration story, the test
  surface, and the doc updates coherent — three close-together
  CSRF iterations across separate PRs would have produced three
  separate `BREAKING` notes.
