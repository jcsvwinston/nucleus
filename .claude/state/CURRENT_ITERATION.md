# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).
>
> Status: AWAITING OWNER DIRECTION (as of 2026-06-18).
> Previous iterations archived to:
>   docs/iterations/2026-06-18-runtime-jwt-accessor.md
>   docs/iterations/2026-06-18-fleetdesk-repin-rt-jwt.md
>   docs/iterations/2026-06-18-openapi-security-schemes.md

## Goal

<pending — owner to select from candidate directions below>

## Candidate next directions

**(b) Next nucleus friction PRs** — v0.9.x candidates:
- #34 — Anonymous reachability-row footgun (finding #23 extension): forgetting
  a module-level auth middleware silently leaves `/api/*` open; needs a
  pre-authorization identity hook or a framework-level guard pattern.

Earlier open friction candidates (also v0.9.x):
- #27 HIGH — CSRFMiddleware unusable from module-middleware position.
- #29 — `Runtime` has no DBForTenant / tenant-enumeration for background workers.
- #30 — `pkg/storage` local driver does not support SignedURL.
- #18 — fluent Router has no `Router.Static(prefix, fs.FS)`.
- #24 — no per-route http-middleware in fluent Router.
- #25 — keyMatch prefix-only footgun.
- #26 — RequireRole returns JSON (not SSR-friendly).

**(c) Data Studio Phases 0 / A / B / C** — nucleus effort.
Phase 0 = architectural decision on how to distribute the admin SPA
(finding #9 in fleetdesk FINDINGS.md); requires an ADR before coding
starts. Phases A/B/C build on that decision.

## Closed / no longer a candidate

- **(a) Re-pin fleetdesk + close finding #32** — COMPLETE 2026-06-18.
  Finding #32 is fully closed: nucleus side (PR #134, `Runtime.JWT()`)
  + consumer side (fleetdesk commit `3567dac`, apiauth refactor, smoke
  12/12). Archived at
  `docs/iterations/2026-06-18-fleetdesk-repin-rt-jwt.md`.

- **(b-#33) OpenAPI security schemes — finding #33** — COMPLETE 2026-06-18.
  Finding #33 is fully closed: nucleus side (PR #138, `0d3d875`,
  `pkg/openapi` security-scheme surface) + consumer side (fleetdesk
  commit `8686574`, bearer auth declaration, smoke 12/12). Archived at
  `docs/iterations/2026-06-18-openapi-security-schemes.md`.

## Scope

- in: <TBD>
- out: <TBD>

## Acceptance criteria

- [ ] <TBD — owner to define>

## Status

### Done
- (none yet — awaiting iteration start)

### In progress
- (none)

### Blocked
- (none)

## Files of interest

- ~/GolandProjects/fleetdesk/FINDINGS.md (open findings ledger; #32 and #33 now FIXED)
- pkg/auth/ (pre-authz identity hook — finding #34)
- pkg/router/ (per-route middleware gap — finding #24; Router.Static — finding #18)
- pkg/authz/ (keyMatch footgun — finding #25; RequireRole JSON — finding #26)
- pkg/storage/ (local SignedURL gap — finding #30)
- pkg/nucleus/runtime.go (Runtime accessor surface)
- .github/workflows/ci.yml (govulncheck pinned @v1.3.0 — TODO unpin when x/tools fixes TypeParam panic)
- docs/iterations/2026-06-18-openapi-security-schemes.md (last completed iteration)
- docs/iterations/2026-06-18-fleetdesk-repin-rt-jwt.md (prior completed iteration — consumer side)
- docs/iterations/2026-06-18-runtime-jwt-accessor.md (prior completed iteration — nucleus side)

## Notes / decisions log

- 2026-06-17 — Stub created after fleetdesk prototype iteration closed. All
  18 acceptance-criteria matrix items satisfied (S1–S7b). 9 nucleus friction
  fixes merged to main (PRs #117–#120, #122, #123, #125–#127, #129, #131).
  Final fleetdesk commit: 6c09cc0 on local-only main.
- 2026-06-18 — Runtime.JWT() iteration complete. PR #135 (b33eee8) pinned
  govulncheck @v1.3.0 to unblock CI; PR #134 (efddf6c) delivered
  Runtime.JWT(). Finding #32 fixed on nucleus side.
- 2026-06-18 — Finding #32 fully closed. fleetdesk re-pinned to efddf6c
  (pseudoversion efddf6ce3dbb), apiauth refactored to use rt.JWT() — no own
  JWTManager, nucleus.yml JWT config moved to top-level keys. E2E smoke
  12/12. Archived: docs/iterations/2026-06-18-fleetdesk-repin-rt-jwt.md.
- 2026-06-18 — Finding #33 fully closed. nucleus PR #138 (0d3d875) added
  OpenAPI 3.1 security-scheme surface to pkg/openapi (experimental, purely
  additive). fleetdesk commit 8686574 re-pinned to 0d3d875 and declared
  bearerAuth scheme + per-op PublicSecurity() on POST /api/token. Live
  /openapi.json confirmed correct. E2E smoke 12/12. Archived:
  docs/iterations/2026-06-18-openapi-security-schemes.md. Stub reset;
  candidates #32 and #33 both removed from open list.
