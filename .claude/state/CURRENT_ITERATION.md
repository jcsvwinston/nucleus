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
- [ ] `pkg/auth`: session login (SQL store) + JWT for the API (HS256 keyset);
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

### Done (as of 2026-06-10)

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

**Fleetdesk prototype (~/GolandProjects/fleetdesk, main @ e6dea6f):**

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
  OPEN: #4, #5, #9, #12, #14, #15, #17, #18.
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

- 2026-06-11 (afternoon) — Finding #19 (user-reported): GET /admin answered
  403 FORBIDDEN — root cause in nucleus pkg/authz: BootstrapAllowList seeds
  /admin/* and casbin keyMatch never extends prefix/* to the bare prefix where
  net/http mounts the canonical redirect. Fixed upstream in nucleus PR #122
  (merged, squash sha 6b3ea75): exact-match /admin row + custom admin_prefix
  mirror in pkg/app + regression tests (bare prefix redirects, /administrator
  stays denied). Full iteration loop ran: architect PASS, code NITS (addressed),
  security PASS, contract PASS (additive), test-runner PASS, CHANGELOG patch
  entry. fleetdesk: temporary policy-row workaround (commit a895063) then
  re-pinned to v0.9.1-0.20260611064010-6b3ea757c461 and workaround removed
  (commit be15965); FINDINGS #19 FIXED, re-verified from the published module.
  Admin login flow verified end-to-end in the browser (admin/fleetdesk-demo
  → panel).

- 2026-06-11 (afternoon) — CI drift-guard flake root-caused and fixed:
  scripts/website/check-coverage.sh dangling-covers check used
  `grep | grep -Fq` under `set -o pipefail` — -q SIGPIPEs the left grep
  (exit 141) and `if !` misreads a successful match as "absent"; bit PR #122
  flagging pkg/storage.Store.Delete spuriously. Fix (drop -q, drain stdin)
  merged in PR #123 (squash sha 85560e5, 2026-06-11); all checks green
  including Website Docs Drift job; branch deleted local+remote.

**Slices completed:** S1 (scaffold + multi-tenant + base models), S2 (list
pages + chrome + dashboard), S3 (Tickets CRUD + alert workflow + React islands).

### In progress

- (none)

### Blocked

- (none)

## Task ladder (full sequence)

- [x] S1: scaffold, git init, first boot, multi-tenant config (2 tenants),
       base models + first migration.
- [x] S2: 7 list pages + chrome partials + dashboard refactor.
- [x] S3: Tickets CRUD + React islands (Vite + go:embed, complete 2026-06-11).
- [ ] S4: sessions + casbin RBAC (admin/operator/viewer replacing anonymous
       rows) + enable CSRF + mitigate findings #15/#17.
       NOTE (security): set cors_origins allow-list BEFORE session cookies land
       — wildcard CORS + credentials would be a cross-tenant leak.
- [ ] S5: tasks/signals/mail/storage.
- [ ] S6: admin/observability/openapi/limits/circuit + finding #14.
- [ ] S7: E2E + docs-truth pass for findings #4/#5/#9/#12.
- [ ] Data Studio Phases 0/A/B/C (Phase 0 = finding #9 ADR decision).

## Files of interest

- ~/GolandProjects/fleetdesk (prototype repo)
- ~/GolandProjects/fleetdesk/FINDINGS.md (friction ledger)
- ~/GolandProjects/fleetdesk/templates/chrome.html (shared chrome partial)
- ~/GolandProjects/fleetdesk/go.mod (nucleus pseudoversion pin)
- pkg/router/context.go (BindForm, BindJSON, c.HTML)
- pkg/app/requestscope.go (tenant scope)
- pkg/admin/login.go (injectHeadMeta helper — PR #120)
- docs/guides/MULTISITE_GUIDE.md (rewritten 2026-06-09)
- internal/cli/new.go (defaultPinnedFrameworkVersion)

## Notes / decisions log

- 2026-06-10 — Prototype #1 = fleetdesk (IoT fleet portal). SSR = Go templates
  + React islands (NOT a Node sidecar): maximizes framework usage. Repo pinned
  to published nucleus, no replace.
- 2026-06-10 — Distilled slices later become the ADR-010 Phase 4 Slice 2/3
  reference apps + harness fixture profiles (v0.9.X).
- 2026-06-10 — Frictions found → small nucleus PRs (prototype is also the
  framework's validation vehicle). Four PRs merged today (#117–#120).
- 2026-06-10 — Preview server (launch.json "fleetdesk", port 8080) dies when
  tooling session recycles — benign, restart with preview_start. nohup
  detached alternative offered to user; pending their call.
- 2026-06-10 — gh REST (pr checks/merge) 401s intermittently; workaround:
  poll via `gh pr view N --json statusCheckRollup`, merge via GraphQL
  mergePullRequest mutation.
- 2026-06-10 — App URLs: http://acme.fleetdesk.localhost:8080/ (and borealis.)
  — needs ≥3 host labels; admin at /admin.
- 2026-06-10 — Finding #17 (login timing oracle, LOW) logged; deferred to S4.
- 2026-06-11 — Dev loop for islands: npm build + go build (bundle embeds at
  compile time); launch.json runs ./app so the binary must be rebuilt before
  preview restarts to pick up island changes.
- 2026-06-11 — Bare /admin gap never bit before because navigation always used
  /admin/ or the login redirect; quickstart-documented bare URL only got
  exercised via the README real-user path. Also: PR #123 (drift-guard flake)
  intentionally skipped the subagent loop — scripts/-only, self-verified 5/5
  deterministic strict runs.
- F-13 (P3, non-blocking): CLAUDE.md §directory-map still says cmd/goframe/;
  actual entry-point is cmd/nucleus/. Fix opportunistically in any docs PR.
