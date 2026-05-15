# Iteration — 2026-05-15 — MSSQL/Oracle SchemaDrift introspection

**Owner direction (start-of-session):** Top candidate from the post-PR-#63
HANDOFF queue — close the `ErrSchemaDriftUnsupported` sentinel paths for
MSSQL and Oracle that ADR-009 explicitly deferred. Self-contained;
follows the live-DB-test pattern established by `pkg/app/automigrate_live_test.go`.

**Status:** DONE — landed via PR #66 (squash-merge `6a9aa00`). All four
matrix CI lanes green: required PG + MySQL, exploratory MSSQL + Oracle.
The SchemaDrift live test exercised all five drift kinds against real
PG/MySQL/MSSQL/Oracle containers on the first execution.

## What shipped

### `pkg/db/schema_drift.go` — MSSQL + Oracle introspection

- The `switch system` block in `SchemaDrift` folds `mssql` and `oracle`
  into the supported set; `ErrSchemaDriftUnsupported` is retained but
  narrowed to fire only for engines outside the supported set.
- `introspectTableColumns` gains two new branches:
  - **MSSQL** — `INFORMATION_SCHEMA.COLUMNS` filtered by `SCHEMA_NAME()`
    with `@p1`-style bound parameters (go-mssqldb convention).
  - **Oracle** — `USER_TAB_COLUMNS` with `:1`/`:2`-style bound parameters.
    Two-bind lookup (`TABLE_NAME = :1 OR TABLE_NAME = :2`) covers both
    the double-quoted AutoMigrate scaffold writes (lower-snake-case
    stored) and hand-rolled unquoted DDL (Oracle UPPER-folded storage).
    `NULLABLE = 'Y'` instead of `'YES'` per Oracle catalog convention.

### Tests

- `pkg/db/schema_drift_test.go`: removed the `_For{MSSQL,Oracle}`
  sentinel tests (no longer valid — those engines now succeed);
  replaced with a single `_ForUnknownSystem` test that forges a `*DB`
  with an unrecognised `system` label.
- `pkg/db/schema_drift_live_test.go` (new, 309 LOC):
  - `TestSQLMatrix_SchemaDrift` (PG/MySQL via `NUCLEUS_SQL_MATRIX_URL`)
  - `TestSQLMatrix_SchemaDrift_Exploratory` (MSSQL/Oracle via
    `NUCLEUS_SQL_EXPLORATORY_URL`)
  - Each provisions a fixture table via raw dialect DDL (independent of
    `AutoMigrate` — keeps the test focused on SchemaDrift) and runs all
    four drift kinds in subtests: `matches`, `missing_column`,
    `extra_column`, `nullability_mismatch`, `missing_table`.

### CI workflow

`.github/workflows/ci.yml` updated so the new live tests are actually
exercised by CI. While there, the **required-lane** `TestSQLMatrix_AutoMigrate`
(PG/MySQL) test added in PR #63 is also added to the workflow — it had
been compiling but never executing because the existing workflow
`-run` regex did not pick it up. The **exploratory-lane**
`TestSQLMatrix_AutoMigrate_Exploratory` (MSSQL/Oracle) was attempted
and reverted in commit `cb06837` after surfacing a separate pre-existing
bug (see below).

### Docs

- `docs/adrs/ADR-009-schema-drift-detection.md` — Addendum dated
  2026-05-15 covering what changed, the Oracle dual-binding rationale,
  and the updated compliance row.
- `CHANGELOG.md`, `docs/reference/API_CONTRACT_INVENTORY.md` — existing
  SchemaDrift entries updated to reflect the new state.

## Iteration loop

Four subagents were dispatched in parallel after the implementation but
before commit — `architect-reviewer`, `code-reviewer`, `security-auditor`,
`contract-guardian`. Their full reports were not preserved across a
session interruption, but the local validation gate (`go test ./...`,
`go vet ./...`, contract freeze) was green at commit time and CI
exercised the live tests against all four engines successfully on the
second run (the first run failed for an unrelated reason; see below).

## Discovered during this iteration (out-of-scope, archived as follow-up)

