# CI SQL Matrix Profiles

Reference date: 2026-04-05.
Status: Current.

This document defines GoFrame CI SQL matrix profiles, required vs exploratory lanes, and local reproduction commands.

## Profile Status

- `sqlite-smoke` (required): fast default path via `go test ./...`
- `postgresql` (required): runtime + CLI critical-command integration smoke
- `mysql` (required): runtime + CLI critical-command integration smoke
- `mssql` (exploratory, non-blocking): compatibility smoke for current unsupported scheme behavior
- `oracle` (exploratory, non-blocking): compatibility smoke for current unsupported scheme behavior

## Required Merge Policy Check

- Required branch-protection status check context on `main`: `CI Required Gate`
- This check consolidates required CI jobs (`test` + `db-matrix-required`) into a single stable context for merge policy.

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

## PostgreSQL required profile

```bash
docker run --rm --name goframe-pg \
  -e POSTGRES_DB=goframe \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 -d postgres:16

export GOFRAME_SQL_MATRIX_URL='postgres://postgres:postgres@127.0.0.1:5432/goframe?sslmode=disable'
go test ./pkg/db -run '^TestSQLMatrix_BunConnectAndPing$' -v
go test ./internal/cli -run '^TestSQLMatrix_CriticalCommands$' -v
```

## MySQL required profile

```bash
docker run --rm --name goframe-mysql \
  -e MYSQL_DATABASE=goframe \
  -e MYSQL_ROOT_PASSWORD=root \
  -p 3306:3306 -d mysql:8.4

export GOFRAME_SQL_MATRIX_URL='mysql://root:root@127.0.0.1:3306/goframe'
go test ./pkg/db -run '^TestSQLMatrix_BunConnectAndPing$' -v
go test ./internal/cli -run '^TestSQLMatrix_CriticalCommands$' -v
```

## MS SQL Server and Oracle exploratory profiles

```bash
export GOFRAME_SQL_EXPLORATORY_URL='sqlserver://sa:StrongPassw0rd!@127.0.0.1:1433/master'
go test ./pkg/db -run '^TestSQLMatrix_UnsupportedExploratoryURL$' -v

export GOFRAME_SQL_EXPLORATORY_URL='oracle://system:oracle@127.0.0.1:1521/FREEPDB1'
go test ./pkg/db -run '^TestSQLMatrix_UnsupportedExploratoryURL$' -v

go test ./pkg/db -run 'UnsupportedEnterpriseCandidates' -v
```

## Known Gaps (Current)

- `pkg/db` supports SQL URLs for `sqlite://`, `postgres://`/`postgresql://`, and `mysql://`.
- Bun dialect and SQL open path do not support `sqlserver://`/`mssql://` or `oracle://` yet.
- CLI SQL helper paths (`flush`, fixture SQL builders, inspect helpers, cache/session SQL helpers) are implemented for SQLite/PostgreSQL/MySQL only.

## Promotion Criteria (Exploratory -> Required)

- Add runtime adapter support for MS SQL Server and Oracle in `pkg/db`:
  - DSN conversion/open path
  - Bun dialect integration
- Add SQL helper parity for affected CLI commands (flush/fixtures/inspect/cache/session helpers).
- Replace compatibility-only exploratory tests with live connectivity + critical-command integration smoke.
- Promote the lane to required only after stable green results and documented local reproduction.
