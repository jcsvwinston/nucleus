# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

Build prototype #1 — **fleetdesk**: a full-stack, multi-tenant MVC application
(IoT SIM/device fleet portal) with server-rendered UI + React islands, pinned
to published nucleus (pseudoversion tracking main @ db9759d), exercising
**every** Nucleus capability with maximal use of the framework's exposed
methods and readable, elegant code.

## Scope

- in: repo `~/GolandProjects/fleetdesk` (module `github.com/jcsvwinston/fleetdesk`),
  scaffolded via `nucleus new` (mvc), NO `replace` directive — the real-user path.
  Domain: tenants = client companies; SIMs/devices, usage records, alerts, members.
  Frontend: Go `html/template` SSR (`c.HTML` + `App.Templates`) + React islands
  (Vite build embedded via `go:embed`, hydration on mount points) + rich visuals.
  Full framework coverage matrix (see acceptance criteria).
- in: framework friction found along the way → recorded, fixed via small PRs to
  nucleus (v0.9.x), prototype re-pinned as patches publish.
- out: Node-based SSR sidecar (bypasses the framework); `Module.Jobs`/`Module.Webhooks`
  (reserved shape — use `pkg/tasks` directly); `outbox.NewKafkaBridge` (unfinished);
  external mail plugins (return v0.9.X per ADR-010).

## Acceptance criteria (framework-coverage matrix)

- [ ] `pkg/nucleus` fluent builder: `New().FromConfigFile(...)` + `Mount(Module[C])`
      with typed module config (`default:`/`validate:` tags), `Runtime` hooks,
      `WithOpenAPI`, `.Start()` terminal.
- [ ] Multi-tenant: `multitenant.*` config (header resolver to start), ≥2 seed
      tenants, `RequestScope`/`TenantFromContext`, tenant-scoped data isolation,
      admin auto-filter.
- [ ] `pkg/model`: registered models (BaseModel, FKs, indexes, tenant field),
      CRUD with `QueryOpts` (search/filters/multi-column order_by), migrations
      via `nucleus makemigrations`/`migrate`.
- [ ] SSR UI: `c.HTML` pages from `App.Templates` + React islands (Vite,
      `go:embed`), visually rich dashboard (charts), responsive.
- [x] `pkg/auth`: session login (SQL store) + JWT for the API (HS256 keyset);
      `pkg/authz`: casbin RBAC (admin/operator/viewer), `RequireRole`,
      4-col policies, a deny rule, policy inspection via forwarders.
- [ ] `pkg/admin`: panel mounted with `RBACEnforcer`, multi-tenant selector,
      audit log, feature flags, live view, exports/imports.
- [ ] `pkg/tasks`: Manager handlers (usage rollup, report generation),
      Scheduler (daily), Inspector wired into admin jobs view.
- [ ] `pkg/signals`: Bus events (e.g. `sim.activated`) → mail + audit, EmitAsync.
- [ ] `pkg/mail`: smtp (dev: mailhog/noop) alert + welcome templates.
- [ ] `pkg/storage`: local store; report exports via `Put`/`SignedURL`.
- [ ] `pkg/observe` + `pkg/health`: slog, Prometheus `/metrics`, `/healthz`
      with custom checks.
- [ ] `pkg/validate`: form/API validation incl. one custom rule via `RegisterRule`.
- [ ] `pkg/errors`: DomainError-based handlers end to end.
- [ ] Router: groups, rate limiting (`RateLimitMiddleware` scopes), CORS
      config keys, `Resource` REST, `FromHTTP`.
- [ ] `pkg/circuit`: breaker around a simulated carrier API call.
- [ ] OpenAPI served (`WithOpenAPI` + `nucleus openapi`).
- [ ] CLI exercised: `new`, `generate resource`, `makemigrations`, `migrate`,
      `doctor`, `config`, `openapi`, `serve`.
- [ ] E2E smoke green; README documents the coverage matrix; zero `replace`.

## Status

### Done (as of 2026-06-14)

**Nucleus fixes (merged to main, re-pinned in fleetdesk):**

- 2026-06-10 — PR #118 (fd74118): `router.BindForm` — was a stub
  (string-map→JSON; non-string fields failed, no validation). Now
  reflection-based, typed (bools/ints/uints/floats/time.Time/pointers,
  form:/json: tags, multipart, 10 MiB cap), validate parity with BindJSON.
  Contract: +1 additive baseline line. Website routing.md documents the rules
  incl. honest BindXML-does-not-validate asymmetry. Fixes finding #13.
