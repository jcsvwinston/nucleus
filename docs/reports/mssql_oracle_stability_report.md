# MSSQL and Oracle Stability Report

Reference date: 2026-05-08.
Status: Current baseline for Track D: Enterprise Data Coverage.

## Overview

This document summarizes the current stability state of MSSQL and Oracle database engines in GoFrame.

## Current Status

### CI Integration

**Jobs in `.github/workflows/ci.yml`:**
- `db-matrix-exploratory-mssql` - MSSQL live connectivity tests (continue-on-error: true)
- `db-matrix-exploratory-oracle` - Oracle live connectivity tests (continue-on-error: true)

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

### Current Classification: Exploratory

Both MSSQL and Oracle are currently in "exploratory" status per the Enterprise Long-Term Roadmap.

**Exploratory characteristics:**
- CI jobs have `continue-on-error: true`
- Not part of required CI gate
- Promotion requires meeting stability thresholds

### Promotion Requirements (from Roadmap)

To promote from exploratory → required:
1. **Reproducible local setup** - Docker images available (✅ MSSQL 2022, Oracle Free 23-slim)
2. **Sustained stability drills above threshold** - Run `scripts/ci/run_exploratory_stability.sh --runs 10 --enforce-threshold`
3. **No unresolved critical regressions** - All critical commands must pass consistently

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

## Recent Updates (2026-05-08)

**Added Critical Command Coverage:**
- migrate up/down/status - Added to exploratory tests
- dumpdata/loaddata - Added to exploratory tests
- clearsessions --all - Added to exploratory tests

**Test Coverage Status:**
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

**Stability Drill Status:**
- Stability drill script exists: `scripts/ci/run_exploratory_stability.sh`
- Default thresholds: MSSQL >= 80%, Oracle >= 80%
- Ready to execute when GitHub CLI is authenticated
- Command: `bash scripts/ci/run_exploratory_stability.sh --runs 10 --enforce-threshold`

## Next Steps

### To Complete Track D Task #15

1. **Execute stability drills:**
   ```bash
   gh auth login
   bash scripts/ci/run_exploratory_stability.sh --runs 10 --enforce-threshold
   ```

2. **Document results:**
   - Update this report with stability drill outcomes
   - If thresholds met, update roadmap to promote to required

3. **Update CI configuration:**
   - If promoted, remove `continue-on-error: true`
   - Add exploratory jobs to required CI gate
