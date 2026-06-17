# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).
>
> Status: AWAITING OWNER DIRECTION (as of 2026-06-17).
> Previous iteration archived to:
>   docs/iterations/2026-06-17-fleetdesk-prototype-s7-close.md

## Goal

<pending — owner to select from candidate directions below>

## Candidate next directions

**(a) Data Studio Phases 0 / A / B / C** — nucleus effort.
Phase 0 = architectural decision on how to distribute the admin SPA
(finding #9 in fleetdesk FINDINGS.md); requires an ADR before coding
starts. Phases A/B/C build on that decision.

**(b) Nucleus friction PRs from the fleetdesk prototype** — three new
v0.9.x candidates surfaced in S7b:
- #32 — `Runtime` exposes no JWT accessor; candidate: `Runtime.JWT()` (the
  natural 5th accessor alongside Session/Authorizer/Mailer/Storage).
- #33 — `pkg/openapi` Document/Components/Operation have no security-scheme
  support; bearer auth is undeclarable in a contract.
- #34 — Anonymous reachability-row footgun (extends finding #23): forgetting
  a module-level auth middleware silently leaves `/api/*` open; needs a
  pre-authorization identity hook or a framework-level guard pattern. Fleetdesk
  instance FIXED in S7b via `platform.APIAuth`.

Earlier open friction candidates (also v0.9.x):
- #27 HIGH — CSRFMiddleware unusable from module-middleware position.
- #29 — `Runtime` has no DBForTenant / tenant-enumeration for background workers.
- #30 — `pkg/storage` local driver does not support SignedURL.
- #18 — fluent Router has no `Router.Static(prefix, fs.FS)`.
- #24 — no per-route http-middleware in fluent Router.
- #25 — keyMatch prefix-only footgun.
- #26 — RequireRole returns JSON (not SSR-friendly).

**(c) Re-pin fleetdesk** once any of the above friction PRs publish to the
Go proxy, then re-run `go test -tags e2e -run TestE2ESmoke .` to verify.

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

- ~/GolandProjects/fleetdesk/FINDINGS.md (open findings ledger)
- pkg/auth/runtime.go (Runtime accessor surface; candidate Runtime.JWT())
- pkg/openapi/ (security-scheme gap — finding #33)
- pkg/router/ (per-route middleware gap — finding #24; Router.Static — finding #18)
- pkg/authz/ (keyMatch footgun — finding #25; RequireRole JSON — finding #26)
- pkg/storage/ (local SignedURL gap — finding #30)
- docs/iterations/2026-06-17-fleetdesk-prototype-s7-close.md (last completed iteration)

## Notes / decisions log

- 2026-06-17 — Stub created after fleetdesk prototype iteration closed. All
  18 acceptance-criteria matrix items satisfied (S1–S7b). 9 nucleus friction
  fixes merged to main (PRs #117–#120, #122, #123, #125–#127, #129, #131).
  Final fleetdesk commit: 6c09cc0 on local-only main.