- 2026-06-10 — PR #117 (5de51dc): Admin model→database attribution by probed
  presence (server + grouping side). Fixes finding #11 (part 1).
- 2026-06-10 — PR #119 (3d1a7d2): Data Studio DATABASE FILTER dropdown now
  matches probed homes + flat-list click passes filtered alias through.
  Verified live: tenant_acme filter lists 11 tenant models, Alerts loads 14
  rows. Fixes finding #11 (part 2, complete).
- 2026-06-10 — PR #120 (db9759d): Rejected admin login was SILENT with SPA
  installed (renderLoginPage dropped the error). Now: injectHeadMeta helper,
  SPA banner, Cache-Control: no-store, meta consumed on mount. Fixes finding
  #16. New finding #17 (login timing oracle, LOW) logged for S4.
- 2026-06-11 — PR #122 (6b3ea75): bare /admin GET → 403 root-caused in
  pkg/authz (BootstrapAllowList keyMatch prefix-only gap). Exact-match /admin
  row + custom admin_prefix mirror in pkg/app + regression tests. Fixes
  finding #19.
- 2026-06-11 — PR #123 (85560e5): drift-guard SIGPIPE flake under pipefail
  (grep -q exits 141, if ! misreads). Fixed; all CI checks green.
- 2026-06-14 — PR #125 (2a70c54): fluent auth surface —
  `Runtime.Session()`/`Runtime.Authorizer()` + exported
  `auth.ContextWithClaims`. auth.md/routing.md corrected; ADR-010 amended;
  baseline +3 additive. Fixes findings #20/#21.
- 2026-06-14 — PR #127 (216ae06): `router.BindForm` skips `db:"pk"` and
  `db:"readonly"` fields (mass-assignment guard; all db-tag aliases covered).
  Fixes finding #15.
- 2026-06-14 — PR #126 (64d28dd): admin login timing equalization — dummy
  cost-12 bcrypt on unknown-user path; ADR-017 filed. Fixes finding #17.

**Fleetdesk prototype (~/GolandProjects/fleetdesk):**

- 2026-06-10 — 7 server-rendered list pages (/fleets /devices /sims /alerts
  /tickets /billing /team) + shared chrome partials (templates/chrome.html) +
  generic list.html cell model; dashboard refactored onto the chrome.
  RBAC: anonymous read rows (until S4).
- 2026-06-10 — Tickets FULL CRUD server-rendered: new/create with BindForm
  bind+validate and 422 error re-render, edit/update via CRUD allow-listed
  partial update, delete soft. Alert workflow buttons ack/resolve with
  transition guard (409 on illegal transition). Verified end-to-end incl.
  tenant isolation (row in acme, absent in borealis).
