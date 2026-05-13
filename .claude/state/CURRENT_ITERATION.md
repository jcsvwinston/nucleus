# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

No active iteration — ADR-004 integration sprint is complete. Awaiting owner
direction for the next priority.

## Scope

- in: (TBD — owner to confirm from the queue below)
- out: (TBD)

## Acceptance criteria

- [ ] (TBD)

## Status

### Done

- ADR-004 integration sprint fully landed. See archived iteration at
  `docs/iterations/2026-05-13-adr004-integration-sprint.md`.
  - #51 Casbin default-deny wired into App.New (ADR-004)
  - #52 Built-in SendGrid provider removed (DEP-2026-002 / MA-2026-002)
  - #53 JWT manager wired from Config.JWTKeys[] + JWKS auto-mount
  - #54 Circuit-breaker autowrap for mail Send + storage ops

### In progress

- (none)

### Blocked

- (none)

## Candidate next steps (priority order, pending owner confirmation)

1. Re-run post-iteration readiness audit (`docs/audits/`) — last was
   2026-05-13 pre-sprint; sprint is now complete.
2. Tagging decision — v0.6.x patch or v0.7.0 minor for the integration sprint.
3. Track D drills + deprecation/MA seeding + Phase 4 modularization
   (post-rename roadmap).
4. Schema-drift detection + MSSQL/Oracle AutoMigrate scaffolds (P1).
5. ES256 / ECDSA + cloud secret-manager integration (P0, deprioritized).
6. E2E test covering integration sprint (the one unwritten acceptance criterion
   from the sprint — see archived iteration).
7. Contract baseline update: add `pkg/storage` to
   `contracts/baseline/api_exported_symbols.txt`.
8. Cosmetic doc pass: bare code fences in STORAGE_GUIDE.md and
   website/docs/features/storage-and-tasks.md.
9. Standalone MAIL_GUIDE.md for parity with STORAGE_GUIDE.md.

## Files of interest

- `docs/iterations/2026-05-13-adr004-integration-sprint.md` — archived sprint record.
- `pkg/app/app.go` — wire-up site (JWT + Casbin + circuit breaker).
- `pkg/app/config.go` — Auth.JWTKeys[], Mail.CircuitBreaker, Storage.CircuitBreaker.
- `contracts/baseline/api_exported_symbols.txt` — needs pkg/storage entry (follow-up).
- `CHANGELOG.md` — Unreleased section reflects all sprint changes.

## Notes / decisions log

- 2026-05-13 — ADR-004 integration sprint completed across PRs #51–#54.
  Single unmet acceptance criterion (standalone E2E cross-integration test)
  carried forward as a follow-up, not blocking the sprint closure.
- 2026-05-13 — CURRENT_ITERATION.md reset to empty slate; awaiting owner
  direction for next iteration.
