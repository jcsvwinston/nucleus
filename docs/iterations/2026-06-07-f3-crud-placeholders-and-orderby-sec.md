# Iteration: F-3 CRUD Placeholder Portability + SEC OrderBy Injection Fix

> Archived: 2026-06-07
> PRs: #93 (audit seed land), #94 (F-3), #95 (SEC)
> HEAD at close: d0d041f
> Branch at close: main (all PRs squash-merged)

---

## Goal

Fix F-3 from `docs/audits/2026-06-07-exhaustive-audit-v2.md`: make the model
CRUD layer placeholder-portable across all five SQL engines, add live
PostgreSQL CRUD coverage so the SQLite-only blind spot cannot recur, and
close the SQL injection finding surfaced by the security-auditor during the
F-3 iteration loop.

---

## Acceptance criteria — all met

- [x] All CRUD-emitted SQL uses engine-correct placeholders (unit-verifiable
      via a rebind helper test table covering the five dialects).
      DONE: `rebind` / `rebindNumbered` in `pkg/model/crud.go`; covers
      postgres (`$N`), sqlserver/mssql (`@pN`), oracle (`:N`), mysql/sqlite
      (pass-through). Hooked into `execContext`, `queryContext`, and all five
      `getEstimate` count-query branches. `SetDialect` normalises the two
      naming conventions in the codebase ("postgresql"→"postgres",
      "sqlserver"→"mssql").

- [x] A live-PG CRUD test exists, fails on pre-fix code ("syntax error at or
      near \"?\"" class), passes post-fix, and runs in the postgresql CI lane
      (never mock the DB — CLAUDE.md §7).
      DONE: `TestCRUDLive_PlaceholderPortability` runs real INSERT/SELECT/
      UPDATE/DELETE against a live PostgreSQL container; added
      `db-matrix-required` CI step so it runs (not skipped) in the postgresql
      and mysql lanes; confirmed RUN+PASS in CI post-fix.

- [x] SQLite + MySQL behaviour unchanged (`go test ./...` green; db-matrix
      lanes green).
      DONE: dialect rebind is a no-op for mysql/sqlite; all existing tests
      pass; db-matrix lanes green.

- [x] No frozen symbol added/removed without contract-guardian sign-off; if a
      helper lands in `pkg/db`, run the freeze flow deliberately.
      DONE: `rebind` / `rebindNumbered` / `sanitizeOrderBy` are unexported.
      No exported surface change. Contract-guardian: PASS (no freeze regen,
      no deprecation path required). Helper kept in `pkg/model/crud.go`
      (not promoted to `pkg/db`) to avoid a freeze-surface change.

- [x] CHANGELOG.md Unreleased entry (bug fix, patch impact) via
      changelog-writer; guides untouched by this fix stay untouched.
      DONE: CHANGELOG patch entry under `### Fixed` for F-3 and under
      `### Security` for SEC (PR #95).

- [x] Full Iteration Loop (§4) run; security-auditor pass included (SQL
      paths touched).
      DONE: architect-reviewer PASS; code-reviewer WARN/NITS addressed;
      security-auditor found [HIGH] OrderBy injection (SEC follow-up PR #95,
      resolved); contract-guardian PASS; test-runner PASS; CHANGELOG written.
      Security-auditor re-ran on SEC PR and returned PASS.

---

## What was built (summary by PR)

### PR #93 — docs(audit): land audit-v2 report + seed F-3 (82fdae0)

- Landed `docs/audits/2026-06-07-exhaustive-audit-v2.md` (the executed-lane
  report from the prior session's audit branch).
- Seeded `CURRENT_ITERATION.md` with the F-3 goal.
- Cleaned working tree: discarded `go.sum` / `admin/*/go.mod` local churn
  (per the prior handoff instruction). Superseded draft
  `docs/audits/2026-06-07-exhaustive-audit.md` left untracked (harmless;
  maintainer may delete at any time).

### PR #94 — fix(model): CRUD placeholder portability across SQL engines (15f18ba)

Files changed:
- `pkg/model/crud.go` — `rebind`, `rebindNumbered`, hooked in `execContext` /
  `queryContext` / `getEstimate` branches; `SetDialect` normalisation.
- `admin/agent/datastudio` — added `SetDialect` call (was unset → `?`-only).
- `pkg/model/crud_test.go` (or companion test file) — rebind unit tests,
  SetDialect-normalisation tests, `TestCRUDLive_PlaceholderPortability`.
- `.github/workflows/ci.yml` — `db-matrix-required` step for live PG+MySQL
  CRUD test lane.

### PR #95 — fix(model): sanitize ORDER BY to close SQL injection in FindAll (d0d041f)

Root cause: `CRUD.FindAll` concatenated the caller-supplied
`QueryOpts.OrderBy` directly into the query string. Reachable via the
data-studio gRPC handler's `req.GetOrderBy()` — an [HIGH] injection vector.

Fix: new unexported `sanitizeOrderBy` in `pkg/model/crud.go` validates each
`<column> [asc|desc]` clause against the `resolveColumn` allow-list and
rebuilds the clause only from allow-listed tokens. Invalid, injecting, or
empty-clause input is rejected before any SQL executes.

Tests: unit tests for `sanitizeOrderBy` + end-to-end test confirming that a
`DROP TABLE` attempt in the `OrderBy` field is rejected and the table survives.

Note: the admin panel HTTP path (`pkg/admin/handlers.go` ~L1160) already had
its own `sanitizeOrderBy`; only the data-studio gRPC path was exposed.

---

## Deferred from this iteration (carried to remediation queue)

- **Two LOW follow-ups from the OrderBy security review** (not blocking):
  1. `parseDBTag` in `pkg/model/meta.go` (~L427) does not validate the
     `column:` tag value through `isValidIdentifierLike` (developer-trust gap;
     no attacker path).
  2. Consolidate the duplicate single-column `sanitizeOrderBy` in
     `pkg/admin/handlers.go` (~L1160) with the model-layer allow-list to
     prevent future drift.

---

## Remediation queue after this iteration (severity order)

1. **F-4** — firewall blind to `/vN` module-path imports + casbin/jwt embedded
   in frozen types (needs contract-guardian + migration-assistant; possibly ADR).
2. **SEC-1** — `corsAllowCredentials` default → `false` + fix misleading R4
   comment in `pkg/app/app.go`.
3. **DOC-1/2** — RATE_LIMITING + MULTISITE guide rewrites (+ AUTH key
   corrections); **WEB-1** — `storage.Metadata`→`PutOptions` on the website.
4. **CLI-V2-1** — scaffold toolchain derived from `go.mod` + freshness test.
5. **GOV-1** — COMPATIBILITY_SLO promotion update + reference-date sweep.
6. **§9 CI** — body-content checks (`docs-content-verifier` discipline) into
   `scripts/website/check-coverage.sh` + a CI lane.

---

## Notes

- `cmd/` is `cmd/nucleus`; CLAUDE.md §directory-map still says `cmd/goframe/`
  (F-13, P3 — fix opportunistically in any docs PR).
- All three PRs (#93/#94/#95) were squash-merged to `main` via the branch →
  push → CI green → `gh pr merge --squash --delete-branch` protocol (direct
  push to `main` is blocked by `enforce_admins=true` branch protection).
