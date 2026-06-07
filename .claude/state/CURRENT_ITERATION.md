# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).
>
> SEEDED by the 2026-06-07 audit-v2 session as the next iteration's kickoff.
> NOT STARTED — per §2.3, confirm this goal with the maintainer before any
> code is written.

## Goal

Fix F-3 from `docs/audits/2026-06-07-exhaustive-audit-v2.md`: make the model
CRUD layer placeholder-portable across all five SQL engines, and add live
PostgreSQL CRUD coverage so the SQLite-only blind spot cannot recur.

## Scope

- in: `pkg/model/crud.go` — central rebind of `?` placeholders keyed on the
  existing `c.dialect` field (postgres → `$N`; sqlserver/mssql → `@pN`;
  oracle → `:N`; mysql/sqlite unchanged). Hook it once in `execContext`
  (~L709) and `queryContext` (~L716) so every CRUD path is covered, INCLUDING
  the per-dialect `getEstimate` count queries (~L218-226) which today use `?`
  even in their postgres/mssql/oracle branches. Reuse/align with the existing
  postgres pattern in `pkg/auth/session_store_sql.go:253` and
  `pkg/outbox/outbox.go:304` (consider promoting a shared helper into
  `pkg/db` if it does not change frozen surfaces). Add CRUD tests that run
  against live PostgreSQL in the `db-matrix-required` lane (and ideally MySQL),
  exercising Create/Find/List/Update/Delete/search paths.
- out: F-4 (firewall /vN — separate iteration, needs contract-guardian +
  migration-assistant on the casbin/jwt embeds), SEC-1 (CORS default),
  DOC-1/2, WEB-1, CLI-V2-1 — queued in HANDOFF.md.

## Acceptance criteria

- [ ] All CRUD-emitted SQL uses engine-correct placeholders (unit-verifiable
      via a rebind helper test table covering the five dialects).
- [ ] A live-PG CRUD test exists, fails on pre-fix code ("syntax error at or
      near \"?\"" class), passes post-fix, and runs in the postgresql CI lane
      (never mock the DB — CLAUDE.md §7).
- [ ] SQLite + MySQL behaviour unchanged (`go test ./...` green; db-matrix
      lanes green).
- [ ] No frozen symbol added/removed without contract-guardian sign-off; if a
      helper lands in `pkg/db`, run the freeze flow deliberately.
- [ ] CHANGELOG.md Unreleased entry (bug fix, patch impact) via
      changelog-writer; guides untouched by this fix stay untouched.
- [ ] Full Iteration Loop (§4) run; security-auditor pass included (SQL paths
      touched).

## Status

### Done
- (none yet)

### In progress
- (not started — awaiting maintainer confirmation at session start)

### Blocked
- (none)

## Files of interest

- pkg/model/crud.go (execContext L709, queryContext L716, placeholders L696,
  getEstimate L212-232, c.dialect field)
- pkg/auth/session_store_sql.go:253 + pkg/outbox/outbox.go:304 (existing
  postgres rebind patterns)
- pkg/db/schema_drift.go:328 (mssql @pN precedent note)
- .github/workflows/ci.yml (db-matrix-required lane), docs/governance/CI_MATRIX.md
- docs/audits/2026-06-07-exhaustive-audit-v2.md §3/§10 (finding + roadmap)

## Notes / decisions log

- 2026-06-07 — Iteration seeded by the audit-v2 session. Remediation queue
  after this one (severity order, one branch+PR each): F-4 firewall /vN +
  leak disposition; SEC-1 CORS credentials default; DOC-1/2 guide rewrites +
  WEB-1 website PutOptions fix; CLI-V2-1 scaffold toolchain derivation;
  GOV-1 SLO promotion update. Full list: audit report §10 + HANDOFF.md.
