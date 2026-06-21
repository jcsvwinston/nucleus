# CI SQL Matrix Profiles

Reference date: 2026-06-09.
Status: Current.

This document defines Nucleus CI SQL matrix profiles, required vs exploratory lanes, and local reproduction commands.

Manual CI dispatch is available via `workflow_dispatch` for stability drills.

## Profile Status

- `sqlite-smoke` (required): fast default path via `go test ./...`
- `postgresql` (required): runtime + CLI critical-command integration smoke
- `mysql` (required): runtime + CLI critical-command integration smoke
- `mssql` (required): live container runtime + CLI critical-command smoke. Promoted from exploratory on 2026-05-12 after a 10/10 stability drill (`docs/reports/mssql_oracle_stability_report.md`); now a hard dependency of `CI Required Gate`.
- `oracle` (required): live container runtime + CLI critical-command smoke. Promoted from exploratory on 2026-05-12 after a 10/10 stability drill (`docs/reports/mssql_oracle_stability_report.md`); now a hard dependency of `CI Required Gate`.

## Required Merge Policy Check

- Required branch-protection status check context on `main`: `CI Required Gate`
- This check consolidates required CI jobs (`test` + `db-matrix-required` + `db-matrix-live-mssql` + `db-matrix-live-oracle` + `compatibility-harness` + `contract-freeze`) into a single stable context for merge policy. The MSSQL and Oracle live lanes were added to the required gate on 2026-05-12 (see Profile Status above).
- The `test` lane also runs `govulncheck ./...` (Go module). It is blocking: a freshly-published vulnerability advisory can fail `CI Required Gate` on any PR regardless of its diff scope.

**Status: APPLIED to `main` on 2026-05-28.** The protection is live with
`enforce_admins: true`, required status check `CI Required Gate`
(`strict: true`), `required_approving_review_count: 0`, and
`required_conversation_resolution: true`. `main` is therefore **PR-only** —
direct pushes are rejected for everyone (admins included), and every change
must open a PR whose `CI Required Gate` goes green before it can merge. This
closes the long-standing gap where red CI could be pushed directly to `main`
(the cause of `main` being red 2026-05-24 → 2026-05-27).

Apply / re-apply branch protection (requires repo-admin permissions):

```bash
gh auth login
# NOTE: --approvals 0 is deliberate. This is a single-maintainer repo; the
# script default of 1 would lock the sole maintainer out (no second reviewer
# to approve, and enforce_admins blocks direct pushes). With 0 approvals the
# maintainer self-merges PRs once CI Required Gate is green.
bash scripts/ci/configure_branch_protection.sh --repo jcsvwinston/nucleus --branch main --approvals 0
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
docker run --rm --name nucleus-pg \
  -e POSTGRES_DB=nucleus \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 -d postgres:16

export NUCLEUS_SQL_MATRIX_URL='postgres://postgres:postgres@127.0.0.1:5432/nucleus?sslmode=disable'
go test ./pkg/db -run '^TestSQLMatrix_ConnectAndPing$' -v
go test ./internal/cli -run '^TestSQLMatrix_CriticalCommands$' -v
```

## MySQL required profile

```bash
docker run --rm --name nucleus-mysql \
  -e MYSQL_DATABASE=nucleus \
  -e MYSQL_ROOT_PASSWORD=root \
  -p 3306:3306 -d mysql:8.4

export NUCLEUS_SQL_MATRIX_URL='mysql://root:root@127.0.0.1:3306/nucleus'
go test ./pkg/db -run '^TestSQLMatrix_ConnectAndPing$' -v
go test ./internal/cli -run '^TestSQLMatrix_CriticalCommands$' -v
```

## MS SQL Server and Oracle live (required) profiles

> **Note:** Enterprise SQL drivers are behind build tags since v0.5.6.
> You must pass `-tags mssql` or `-tags oracle` when running these tests.
> These lanes are required (blocking) as of 2026-05-12; the underlying
> test functions retain their `Exploratory` names (a cosmetic rename is a
> tracked follow-up in `docs/reports/mssql_oracle_stability_report.md`).

```bash
docker run --rm --name nucleus-mssql \
  -e ACCEPT_EULA=Y \
  -e MSSQL_SA_PASSWORD='StrongPassw0rd!' \
  -p 1433:1433 -d mcr.microsoft.com/mssql/server:2022-latest

export NUCLEUS_SQL_EXPLORATORY_URL='sqlserver://sa:StrongPassw0rd!@127.0.0.1:1433/master'
go test -tags mssql ./pkg/db -run '^TestSQLMatrix_ExploratoryLiveConnectAndPing$' -v
go test -tags mssql ./internal/cli -run '^TestSQLMatrix_ExploratoryCriticalCommands$' -v

docker run --rm --name nucleus-oracle \
  -e ORACLE_PASSWORD='oracle' \
  -p 1521:1521 -d gvenzl/oracle-free:23-slim

export NUCLEUS_SQL_EXPLORATORY_URL='oracle://system:oracle@127.0.0.1:1521/FREEPDB1'
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
  --repo jcsvwinston/nucleus \
  --branch <feature-or-release-branch> \
  --runs 10
```

## Known Gaps (Current)

- `pkg/db` supports SQL URLs for `sqlite://`, `postgres://`/`postgresql://`, `mysql://`, `sqlserver://`/`mssql://`, and `oracle://`.
- **MSSQL and Oracle drivers are now behind build tags** (`-tags mssql`, `-tags oracle`). They are excluded from default builds.
- MS SQL Server and Oracle lanes are live-smoke in CI and are **required** (blocking) as of 2026-05-12.
- CI exploratory lanes must pass `-tags mssql` or `-tags oracle` to run the corresponding tests.
- CLI exploratory critical-command coverage now includes:
  - `createcachetable` idempotency check (engine-specific DDL safety)
  - `sqlflush` and `flush --dry-run` assertions on MSSQL/Oracle SQL generation
  - `sqlsequencereset` assertions for MSSQL and Oracle guidance output
- Oracle `sqlsequencereset` now auto-generates `ALTER SEQUENCE ... RESTART START WITH ...` for common naming patterns (`<table>_SEQ`, `<table>_ID_SEQ`) when an `id` column exists.
- Remaining gap: custom Oracle sequence naming strategies still require manual mapping conventions.

## Non-framework lanes

- `Docs site` (`.github/workflows/docs.yml`) — Docusaurus build for the
  public documentation site under `website/`. Path-scoped to
  `website/**` and `.github/workflows/docs.yml`; runs build-only on PRs
  and build + deploy to GitHub Pages on push to `main`. **Non-blocking**
  to the framework `CI Required Gate` and to release rehearsal — a
  failure here does not gate a Nucleus release. The lane uses Node
  tooling and produces no artefact the framework runtime depends on.

## Promotion Criteria (Exploratory -> Required) — SATISFIED 2026-05-12

The MSSQL and Oracle lanes were promoted from exploratory to required on
2026-05-12 after meeting every criterion below. Kept here as the record of
the bar a future engine lane must clear.

- [x] Runtime adapter support for MS SQL Server and Oracle in `pkg/db`
  (DSN conversion/open path; driver wiring and health checks).
- [x] SQL helper coverage for affected CLI commands
  (flush/fixtures/inspect/cache/session helpers).
- [x] Live smoke stable over time — a 10/10 stability drill on both lanes
  (`docs/reports/mssql_oracle_stability_report.md`), with documented local
  reproduction (above).
- [x] Lanes added to `CI Required Gate.needs` in `.github/workflows/ci.yml`
  with no `continue-on-error`.
