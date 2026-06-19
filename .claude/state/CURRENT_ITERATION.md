# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).
>
> Status: AWAITING OWNER DIRECTION (as of 2026-06-19).
> Previous iterations archived to:
>   docs/iterations/2026-06-18-runtime-jwt-accessor.md
>   docs/iterations/2026-06-18-fleetdesk-repin-rt-jwt.md
>   docs/iterations/2026-06-18-openapi-security-schemes.md
>   docs/iterations/2026-06-18-router-with-per-route-middleware.md
>   docs/iterations/2026-06-18-csrf-session-key-and-fleetdesk-adoption.md
>   docs/iterations/2026-06-19-authz-ssr-denial-handler.md

## Goal

<pending — owner to select from candidate directions below>

## Candidate next directions

**(b) Next nucleus friction PRs** — v0.9.x candidates (in recommended priority order):

- **#35** — `Enforcer.Middleware` / `MiddlewareWithOptions` derive the CRUD action
  from HTTP method only (every POST → `"create"`). An SSR app that POSTs to both
  update and delete routes cannot enforce a delete deny-override through the framework
  middleware. fleetdesk keeps a hand-rolled `requirePerm` for this case. Candidate
  fix: let the authz middleware accept a path/method→action resolver function.
  Natural follow-on to #26 — completes the full SSR authz story. Spun off 2026-06-19.

- **#23 HIGH** — Global default-deny vs module-middleware order. Module middleware
  that applies an auth gate runs in an ambiguous order relative to the global
  default-deny policy; the interaction is not tested and the mental model is
  unclear. Same root class as #26 and #34.

- **#34** — Anonymous reachability-row footgun: forgetting a module-level auth
  middleware silently leaves `/api/*` open. Needs a pre-authorization identity
  hook or a framework-level guard pattern (pkg/auth/). Note: fleetdesk has a
  local workaround; nucleus-side gap remains open.

- **#14** — No mux-level body cap. Missing `http.MaxBytesReader` on the global
  mux means handlers are vulnerable to request-body exhaustion. Touches the same
  `pkg/router/` area as the CSRF work already landed.

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
  role check). Note: SSR guard adoption of With was deferred until #26
  resolved — that deferral is now complete (see #26 below). Archived at
  `docs/iterations/2026-06-18-router-with-per-route-middleware.md`.

- **(b-#27) CSRF session-key fix + fleetdesk adoption — finding #27** — COMPLETE 2026-06-18.
  Finding #27 is fully closed: nucleus side (PR #142, `a6beffc`,
  `CSRFToken` honours any `SessionKey`; token available on origin shortcut)
  + consumer side (fleetdesk commit `7e9666b`, hand-rolled CSRF dropped,
  framework `CSRFMiddleware` adopted, 419 on token-mismatch, smoke 12/12).
  NOTE: original root-cause diagnosis was disproved — `injectDependencies`
  wraps the group middleware chain, so the session IS in context when
  `Module.Middleware` runs; actual bugs were the hard-coded key and the skipped
  injection on the shortcut path. Archived at
  `docs/iterations/2026-06-18-csrf-session-key-and-fleetdesk-adoption.md`.

- **(b-#26) Authz SSR-friendly denial handler — finding #26** — COMPLETE 2026-06-19.
  Finding #26 is fully closed: nucleus side (PR #144, `e33d8ae`, new types
  `Denial`, `DenialHandler`, `AuthzOptions`; new methods
  `MiddlewareWithOptions`, `RequireRoleWithOptions` in `pkg/authz`) +
  consumer side (fleetdesk commit `e3923b7`, 8 SSR role-only routes now use
  `Router.With(RequireRoleWithOptions(...))` with `denyHTTP` handler,
  `chromeForRequest` added to chrome.go, smoke 12/12). Completes the SSR
  per-route guard migration deferred from #24. Spun off finding #35 (POST
  method→action resolver gap). Archived at
  `docs/iterations/2026-06-19-authz-ssr-denial-handler.md`.

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

- `~/GolandProjects/fleetdesk/FINDINGS.md` (open findings ledger; 21 FIXED / 13 OPEN; #35 newly OPEN)
- `pkg/authz/middleware.go` (MiddlewareWithOptions, RequireRoleWithOptions — finding #35 is the follow-on here)
- `~/GolandProjects/fleetdesk/internal/webui/authz.go` (roleGuard + denyHTTP; requirePerm retained for #35)
- `pkg/auth/` (pre-authz identity hook — finding #34; global default-deny order — finding #23)
- `pkg/router/` (no mux-level body cap — finding #14; Router.Static — finding #18)
- `pkg/storage/` (local SignedURL gap — finding #30)
- `pkg/nucleus/runtime.go` (Runtime accessor surface; DBForTenant — finding #29)
- `.github/workflows/ci.yml` (govulncheck pinned @v1.3.0 — TODO unpin when x/tools fixes TypeParam panic)
- `docs/iterations/2026-06-19-authz-ssr-denial-handler.md` (last completed iteration)
- `docs/iterations/2026-06-18-csrf-session-key-and-fleetdesk-adoption.md` (prior completed iteration)

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
- 2026-06-18 — Finding #27 fully closed. nucleus PR #142 (a6beffc) fixed
  CSRFToken to honour any configured SessionKey (was hard-coded to "csrf_token")
  and reordered token injection to run before the Layer-1 same-origin shortcut
  so form-rendering handlers receive a token on GET. IMPORTANT: original
  root-cause diagnosis (session not in context when Module.Middleware runs) was
  disproved — injectDependencies wraps the whole group middleware chain, so the
  session IS present; actual bugs were the hard-coded key and the skipped
  injection on the shortcut path. fleetdesk commit 7e9666b dropped hand-rolled
  CSRF (platform.EnsureCSRFToken + module csrf middleware), adopted
  router.CSRFMiddleware via frameworkCSRF() helper, exported
  platform.CSRFSessionKey="fd_csrf", rotates on login (OWASP), token-mismatch
  now answers 419 (was 403). E2E smoke 12/12. Ledger: 20 FIXED / 13 OPEN.
  Archived: docs/iterations/2026-06-18-csrf-session-key-and-fleetdesk-adoption.md.
- 2026-06-19 — Finding #26 fully closed. nucleus PR #144 (e33d8ae) added
  opt-in denial handler to pkg/authz: types Denial, DenialHandler, AuthzOptions;
  methods MiddlewareWithOptions, RequireRoleWithOptions. Existing Middleware() and
  RequireRole() delegate with zero AuthzOptions — default JSON 401/403 unchanged;
  no fail-open (security-auditor PASS). Contract baseline +9 additive, 0 removals.
  fleetdesk commit e3923b7 re-pinned to v0.9.1-0.20260619093054-e33d8ae9f9b2;
  8 SSR role-only routes now use Router.With(RequireRoleWithOptions(...)) with
  denyHTTP handler (redirects anon / renders forbidden.html for wrong-role);
  chromeForRequest added. Completes SSR per-route guard migration deferred from
  #24. E2E smoke 12/12. Ledger: 21 FIXED / 13 OPEN. Finding #35 spun off (POST
  method→action resolver gap — requirePerm stays hand-rolled in fleetdesk until
  resolved). Archived: docs/iterations/2026-06-19-authz-ssr-denial-handler.md.
