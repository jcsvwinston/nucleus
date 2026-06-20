# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).
>
> Status: IN PROGRESS — Orbit extraction, Slice 2 (orbit-side).
> State file was stale (last written 2026-06-19 after finding #35); reconciled
> 2026-06-20 to reflect the orbit foundation epic that landed since then.
>
> Previous iterations archived to:
>   docs/iterations/2026-06-18-runtime-jwt-accessor.md
>   docs/iterations/2026-06-18-fleetdesk-repin-rt-jwt.md
>   docs/iterations/2026-06-18-openapi-security-schemes.md
>   docs/iterations/2026-06-18-router-with-per-route-middleware.md
>   docs/iterations/2026-06-18-csrf-session-key-and-fleetdesk-adoption.md
>   docs/iterations/2026-06-19-authz-ssr-denial-handler.md
>   docs/iterations/2026-06-19-authz-subject-action-resolvers.md
>   docs/iterations/2026-06-20-orbit-extraction-foundation.md

## Goal

**Orbit extraction (ADR-019)** — extract the admin panel from the nucleus core
into `github.com/jcsvwinston/orbit`, a separate pluggable Go module that mounts
itself into a running nucleus application via the public extension API. Orbit is
the owner-chosen active work line and serves as dogfooding consumer #2 of the
Runtime accessor surface (fleetdesk is #1).

## Scope

- in: orbit repo (`~/GolandProjects/orbit`); nucleus `pkg/admin` + `ui/` as
  the move source; orbit construction against Runtime accessors; `Router.Mount`
  as the wiring mechanism; orbit bootstrap allow-list (ADR-004/#19 pattern);
  clean break in nucleus (removing `app.MountAdmin()`, `App.Admin`,
  `admin_prefix`, `admin_title`) at Slice 2.4.
- out: open v0.9.x friction candidates (#23, #34, #14, #29, #30, #18, #25)
  — explicitly deferred until after Slice 2.4; fleetdesk consumer changes
  (beyond re-pin) until orbit's panel is stable.

## Acceptance criteria

- [ ] orbit module builds and passes `go test ./...` against nucleus `daa6706`.
- [ ] `pkg/admin` logic and `ui/` SPA assets live in orbit; nucleus `pkg/admin`
      is gone (or stub-deprecated with removal at Slice 2.4).
- [ ] Panel mounts via `r.Mount(prefix, panel.Handler())` using `Router.Mount`.
- [ ] Panel construction uses only the Runtime accessor API (no direct import of
      nucleus internals).
- [ ] `<prefix>/_orbit/health` probe responds 200 when mounted.
- [ ] Bootstrap allow-list seeded in orbit (ADR-004 / finding #19 pattern).
- [ ] go:embed wires the SPA assets; fleetdesk finding #9 closed.
- [ ] Nucleus breaking removals (`app.MountAdmin`, `App.Admin`, `admin_prefix`,
      `admin_title`) land with deprecation notice + migration doc
      (`contract-guardian` + `migration-assistant`).
- [ ] ADR-019 status flipped from Proposed to Accepted.
- [ ] Full iteration loop green for each nucleus-touching PR; orbit
      `go test ./...` green.

## Status

### Done

- **ADR-019** written and merged (nucleus PR #148, `d85f3b1`, 2026-06-20).
  ADR index backfilled 015–019.
- **Slice 1a** — `Runtime.Models() *model.Registry` + `Runtime.Databases()
  map[string]*sql.DB` + `app.RequestScope` / `app.RequestScopeFromContext`
  (nucleus PR #149, `72f95b4`, 2026-06-20). Contract baseline: additive, 0
  removals.
- **Slice 1b** — `auth.SessionInfo` + `(*SessionManager).ActiveSessions(ctx)`
  + `ErrSessionStoreNotIterable` + `ErrNilSessionManager` (nucleus PR #150,
  `2c7fa28`, 2026-06-20). Contract baseline: additive, 0 removals.
- **Slice 1c** — `Runtime.Observability() EventBus` + `nucleus.EventBus` /
  `nucleus.SQLEvent` / `nucleus.HTTPEvent` (A2 adapter; `pkg/observability`
  not leaked) (nucleus PR #151, `481db78`, 2026-06-20). Contract baseline:
  additive, 0 removals.
- **Slice 2 prereq / Router.Mount** — `nucleus.Router.Mount(pattern string, h
  http.Handler)` (nucleus PR #152, `daa6706`, 2026-06-20). Contract baseline:
  additive, 0 removals.
- **orbit repo scaffolded** — `github.com/jcsvwinston/orbit` (PRIVATE, local
  clone `~/GolandProjects/orbit`, HEAD `9cce16f`). `orbit.Module(Config{Prefix})`
  mounts via Module API; `<prefix>/_orbit/health` probe live; builds + tests
  green. Pinned to nucleus `v0.9.1-0.20260620081822-481db78d3349` (= `481db78`).
- All nucleus-side prerequisites for orbit are merged. Nothing more needed in
  nucleus before Slice 2.2.

### In progress

- **Slice 2.2** (orbit-side) — move `pkg/admin` + `ui/` into orbit; adapt
  construction to Runtime accessors; mount via `r.Mount`; seed bootstrap
  allow-list. Not started yet.

### Blocked

- (none)

## Files of interest

- `~/GolandProjects/orbit` (orbit repo root; HEAD `9cce16f`)
- `~/GolandProjects/orbit/go.mod` (nucleus pin `481db78`; bump to `daa6706`
  before Slice 2.2)
- `pkg/nucleus/runtime.go` (`Runtime.Models`, `Runtime.Databases`,
  `Runtime.Observability` — delivered in Slices 1a/1c)
- `pkg/nucleus/eventbus.go` (`EventBus`, `SQLEvent`, `HTTPEvent` — Slice 1c)
- `pkg/auth/session.go` (`SessionInfo`, `ActiveSessions`,
  `ErrSessionStoreNotIterable`, `ErrNilSessionManager` — Slice 1b)
- `pkg/router/router.go` (`Router.Mount` — Slice 2 prereq, PR #152)
- `pkg/app/scope.go` (`RequestScope`, `RequestScopeFromContext` — Slice 1a)
- `pkg/admin/` (move source for Slice 2.2; remove from nucleus in Slice 2.4)
- `docs/adrs/ADR-019.md` (Proposed; flips Accepted at Slice 2.4)
- `contracts/baseline/api_exported_symbols.txt` (updated through each slice)
- `.github/workflows/ci.yml` (govulncheck pinned @v1.3.0 — do NOT upgrade; see
  standing notes)

## Deferred — open after orbit Slice 2.4

These v0.9.x friction candidates remain open but are explicitly deferred:

- **#23 HIGH** — Global default-deny vs module-middleware order.
- **#34** — Anonymous reachability-row footgun (pre-authz identity hook).
- **#14** — No mux-level body cap (`http.MaxBytesReader`).
- **#29** — `Runtime` has no `DBForTenant` / tenant-enumeration.
- **#30** — `pkg/storage` local driver does not support `SignedURL`.
- **#18** — fluent Router has no `Router.Static(prefix, fs.FS)`.
- **#25** — keyMatch prefix-only footgun.

Fleetdesk findings ledger: **22 FIXED / 12 OPEN** (finding #9 closes when
Slice 2.3 ships the embedded SPA).

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
  docs/iterations/2026-06-18-openapi-security-schemes.md.
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
  methods MiddlewareWithOptions, RequireRoleWithOptions. Existing Middleware()
  and RequireRole() delegate with zero AuthzOptions — default JSON 401/403
  unchanged; no fail-open (security-auditor PASS). Contract baseline +9
  additive, 0 removals. fleetdesk commit e3923b7 re-pinned to
  v0.9.1-0.20260619093054-e33d8ae9f9b2; 8 SSR role-only routes now use
  Router.With(RequireRoleWithOptions(...)) with denyHTTP handler (redirects anon
  / renders forbidden.html for wrong-role); chromeForRequest added. Completes
  SSR per-route guard migration deferred from #24. E2E smoke 12/12. Ledger:
  21 FIXED / 13 OPEN. Finding #35 spun off (POST method→action resolver gap —
  requirePerm stays hand-rolled in fleetdesk until resolved). Archived:
  docs/iterations/2026-06-19-authz-ssr-denial-handler.md.
- 2026-06-19 — Finding #35 fully closed. nucleus PR #146 (32a01a0) added
  pluggable SubjectResolver and ActionResolver to pkg/authz: new types
  SubjectResolver (func(r *http.Request, claims *auth.Claims) string) and
  ActionResolver (func(r *http.Request) string); new fields
  AuthzOptions.ResolveSubject and AuthzOptions.ResolveAction. Defaults
  unchanged when nil; resolver returning "" safely denied + Warn logged; no
  fail-open (security-auditor PASS). Contract baseline +4 additive, 0 removals.
  fleetdesk commit 3996e18 re-pinned to v0.9.1-0.20260619132308-32a01a002e72;
  hand-rolled requirePerm and forbidden removed; 5 state-changing ticket/alert
  routes now use Router.With(MiddlewareWithOptions(...)) with custom
  ResolveSubject (role) and ResolveAction (actionFor). E2E smoke 12/12.
  Ledger: 22 FIXED / 12 OPEN. With #26, COMPLETES the full SSR authz story.
  Archived: docs/iterations/2026-06-19-authz-subject-action-resolvers.md.
- 2026-06-20 — Orbit foundation complete. ADR-019 written + merged (PR #148,
  d85f3b1). Slice 1a: Runtime.Models/Databases + app.RequestScope (PR #149,
  72f95b4). Slice 1b: auth.SessionInfo + ActiveSessions + sentinels (PR #150,
  2c7fa28). Slice 1c: Runtime.Observability + EventBus/SQLEvent/HTTPEvent
  (PR #151, 481db78). Router.Mount (PR #152, daa6706). All nucleus-side
  prerequisites for orbit done. orbit repo created
  (github.com/jcsvwinston/orbit, PRIVATE; local ~/GolandProjects/orbit; HEAD
  9cce16f; pins nucleus 481db78). CURRENT_ITERATION rebuilt from scratch
  (state file was stale since 2026-06-19 finding-#35 close). Archived:
  docs/iterations/2026-06-20-orbit-extraction-foundation.md.
