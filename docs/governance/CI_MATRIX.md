# CI SQL Matrix Profiles

Reference date: 2026-04-23.
Status: Current.

This document defines GoFrame CI SQL matrix profiles, required vs exploratory lanes, and local reproduction commands.

Manual CI dispatch is available via `workflow_dispatch` for stability drills.

## Profile Status

- `sqlite-smoke` (required): fast default path via `go test ./...`
- `postgresql` (required): runtime + CLI critical-command integration smoke
- `mysql` (required): runtime + CLI critical-command integration smoke
- `mssql` (exploratory, non-blocking): live container runtime + CLI exploratory critical-command smoke
- `oracle` (exploratory, non-blocking): live container runtime + CLI exploratory critical-command smoke

## Required Merge Policy Check

- Required branch-protection status check context on `main`: `CI Required Gate`
- This check consolidates required CI jobs (`test` + `db-matrix-required` + `compatibility-harness` + `contract-freeze`) into a single stable context for merge policy.

Apply branch protection (requires repo-admin permissions):

```bash
gh auth login
bash scripts/ci/configure_branch_protection.sh --repo jcsvwinston/GoFrame --branch main
```

Preview payload without applying:

```bash
bash scripts/ci/configure_branch_protection.sh --dry-run
```

## Local Reproduction

## SQLite fast path

```bash
go test ./...
```

## Compatibility fixture harness (required)

```bash
bash scripts/ci/run_compatibility_harness.sh --enforce-threshold
```

## Stable contract freeze (required)

```bash
bash scripts/ci/check_contract_freeze.sh
```

Validated baselines:
- CLI primary command names
- CLI JSON command-status envelope/data keys (automation paths)
- Config key patterns
- Exported symbols from stable API packages

## PostgreSQL required profile

```bash
docker run --rm --name goframe-pg \
  -e POSTGRES_DB=goframe \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 -d postgres:16

export GOFRAME_SQL_MATRIX_URL='postgres://postgres:postgres@127.0.0.1:5432/goframe?sslmode=disable'
go test ./pkg/db -run '^TestSQLMatrix_ConnectAndPing$' -v
go test ./internal/cli -run '^TestSQLMatrix_CriticalCommands$' -v
```

## MySQL required profile

```bash
docker run --rm --name goframe-mysql \
  -e MYSQL_DATABASE=goframe \
  -e MYSQL_ROOT_PASSWORD=root \
  -p 3306:3306 -d mysql:8.4

export GOFRAME_SQL_MATRIX_URL='mysql://root:root@127.0.0.1:3306/goframe'
go test ./pkg/db -run '^TestSQLMatrix_ConnectAndPing$' -v
go test ./internal/cli -run '^TestSQLMatrix_CriticalCommands$' -v
```

## MS SQL Server and Oracle exploratory profiles

> **Note:** Enterprise SQL drivers are behind build tags since v0.5.6.
> You must pass `-tags mssql` or `-tags oracle` when running these tests.

```bash
docker run --rm --name goframe-mssql \
  -e ACCEPT_EULA=Y \
  -e MSSQL_SA_PASSWORD='StrongPassw0rd!' \
  -p 1433:1433 -d mcr.microsoft.com/mssql/server:2022-latest

export GOFRAME_SQL_EXPLORATORY_URL='sqlserver://sa:StrongPassw0rd!@127.0.0.1:1433/master'
go test -tags mssql ./pkg/db -run '^TestSQLMatrix_ExploratoryLiveConnectAndPing$' -v
go test -tags mssql ./internal/cli -run '^TestSQLMatrix_ExploratoryCriticalCommands$' -v

docker run --rm --name goframe-oracle \
  -e ORACLE_PASSWORD='oracle' \
  -p 1521:1521 -d gvenzl/oracle-free:23-slim

export GOFRAME_SQL_EXPLORATORY_URL='oracle://system:oracle@127.0.0.1:1521/FREEPDB1'
go test -tags oracle ./pkg/db -run '^TestSQLMatrix_ExploratoryLiveConnectAndPing$' -v
go test -tags oracle ./internal/cli -run '^TestSQLMatrix_ExploratoryCriticalCommands$' -v
```

## Repeated Stability Drill (GitHub Actions)

Trigger and analyze repeated exploratory lanes from your current branch:

```bash
gh auth login
bash scripts/ci/run_exploratory_stability.sh --runs 10 --output docs/reports/exploratory_stability.md
```

Optional explicit repo/branch:

```bash
bash scripts/ci/run_exploratory_stability.sh \
  --repo jcsvwinston/GoFrame \
  --branch <feature-or-release-branch> \
  --runs 10
```

## Known Gaps (Current)

- `pkg/db` supports SQL URLs for `sqlite://`, `postgres://`/`postgresql://`, `mysql://`, `sqlserver://`/`mssql://`, and `oracle://`.
- **MSSQL and Oracle drivers are now behind build tags** (`-tags mssql`, `-tags oracle`). They are excluded from default builds.
- MS SQL Server and Oracle lanes are live-smoke in CI but remain non-blocking.
- CI exploratory lanes must pass `-tags mssql` or `-tags oracle` to run the corresponding tests.
- CLI exploratory critical-command coverage now includes:
  - `createcachetable` idempotency check (engine-specific DDL safety)
  - `sqlflush` and `flush --dry-run` assertions on MSSQL/Oracle SQL generation
  - `sqlsequencereset` assertions for MSSQL and Oracle guidance output
- Oracle `sqlsequencereset` now auto-generates `ALTER SEQUENCE ... RESTART START WITH ...` for common naming patterns (`<table>_SEQ`, `<table>_ID_SEQ`) when an `id` column exists.
- Remaining gap: custom Oracle sequence naming strategies still require manual mapping conventions.

## Promotion Criteria (Exploratory -> Required)

- Add runtime adapter support for MS SQL Server and Oracle in `pkg/db`:
  - DSN conversion/open path
  - driver wiring and health checks
- Complete SQL helper coverage for affected CLI commands (flush/fixtures/inspect/cache/session helpers).
- Keep live exploratory smoke stable over time (low flaky rate, reproducible local setup).
- Promote the lane to required only after stable green results and documented local reproduction.