**Admin bootstrap users-table DDL is not dialect-aware on MSSQL/Oracle.**
When `TestSQLMatrix_AutoMigrate_Exploratory` finally ran for the first
time (after the workflow `-run` regex fix), it failed with:

- MSSQL: `Incorrect syntax near 'nucleus_admin_users'`
- Oracle: `ORA-03076: unexpected item DEFAULT in a column definition`

Both come from `app.New → admin bootstrap → ensure users table`, even
when `app.New(cfg, WithoutDefaults())` is used (the admin bootstrap is
gated by `AdminBootstrapEmail`, not by `WithoutDefaults`).

The bug is unrelated to SchemaDrift — same class as the dialect-aware
DDL discipline we already applied to AutoMigrate scaffolds (see PR #54
/ ADR-004). Fix scope: replicate that discipline in `pkg/admin`'s
bootstrap users-table DDL.

**Path forward:** the exploratory-lane wiring stays out of CI until a
dedicated iteration fixes the admin-bootstrap DDL. The NOTE comments in
`ci.yml` and the ADR-009 addendum carry the breadcrumb so the next
iteration owner picks it up.

## Verification

- `go test ./...` (local) — green
- `go vet ./...` — clean
- `bash scripts/ci/check_contract_freeze.sh` — green
- CI on PR #66 final: 9/9 green (Test And Smoke, Compatibility Harness,
  Contract Freeze, Admin Observability Skeleton, DB Matrix Required PG,
  DB Matrix Required MySQL, DB Matrix Live MSSQL, DB Matrix Live
  Oracle, CI Required Gate)

## Files touched (post squash to `4f48d3f → 6a9aa00` on `main`)

```
.github/workflows/ci.yml                          | +6/-1
CHANGELOG.md                                      | +2/-1
docs/adrs/ADR-009-schema-drift-detection.md       | +21/0
docs/reference/API_CONTRACT_INVENTORY.md          | +1/-1
pkg/db/schema_drift.go                            | +60/-7
pkg/db/schema_drift_test.go                       | +5/-20
pkg/db/schema_drift_live_test.go                  | +309/0 (new)
```

Plus the related PR #65 (inventory archive, doc-only, merged the same
day): `docs/iterations/2026-05-15-pkg-app-nucleus-inventory.md`.

## Why this fits one iteration

The scope was bounded by ADR-009's explicit deferral: "the introspection
queries for those engines are not difficult — MSSQL uses `INFORMATION_SCHEMA.COLUMNS`
with `@p1`-bound parameters; Oracle uses `USER_TAB_COLUMNS` with `:1`-bound
parameters and `NULLABLE` returning 'Y'/'N' rather than 'YES'/'NO'. The
reason they are deferred is not technical complexity, it is verification
scope" (ADR-009 §4). The verification scope closed cleanly here: the
live tests pass on the first execution against real Oracle and MSSQL
containers in CI.

The one surprise — the admin bootstrap DDL bug — was a pre-existing
latent issue that surfaced because of the CI workflow fix. Scope
discipline kept it out of this iteration and into its own.

## Follow-ups for the next iteration

1. **`pkg/admin` bootstrap users-table DDL — dialect-aware fix for
   MSSQL/Oracle.** Specific failure modes documented above. Replicates
   the discipline already applied to AutoMigrate scaffolds (per
   `pkg/model/migration_scaffold_{mssql,oracle}.go`). After the fix,
   re-wire `TestSQLMatrix_AutoMigrate_Exploratory` into the CI workflow.

2. **Column-type comparison in SchemaDrift.** The four drift kinds in
   place today cover existence and nullability. Cross-dialect type-family
   compatibility (BIGINT vs INT vs BIGSERIAL vs NUMBER vs NVARCHAR vs
   VARCHAR vs TEXT) is its own rabbit hole; deferred deliberately.
   `ExpectedColumn` is already documented as additive-growth-safe.

3. **SchemaDrift end-to-end usage guide.** Bridge `model.ExtractMeta`
   → `[]db.ExpectedTable` documented in `docs/guides/MODELING_MULTI_DATABASE.md`
   so callers don't reconstruct the bridge each time.
