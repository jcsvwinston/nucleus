# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).
>
> Status: AWAITING OWNER DIRECTION (as of 2026-06-18).
> Previous iterations archived to:
>   docs/iterations/2026-06-18-runtime-jwt-accessor.md
>   docs/iterations/2026-06-18-fleetdesk-repin-rt-jwt.md
>   docs/iterations/2026-06-18-openapi-security-schemes.md
>   docs/iterations/2026-06-18-router-with-per-route-middleware.md

## Goal

<pending — owner to select from candidate directions below>

## Candidate next directions

**(b) Next nucleus friction PRs** — v0.9.x candidates (in recommended priority order):

- **#27 HIGH** — CSRFMiddleware unusable from module-middleware position.
  Most attractive next: it is HIGH severity, and its root cause (module
  middleware runs after session injection / the global auth gate) is the
  same class of problem as #26 and #34. Fixing it here likely unblocks
  the others.

- **#26** — `RequireRole` returns JSON 403 (not SSR-friendly). Now also
  implicated by the #24 close: fleetdesk's SSR guards cannot adopt
  `With(Enforcer.RequireRole(...))` until this is resolved. Fixing #26
  would let the per-route middleware story extend to SSR apps.

- **#34** — Anonymous reachability-row footgun (finding #23 extension):
  forgetting a module-level auth middleware silently leaves `/api/*` open;
  needs a pre-authorization identity hook or a framework-level guard
  pattern.

Earlier open friction candidates (also v0.9.x):
- #29 — `Runtime` has no DBForTenant / tenant-enumeration for background workers.
- #30 — `pkg/storage` local driver does not support SignedURL.
- #18 — fluent Router has no `Router.Static(prefix, fs.FS)`.
- #25 — keyMatch prefix-only footgun.

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

- **(b-#24) Router.With() per-route middleware — finding #24** — COMPLETE 2026-06-18.
  Finding #24 is fully closed: nucleus side (PR #140, `05fb701`,
  `Router.With()` on the stable `nucleus.Router` interface) + consumer
  side (fleetdesk commit `c5d969f`, nopResponseWriter removed, direct
  role check). Note: fleetdesk SSR guards do NOT yet use `With` (deferred
  until #26 resolved). Archived at
  `docs/iterations/2026-06-18-router-with-per-route-middleware.md`.

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

- ~/GolandProjects/fleetdesk/FINDINGS.md (open findings ledger; #32, #33, #24 now FIXED)
- pkg/router/ (CSRF gap — finding #27; Router.Static — finding #18)
- pkg/authz/ (keyMatch footgun — finding #25; RequireRole JSON / SSR-unfriendly — finding #26)
- pkg/auth/ (pre-authz identity hook — finding #34)
- pkg/storage/ (local SignedURL gap — finding #30)
- pkg/nucleus/runtime.go (Runtime accessor surface; DBForTenant — finding #29)
- .github/workflows/ci.yml (govulncheck pinned @v1.3.0 — TODO unpin when x/tools fixes TypeParam panic)
- docs/iterations/2026-06-18-router-with-per-route-middleware.md (last completed iteration)
- docs/iterations/2026-06-18-openapi-security-schemes.md (prior completed iteration)

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
- 2026-06-18 — Finding #24 fully closed. nucleus PR #140 (05fb701) added
  Router.With() to the stable nucleus.Router interface; routerAdapter
  delegates to router.Mux.With (inline sub-Mux, middleware isolated to
  returned Router only). Security-auditor concern re Resource bypass
  disproved by regression test. fleetdesk commit c5d969f dropped
  nopResponseWriter; requireRole now checks directly via Enforcer.RequireRole,
  symmetric with requirePerm. E2E smoke 12/12. SSR guard adoption of With
  deferred until finding #26 resolved. Archived:
  docs/iterations/2026-06-18-router-with-per-route-middleware.md.
