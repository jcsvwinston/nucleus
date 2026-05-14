# MSSQL and Oracle Stability Report

Reference date: 2026-05-12.
Status: **Promoted to `required` — Track D: Enterprise Data Coverage gate met.**

## Overview

This document summarizes the stability state of MSSQL and Oracle database engines in Nucleus, and records the first sustained stability drill that closes the promotion gate.

## Latest Drill (2026-05-12)

First end-to-end exploratory stability drill against `main` after the four blocking fixes landed in [#30](https://github.com/jcsvwinston/nucleus/pull/30) — build tags, migrator dialect, cache-table-name in the test, and case/empty-state asserts.

| Metric | Result | Threshold |
|---|---|---|
| MSSQL exploratory success | **10/10 (100%)** | >= 80% |
| Oracle exploratory success | **10/10 (100%)** | >= 80% |
| Decision | **READY** | — |

Per-run breakdown: `docs/reports/mssql_oracle_stability_2026-05-12.md`.

A prior drill on 2026-05-12 against `main@58a9be9` (pre-fix) recorded `0/10 / 0/10 — NOT READY`; that report is not archived in the repo as the underlying CI configuration is no longer reachable from `main`.

## Current Status

### CI Integration

**Jobs in `.github/workflows/ci.yml`:**
- `db-matrix-live-mssql` - MSSQL live connectivity + critical commands (required, gated). Renamed from `db-matrix-exploratory-mssql` once promotion landed.
- `db-matrix-live-oracle` - Oracle live connectivity + critical commands (required, gated). Renamed from `db-matrix-exploratory-oracle` once promotion landed.

**Stability Drills:**
- `scripts/ci/run_exploratory_stability.sh` - Executes multiple CI workflow_dispatch runs and summarizes MSSQL/Oracle stability
- Default promotion thresholds: MSSQL >= 80%, Oracle >= 80%
- Supports custom thresholds via `--min-rate-mssql` and `--min-rate-oracle`

### Test Coverage

**Database Layer (`pkg/db/`):**
- `TestSQLMatrix_ExploratoryURLCompatibility` - URL scheme recognition
- `TestSQLMatrix_ExploratoryLiveConnectAndPing` - Live connectivity with retry logic
- `TestOpenSQLDB_MSSQLAndOracleSchemes` - Build-tagged tests (`//go:build mssql && oracle`)
- `TestOpenSQLDB_EnterpriseCandidatesSupported` - URL scheme validation

**CLI Layer (`internal/cli/`):**
- `TestSQLMatrix_ExploratoryCriticalCommands` - Critical command smoke tests:
  - `health` - Database health check
  - `createcachetable` - Cache table provisioning with idempotency
  - `inspectdb` - Schema introspection
  - `sqlflush` - Flush SQL preview
  - `flush --dry-run` - Destructive operation guardrails
  - `sqlsequencereset` - Sequence reset (with Oracle-specific fixture tests)
  - `shell` - SQL query execution

### Supported Schemes

**MSSQL:**
- `sqlserver://sa:password@host:1433/database`
- `mssql://sa:password@host:1433/database`

**Oracle:**
- `oracle://system:oracle@host:1521/FREEPDB1`

### Driver Configuration

**MSSQL:**
- Driver: `github.com/microsoft/go-mssqldb` v1.8.2
- Build tag: `//go:build mssql`
- File: `pkg/db/driver_mssql.go`

**Oracle:**
- Driver: `github.com/sijms/go-ora/v2` v2.9.0
- Build tag: `//go:build oracle`
- File: `pkg/db/driver_oracle.go`

## Stability Assessment

### Current Classification: **Required** (promoted 2026-05-12)

Both MSSQL and Oracle are now `required` per the Enterprise Long-Term Roadmap.

**Required characteristics:**
- CI jobs no longer have `continue-on-error: true`
- Listed in `ci-required-gate` `needs:` so a failure blocks merge
- Job names retain the `Exploratory` label inside `ci.yml` (purely cosmetic; promotion is via gating, not renaming — a follow-up rename PR can drop the label)

### Promotion History

The drill on 2026-05-12 satisfied the three roadmap criteria for `exploratory → required`:

1. **Reproducible local setup** - Docker images available (✅ MSSQL 2022, Oracle Free 23-slim).
2. **Sustained stability drill above threshold** - `bash scripts/ci/run_exploratory_stability.sh --runs 10 --enforce-threshold` returned 100%/100% (threshold 80%/80%).
3. **No unresolved critical regressions** - All critical commands pass across both engines on all 10 runs.

### Known Limitations

**MSSQL:**
- Sequence reset uses `DBCC CHECKIDENT` (identity columns only)
- Table quoting uses `[brackets]` syntax
- Requires explicit schema handling (default: `dbo`)

**Oracle:**
- Sequence reset requires explicit sequence naming convention
- Table names are uppercased by default
- Query syntax requires `FROM dual` for scalar selects
- Connection timeout may be longer than other engines

## Critical Command Coverage

### Currently Tested (✅)
- health
- createcachetable (with idempotency)
- inspectdb
- sqlflush
- flush --dry-run
- sqlsequencereset
- shell

### Missing Coverage (❌)
- remove_stale_contenttypes

## Recent Updates (2026-05-12)

**Fixes that unblocked promotion (shipped in [#30](https://github.com/jcsvwinston/nucleus/pull/30)):**

- `fix(ci): enable mssql/oracle build tags on exploratory live jobs` — `go test` now runs with `-tags mssql` / `-tags oracle` so the enterprise driver `init()` registrations are linked in.
- `fix(db/migrate): dialect-aware migrations-table DDL for mssql/oracle` — `ensureMigrationsTable` now emits `IF OBJECT_ID ... CREATE TABLE` (MSSQL) and an anonymous PL/SQL block that swallows ORA-00955 (Oracle), with `NVARCHAR` / `VARCHAR2` columns.
- `fix(test): use distinct cache table in exploratory critical-commands` — `createcachetable` is now called on its own table name, not on the migration target whose `(id, name)` schema lacks `expires_at`.
- `fix(test): relax exploratory asserts for clearsessions and dumpdata` — accept `nothing to clear` when the sessions table was not pre-seeded; tolerate Oracle's upper-cased column names in dumpdata JSON.

**Test Coverage Status (after fixes):**
- ✅ health
- ✅ migrate (up, down, status)
- ✅ createcachetable (with idempotency)
- ✅ inspectdb
- ✅ sqlflush
- ✅ flush --dry-run
- ✅ sqlsequencereset
- ✅ shell
- ✅ loaddata
- ✅ dumpdata
- ✅ clearsessions
- ❌ remove_stale_contenttypes (not applicable to MSSQL/Oracle without Django-style contenttypes)

## Follow-ups (post-promotion)

1. ~~**Cosmetic rename**: `db-matrix-exploratory-{mssql,oracle}` jobs in `ci.yml` retain the word *Exploratory* in their `name:`.~~ **Done** — renamed to `db-matrix-live-{mssql,oracle}` in the hygiene-sweep PR; `scripts/ci/run_exploratory_stability.sh` queries the new display names. The script's filename keeps the word "exploratory" by convention; the data it produces no longer does.
2. **Watch for flake**: the first drill returned 100%/100% on the post-fix tree, but it is a single drill over ~30 minutes. If transient docker-pull or container-startup issues surface, the right response is to add retries inside the jobs rather than re-downgrade the gate.
3. **`remove_stale_contenttypes`** still has no MSSQL/Oracle coverage; consider whether the command is meaningful for these engines or should be skipped explicitly.

## Post-ADR-004-sprint drill (completed, 2026-05-14)

The ADR-004 integration sprint (PRs #51–#54, merged 2026-05-13) touched `pkg/auth`, `pkg/authz`, `pkg/mail`, `pkg/storage`, and `pkg/app`. None of these directly touch the MSSQL/Oracle driver paths (`pkg/db/driver_{mssql,oracle}.go`) or the migrator dialect plumbing. However, the default-deny middleware now mounted on `Router.Use` flows through every request, including the `/health` paths exercised by the live DB jobs — so a confirmation drill was warranted.

| Metric | Result | Threshold |
|---|---|---|
| MSSQL live success | **10/10 (100%)** | >= 80% |
| Oracle live success | **10/10 (100%)** | >= 80% |
| Decision | **READY** | — |

Run against `main@fce7c57` (the v0.7.0 candidate, after PR #56 merged). Per-run breakdown: `docs/reports/mssql_oracle_stability_2026-05-14.md`. The drill was dispatched with:

```
bash scripts/ci/run_exploratory_stability.sh \
  --runs 10 \
  --min-rate-mssql 80 \
  --min-rate-oracle 80 \
  --enforce-threshold \
  --output docs/reports/mssql_oracle_stability_2026-05-14.md
```

**Result:** no regression from the ADR-004 sprint. The default-deny middleware does not interfere with the internal probe routes the live DB jobs exercise, and the circuit breaker does not trip on the CLI test stubs. MSSQL and Oracle remain `required` with sustained-stability evidence on the post-sprint tree.
