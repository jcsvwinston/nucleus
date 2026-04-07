# Critical Dependency Review

Reference date: 2026-04-07.
Branch: `codex/v0.6.0-roadmap`.
Input artifact: `dist/reports/dependency_impact_report.md` (generated at `2026-04-07T20:10:44Z`, commit `5aa6a16`).
Status: Completed review for RC decision.

## Scope

Critical dependency changes flagged by the report (`4`):

- `modernc.org/sqlite` (added)
- `github.com/redis/go-redis/v9` (added)
- `github.com/microsoft/go-mssqldb` (added)
- `github.com/sijms/go-ora/v2` (added)

## Review Matrix

| Dependency | Change | Runtime Surface | Evidence | Risk Assessment | Decision |
| --- | --- | --- | --- | --- | --- |
| `modernc.org/sqlite` | added | default/dev SQL path (`sqlite://`) and test runtime | `go test ./...` green; compatibility report `READY` | medium: SQL-driver behavior differences vs previous stack | accepted for RC |
| `github.com/redis/go-redis/v9` | added | optional session store (`session_store=redis`) | deploy checks + tests green; default remains `memory` | medium: external redis topology/timeout misconfiguration risk | accepted for RC |
| `github.com/microsoft/go-mssqldb` | added | exploratory SQL support (`sqlserver://`, non-blocking lane) | exploratory stability report `10/10` MSSQL success | medium: enterprise-driver edge cases remain possible | accepted as exploratory |
| `github.com/sijms/go-ora/v2` | added | exploratory SQL support (`oracle://`, non-blocking lane) | exploratory stability report `10/10` Oracle success | medium-high: Oracle schema/sequence conventions vary by deployment | accepted as exploratory |

## Compatibility Guardrails Confirmed

- `go test ./...` passed.
- `bash scripts/ci/check_contract_freeze.sh` passed.
- `bash scripts/ci/run_compatibility_harness.sh --enforce-threshold` passed (`3/3`, `100%`).
- `bash scripts/release/rehearse_rc.sh` completed successfully.

## Release Decision for This Review

- Dependency-impact script output is expected to remain `CRITICAL REVIEW REQUIRED` when critical modules change.
- After manual review, current decision is:
  - `READY FOR RC` with exploratory SQL lanes remaining non-blocking by policy.

## Follow-up Constraints

1. Keep MSSQL/Oracle marked exploratory until promotion criteria in `docs/CI_MATRIX.md` are met.
2. Keep dependency delta explicitly called out in release notes/changelog.
3. Continue 10-run exploratory stability drills before each RC/tag window.