- 2026-06-10 — nucleus pin: v0.9.1-0.20260610184553-db9759d5822a (no replace).
- 2026-06-10 — FINDINGS.md ledger: #11 FIXED, #13 FIXED, #16 FIXED.
- 2026-06-11 (afternoon) — FINDINGS.md ledger: #19 FIXED (PR #122).
- 2026-06-10 — Admin demo credentials reset: admin / fleetdesk-demo.
- 2026-06-11 — S3 React islands sub-slice shipped (fleetdesk commit 840b259):
  web/ Vite+React scaffold building one deterministic dist/islands.js; embedded
  via web/embed.go (go:embed all:dist) — binary self-contained, fresh clone
  compiles without Node and degrades to pure SSR (dist/.gitkeep placeholder;
  islands are progressive enhancement); assets served at /assets/{path...} via
  router.FromHTTP + http.FileServerFS with listing guard and Cache-Control:
  no-cache; tenant-scoped /dashboard/usage.json feed (no-store) sharing
  loadUsageSeries with the SSR chart; usage-chart island hydrates the SSR usage
  card (15 s polling paused while hidden + refresh on visibilitychange, hover
  crosshair/tooltip, freshness badge, honest day-over-day delta only when the
  series reaches today); RBAC rows for /assets/* and /dashboard/usage.json
  (anonymous until S4); README rewritten; stray tracked binary dropped. Verified
  live on acme + borealis (isolated series; poll picked up rows inserted into
  tenant_acme.db without reload). code-reviewer: NITS (all addressed);
  security-auditor: PASS.
- 2026-06-11 — FINDINGS #18 (MED, OPEN): fluent Router has no static-file path
  (no Router.Static; raw http.Handler unreachable); candidate
  Router.Static(prefix, fs.FS) for v0.9.x.
- 2026-06-11 — Finding #19 (user-reported): GET /admin answered 403 FORBIDDEN
  — root cause in nucleus pkg/authz; fixed PR #122; re-pinned to
  v0.9.1-0.20260611064010-6b3ea757c461 (commit be15965); workaround removed;
  admin login verified end-to-end.
- 2026-06-11 — CI drift-guard flake (PR #123) fixed and all checks green.
- 2026-06-14 — nucleus re-pinned to v0.9.1-0.20260611164013-64d28dd8eeb6
  (commit 1e8f2d5, fleetdesk); dropped t.ID=0 workaround now that BindForm
  guards server-owned fields (PR #127).
- 2026-06-14 — S4a (fleetdesk e81e87e): per-tenant staff login + server-side
  sessions. gate middleware (Module.Middleware), auth.ContextWithClaims bridge,
  session fixation defence (RenewToken), tenant-bound sessions, timing
  equalization (unknown/wrong-pw/inactive all run bcrypt), sanitizeNext
  open-redirect guard (incl. backslash), login.html + chrome userbar,
  session_cookie_secure:false for dev, real cost-12 bcrypt seed hashes (demo
  password "fleetdesk").
- 2026-06-14 — S4b (fleetdesk 09878b9): casbin RBAC admin/operator/viewer.
  requireRole (wraps framework Enforcer.RequireRole via nopResponseWriter for
  styled 403) + requirePerm (Enforcer.Can over 4-col policy table) + genuine
  deny-override (operator: everything on tickets EXCEPT delete) + admin-only
  /access inspector via GetPolicy forwarder. Delete distinguished by ACTION not
  path-suffix (keyMatch is prefix-only — caught in review). UI mirrors guards
  (action cells, +New, Billing/Access nav hidden by role).
- 2026-06-14 — S4c (fleetdesk 2dbbe82): CSRF (two-layer: Sec-Fetch-Site
  same-origin OR session-stored 256-bit token, constant-time, rotated on login,
  fail-closed) + CORS allow-list (cors_origins pinned to dev tenant hosts,
  credentials:false) + Chrome view-model refactor (all view models embed
  Chrome). fleetdesk manages its own CSRF token because the framework's
  CSRFMiddleware can't see the session from module-middleware position (finding
  #27 HIGH).
- 2026-06-14 — NEW framework findings logged in fleetdesk FINDINGS.md:
  #24 (no per-route http-middleware in fluent Router),
  #25 (keyMatch prefix-only footgun, surfaced during S4b delete-guard review),
  #26 (RequireRole returns JSON, not SSR-friendly),
  #27 HIGH (session-based CSRFMiddleware unusable from module middleware —
  session injected into context after module mw; #24-#27 are v0.9.x PR
  candidates, #27 is HIGH priority).
- 2026-06-14 — S4 complete, verified live across all roles (admin/operator/
  viewer on acme + borealis); FINDINGS fixed cumulative: #11/#13/#15/#16/#17/
  #19/#20/#21.

**Slices completed:** S1, S2, S3, S4.

**Findings status:**
- FIXED: #11, #13, #15, #16, #17, #19, #20, #21
- OPEN: #4, #5, #9, #12, #14, #18, #22, #23, #24, #25, #26, #27
  (framework-friction findings #24–#27 are v0.9.x PR candidates; #27 is HIGH)

### In progress

- (none)

### Blocked

- (none)

## Task ladder (full sequence)

- [x] S1: scaffold, git init, first boot, multi-tenant config (2 tenants),
       base models + first migration.
- [x] S2: 7 list pages + chrome partials + dashboard refactor.
- [x] S3: Tickets CRUD + React islands (Vite + go:embed, complete 2026-06-11).
- [x] S4: sessions + casbin RBAC (admin/operator/viewer) + CSRF + CORS +
       findings #15/#17 (complete 2026-06-14).
- [ ] S5: tasks/signals/mail/storage.
       pkg/tasks: usage rollup + report generation handlers, Scheduler (daily),
       Inspector wired into admin jobs view.
       pkg/signals: Bus events (sim.activated → mail + audit, EmitAsync).
       pkg/mail: smtp dev mailhog/noop; alert + welcome templates.
       pkg/storage: local store; report exports via Put/SignedURL.
- [ ] S6: admin/observability/openapi/limits/circuit + finding #14.
- [ ] S7: E2E + docs-truth pass for findings #4/#5/#9/#12.
- [ ] Data Studio Phases 0/A/B/C (Phase 0 = finding #9 ADR decision).

## Files of interest

- ~/GolandProjects/fleetdesk (prototype repo)
- ~/GolandProjects/fleetdesk/FINDINGS.md (friction ledger)
- ~/GolandProjects/fleetdesk/go.mod (nucleus pseudoversion pin: 64d28dd8eeb6)
- ~/GolandProjects/fleetdesk/internal/webui/auth.go (gate middleware + login)
- ~/GolandProjects/fleetdesk/internal/webui/authz.go (requireRole/requirePerm)
- ~/GolandProjects/fleetdesk/internal/webui/csrf.go (two-layer CSRF)
- ~/GolandProjects/fleetdesk/internal/webui/chrome.go (Chrome view-model)
- ~/GolandProjects/fleetdesk/internal/webui/access.go (/access inspector)
- ~/GolandProjects/fleetdesk/internal/platform/session.go (session + CSRF helpers)
- ~/GolandProjects/fleetdesk/rbac_policy.csv (anon reachability + role policies
  with deny-override)
- ~/GolandProjects/fleetdesk/nucleus.yml (session_cookie_secure:false, cors_origins)
- ~/GolandProjects/fleetdesk/templates/chrome.html (shared chrome partial)
- pkg/router/context.go (BindForm, BindJSON, c.HTML)
- pkg/app/requestscope.go (tenant scope)
- pkg/admin/login.go (injectHeadMeta helper — PR #120)
- docs/guides/MULTISITE_GUIDE.md (rewritten 2026-06-09)
- internal/cli/new.go (defaultPinnedFrameworkVersion)
- .claude/state/CURRENT_ITERATION.md

## Notes / decisions log

- 2026-06-10 — Prototype #1 = fleetdesk (IoT fleet portal). SSR = Go templates
  + React islands (NOT a Node sidecar): maximizes framework usage. Repo pinned
  to published nucleus, no replace.
- 2026-06-10 — Distilled slices later become the ADR-010 Phase 4 Slice 2/3
  reference apps + harness fixture profiles (v0.9.X).
- 2026-06-10 — Frictions found → small nucleus PRs (prototype is also the
  framework's validation vehicle). Four PRs merged today (#117–#120).
- 2026-06-10 — Preview server (launch.json "fleetdesk", port 8080) dies when
  tooling session recycles — benign, restart with preview_start.
- 2026-06-10 — gh REST (pr checks/merge) 401s intermittently; workaround:
  poll via `gh pr view N --json statusCheckRollup`, merge via GraphQL
  mergePullRequest mutation.
- 2026-06-10 — App URLs: http://acme.fleetdesk.localhost:8080/ (and borealis.)
  — needs ≥3 host labels; admin at /admin.
- 2026-06-11 — Dev loop for islands: npm build + go build (bundle embeds at
  compile time); launch.json runs ./app so the binary must be rebuilt before
  preview restarts to pick up island changes.
- 2026-06-11 — Bare /admin gap never bit before because navigation always used
  /admin/ or the login redirect; quickstart-documented bare URL only got
  exercised via the README real-user path.
- 2026-06-11 — PR #123 (drift-guard flake) intentionally skipped the subagent
  loop — scripts/-only, self-verified 5/5 deterministic strict runs.
- 2026-06-14 — S4b delete-guard: framework keyMatch is prefix-only, so action
  discrimination (DELETE vs GET/POST) must be done by ACTION column in the
  4-col policy, not by path suffix. Casbin deny-override verified working.
- 2026-06-14 — fleetdesk manages its own CSRF token (finding #27 HIGH) because
  framework CSRFMiddleware cannot see the session when called from module
  middleware position (session injected into context later in the chain).
  This is the richest upstream follow-up: a nucleus PR fixing #27 would
  directly simplify fleetdesk S5+.
- 2026-06-14 — Demo login: admin@acme.example / operator@acme.example /
  viewer@acme.example, password "fleetdesk"; admin panel still admin /
  fleetdesk-demo. acme and borealis tenants available.
- F-13 (P3, non-blocking): CLAUDE.md §directory-map still says cmd/goframe/;
  actual entry-point is cmd/nucleus/. Fix opportunistically in any docs PR.
