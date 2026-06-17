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
- [x] `pkg/admin`: panel mounted with `RBACEnforcer`, multi-tenant selector,
      audit log, feature flags, live view, exports/imports (panel cabled by
      framework; PR #131 added the application-SQL feed via the observability bus).
- [x] `pkg/tasks`: Manager handlers (usage rollup, report generation),
      Scheduler (daily), Inspector wired into admin jobs view.
- [x] `pkg/signals`: Bus events (e.g. `sim.activated`) → mail + audit, EmitAsync.
- [x] `pkg/mail`: smtp (dev: mailhog/noop) alert + welcome templates.
- [x] `pkg/storage`: local store; report exports via `Put`/`SignedURL`.
- [x] `pkg/observe` + `pkg/health`: slog, Prometheus `/metrics`, `/healthz`
      with custom checks (framework auto-probes verified live; custom-from-module
      probes are a framework follow-up).
- [ ] `pkg/validate`: form/API validation incl. one custom rule via `RegisterRule`.
- [ ] `pkg/errors`: DomainError-based handlers end to end.
- [x] Router: groups, rate limiting (`RateLimitMiddleware` scopes), CORS
      config keys, `Resource` REST, `FromHTTP`.
- [x] `pkg/circuit`: breaker around a simulated carrier API call.
- [x] OpenAPI served (`WithOpenAPI` + `nucleus openapi`).
- [ ] CLI exercised: `new`, `generate resource`, `makemigrations`, `migrate`,
      `doctor`, `config`, `openapi`, `serve`.
- [ ] E2E smoke green; README documents the coverage matrix; zero `replace`.

## Status

### Done (as of 2026-06-17)

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
- 2026-06-15 — PR #129 (a02c96e): `Runtime.Mailer()` + `Runtime.Storage()`
  — module access to framework-managed mail sender and object store; same
  degrade-to-nil posture as Session/Authorizer; nil under WithoutDefaults.
  tasks/signals intentionally NOT added (standalone, module-instantiable).
  Baseline +2 additive. ADR-010 service-surface amendment;
  API_CONTRACT_INVENTORY + CHANGELOG updated. Fixes fleetdesk finding #28.
  Loop: architect PASS (ADR amendment), code NITS fixed (vacuous noop-identity
  test → pointer sentinel, %p→%v, godoc nil-first), contract PASS, tests green.
- 2026-06-17 — PR #131 (084a4b5): admin live SQL feed now consumes the
  observability bus (`Panel.ConsumeObservability`). The admin live view's SQL
  feed previously only captured the admin's own Data Studio CRUDs (per-CRUD
  observer in getCRUD); now it subscribes to the application's observability
  bus (KindSQLStatement) which carries EVERY model.CRUD query across the whole
  app — REST resource handlers, fleetdesk's ticket CRUD via platform.Resource
  — so app SQL surfaces in the panel. getCRUD skips the now-redundant per-CRUD
  observer when the bus is connected (no double-recording). Drain goroutine
  stops cleanly via a `done` channel + sync.Once (Subscription.Cancel does NOT
  close the channel by design) and drains buffered events to honour pool Release
  obligations. Clean under -race x3. ADR-018 records the transition (Phase 3
  admin/agent will own this); CHANGELOG minor. Root cause of "Live data muerto"
  finding diagnosed and fixed at level 1.
  Loop: code-reviewer CHANGES_REQUESTED → blockers fixed (goroutine leak via
  `done`+Once; observCancel race); architect PASS → WARN fixed (stale app.go
  comment) + ADR-018; changelog minor; contract freeze green; -race x3 clean.

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

- 2026-06-15 — PR #129 (nucleus a02c96e): `Runtime.Mailer()` + `Runtime.Storage()`
  — module access to framework-managed mail sender and object store; same
  degrade-to-nil posture as Session/Authorizer; nil under WithoutDefaults.
  tasks/signals intentionally NOT added (standalone, module-instantiable).
  Baseline +2 additive. ADR-010 service-surface amendment;
  API_CONTRACT_INVENTORY + CHANGELOG updated. Fixes fleetdesk finding #28.
  Loop: architect PASS (ADR amendment), code NITS fixed (vacuous noop-identity
  test → pointer sentinel, %p→%v, godoc nil-first), contract PASS, tests green.

- 2026-06-15 — nucleus re-pinned in fleetdesk to
  v0.9.1-0.20260615064439-a02c96e33fa9 (folded into the S5 commit go.mod).

- 2026-06-15 — S5 shipped (fleetdesk commit 4ce01df "feat(ops): S5 — services
  layer (signals, tasks, mail, storage)"):
  - internal/ops module: signals.Bus + tasks worker (memory provider, no Redis)
    on a cancellable context + @daily scheduler + inspector; package-level
    Emit/EmitAsync/Inspector/Schedules/EnqueueRollup accessors (singleton set
    in OnStart — the fluent surface gives modules no shared state).
  - signals: sim.activated emitted (sync) from fleetops.activate (refactored
    transition with an onSuccess hook) → audit row in the tenant DB (carried
    in-process on the event) + enqueues welcome mail task. alert.changed shape
    defined.
  - tasks: mail.welcome/mail.alert handlers (rt.Mailer); usage.rollup (@daily
    + on-demand via EnqueueRollup); worker stopped cleanly in OnShutdown.
  - mail: welcome/alert templates through rt.Mailer() (noop driver in dev —
    logs the send).
  - storage: usage CSV export via rt.Storage().Put + SignedURL (cloud) with a
    Get-streaming fallback for the local driver, behind admin-only
    /jobs/download (reportKeyPrefix-bounded against traversal).
  - AuditLog model (append-only, NO admin: tags so panel cannot expose
    soft-delete) + tenant migration 20260615120001_create_audit_logs_table
    (applied live to acme/borealis).
  - webui: admin-only /jobs ops console (worker/schedule snapshot via
    Inspector + ops.Schedules, run-rollup, export-CSV); Jobs nav gated to
    admin; storage/ gitignored; web/package.json build now `touch dist/.gitkeep`.
  - Verified live: activate→audit→welcome mail, rollup, export+download
    round-trip, CSV formula-injection guard (=cmd → '=cmd).
  - Reviews: code CHANGES_REQUESTED + security WARN — all addressed (worker
    goroutine leak → cancellable ctx; CSV injection → csvSafe + carrier_code
    alphanum; audit soft-delete → admin tags removed; HandleFunc errors
    surfaced; /jobs reachability scoped to exact POST paths; ctx threaded into
    synthetic tenant request; truncated download logged).

- 2026-06-15 — NEW framework findings: #29 (Runtime has no DBForTenant /
  tenant-enumeration for background workers — fleetdesk workaround: synthetic
  request with Host=<tenant>.fleetdesk.localhost; DB-bound jobs run
  synchronously in requests), #30 (pkg/storage local driver does not support
  SignedURL — fleetdesk falls back to Get-streaming).

- 2026-06-17 — fleetdesk re-pinned to nucleus v0.9.1-0.20260616174301-084a4b5689ca
  (commit 05c6f8b in fleetdesk): level-1 admin live SQL feed available, no
  code change in fleetdesk; wired automatically by app.New.

- 2026-06-17 — finding #31 logged (fleetdesk commit f6638e2): direct *sql.DB
  queries — fleetdesk's dashboard/lists/ops/audit — still invisible in the live
  SQL feed. Level-2 deferred by user choice; needs database/sql/driver wrapper
  in pkg/db plus dedup with model.CRUD observer; ADR territory.

- 2026-06-17 — S6 shipped (fleetdesk commit 15210ce "feat(s6): rate limit +
  carrier circuit breaker + body limit"):
  - nucleus.yml: rate_limit_requests 0→600/min/IP, burst 30, by_route true.
  - internal/connectivity/carrier.go: simulated CarrierClient + pkg/circuit.Breaker
    (3 failures trip, 15s cooldown, 1 half-open probe); sharedCarrier singleton.
  - internal/fleetops sim_lifecycle.activate: pre-flight carrier provisioning
    before the local SIM state flip; returns 502 on failure / 503
    (ErrCarrierUnreachable) when breaker is open.
  - internal/webui/bodylimit.go: 2 MiB body cap on POST/PUT/PATCH via
    Module.Middleware (fleetdesk workaround for finding #14; framework has no
    mux-level body limit, only BindForm's 10 MiB).
  - Tests: TestCarrierBreakerTripsAfterRepeatedFailures + steady-state PASS.

- 2026-06-17 — S6 OpenAPI shipped (fleetdesk commit a4ca8b3 "feat(openapi):
  /openapi.json + nucleus openapi CLI export"):
  - internal/contracts/openapi.go + schemas.go: idiomatic location (matches CLI
    exporter template). NewDocument() used by BOTH the runtime endpoint
    (WithOpenAPI) AND the `nucleus openapi --project .` CLI — no drift.
  - 15 paths across 6 component schemas: 5 resources × CRUD (fleets/devices/
    sims/subscriptions/tickets) + /api/usage (read-only) + 3 actions
    (/sims/{id}/activate, /suspend, /usage/summary) with new 502/503 carrier
    responses.
  - main.go: WithOpenAPI("/openapi.json", openapi.DocumentProvider(contracts.NewDocument)).
  - rbac_policy.csv: anonymous read for /openapi.json, /metrics, /healthz.
  - Verified live on port 18080: runtime endpoint 200 + 100,695 bytes; CLI
    export 100,695 bytes (bit-identical).

- 2026-06-17 — /healthz + /metrics verified live on port 18080: /healthz lists
  3 DBs (default + tenant_acme + tenant_borealis) + storage, all healthy;
  /metrics serves Prometheus output with otel db pool gauges.

**Slices completed:** S1, S2, S3, S4, S5, S6.

**Findings status:**
- FIXED: #11, #13, #15, #16, #17, #19, #20, #21, #28
- OPEN: #4, #5, #9, #12, #14 (mitigated in app), #18, #22, #23, #24, #25,
  #26, #27, #29, #30, #31
  (framework-friction #24–#27 are v0.9.x PR candidates; #27 HIGH;
   #29 DBForTenant + #30 local SignedURL + #31 direct-SQL visibility are
   v0.9.x candidates; #31 is level-2 deferred by user choice)

### In progress

- (none)

### Blocked

- (none — port 8080 occupied by Docker Desktop PID 1926 is an environment
  annoyance; all live verification done on NUCLEUS_PORT=18080)

## Task ladder (full sequence)

- [x] S1: scaffold, git init, first boot, multi-tenant config (2 tenants),
       base models + first migration.
- [x] S2: 7 list pages + chrome partials + dashboard refactor.
- [x] S3: Tickets CRUD + React islands (Vite + go:embed, complete 2026-06-11).
- [x] S4: sessions + casbin RBAC (admin/operator/viewer) + CSRF + CORS +
       findings #15/#17 (complete 2026-06-14).
- [x] S5: tasks/signals/mail/storage (complete 2026-06-15).
       pkg/tasks: usage rollup + report generation handlers, Scheduler (daily),
       Inspector wired into admin jobs view.
       pkg/signals: Bus events (sim.activated → mail + audit, EmitAsync).
       pkg/mail: smtp dev mailhog/noop; alert + welcome templates.
       pkg/storage: local store; report exports via Put/SignedURL.
- [x] S6: admin/observability/openapi/limits/circuit + finding #14
       (complete 2026-06-17).
       pkg/admin: live SQL feed via observability bus (PR #131).
       pkg/observe + pkg/health: /metrics + /healthz verified live.
       Router: rate_limit_requests=600/min/IP, burst 30, by_route=true.
       pkg/circuit: CarrierClient + Breaker (3-failure trip, 15s cooldown).
       OpenAPI: WithOpenAPI + nucleus openapi (bit-identical output, 100,695 bytes).
       Body limit: 2 MiB mux-level workaround via Module.Middleware.
- [ ] S7: E2E smoke green + README documents the coverage matrix + zero `replace`;
       CLI fully exercised (`new`, `generate resource`, `makemigrations`,
       `migrate`, `doctor`, `config`, `openapi`, `serve`); pending browser
       verification of /jobs, /access, live admin view once port 8080 is freed.
- [ ] Data Studio Phases 0/A/B/C (Phase 0 = finding #9 ADR decision).

## Files of interest

- ~/GolandProjects/fleetdesk (prototype repo)
- ~/GolandProjects/fleetdesk/FINDINGS.md (friction ledger; OPEN: #4 #5 #9 #12
  #14 #18 #22 #23 #24 #25 #26 #27 #29 #30 #31)
- ~/GolandProjects/fleetdesk/go.mod (nucleus pin: v0.9.1-0.20260616174301-084a4b5689ca)
- ~/GolandProjects/fleetdesk/internal/contracts/ (openapi.go + schemas.go —
  runtime + CLI source of truth)
- ~/GolandProjects/fleetdesk/internal/connectivity/carrier.go (CarrierClient + breaker)
- ~/GolandProjects/fleetdesk/internal/webui/bodylimit.go (2 MiB body cap)
- ~/GolandProjects/fleetdesk/nucleus.yml (rate_limit_requests=600, by_route=true)
- ~/GolandProjects/fleetdesk/main.go (WithOpenAPI wiring)
- ~/GolandProjects/fleetdesk/internal/ops/ (ops.go bus+worker+scheduler,
  mail.go, rollup.go finding-#29 workaround, report.go storage+csvSafe)
- ~/GolandProjects/fleetdesk/internal/webui/ops_views.go (/jobs console + download)
- ~/GolandProjects/fleetdesk/internal/models/audit_log.go
- ~/GolandProjects/fleetdesk/internal/webui/auth.go (gate middleware + login)
- ~/GolandProjects/fleetdesk/internal/webui/authz.go (requireRole/requirePerm)
- ~/GolandProjects/fleetdesk/internal/webui/csrf.go (two-layer CSRF)
- ~/GolandProjects/fleetdesk/internal/webui/chrome.go (Chrome view-model)
- ~/GolandProjects/fleetdesk/internal/webui/access.go (/access inspector)
- ~/GolandProjects/fleetdesk/internal/platform/session.go (session + CSRF helpers)
- ~/GolandProjects/fleetdesk/rbac_policy.csv (anon reachability + role policies
  with deny-override; /openapi.json + /metrics + /healthz added anonymous)
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
  — needs ≥3 host labels; admin at /admin. Dev override: NUCLEUS_PORT=18080.
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
- 2026-06-15 — S5 ops mail driver is noop in dev (welcome/alert sends are
  logged, not delivered). storage writes under ./storage/<tenant>/reports/
  (gitignored). Dev loop unchanged: npm --prefix web run build THEN
  go build -o app . before preview_start.
- 2026-06-15 — finding #29 workaround: background workers that need a
  tenant DB synthesize an http.Request with Host=<tenant>.fleetdesk.localhost
  so nucleus resolves the tenant scope. Real fix requires Runtime.DBForTenant
  or tenant enumeration API (v0.9.x).
- 2026-06-15 — finding #30 workaround: local storage driver returns
  ErrNotSupported on SignedURL; /jobs/download falls back to Get-streaming
  through the response writer. Cloud driver (S3/GCS) uses SignedURL directly.
- 2026-06-17 — finding #31: admin live SQL feed (PR #131) only captures
  model.CRUD queries (those that go through the observability bus). Direct
  *sql.DB queries (dashboard aggregates, list queries, ops/audit writes) are
  still invisible. Level-2 fix deferred by user: needs database/sql/driver
  wrapper in pkg/db + dedup logic with the existing bus events; ADR territory.
- 2026-06-17 — Live verification on port 18080 (NUCLEUS_PORT override):
  /healthz 200 (3 DBs + storage), /metrics 200 (Prometheus + otel gauges),
  /openapi.json 200 (100,695 bytes, bit-identical with CLI export). Real browser
  verification of subdomain-routed pages (/jobs, /access, admin live view)
  pending until port 8080 is freed from Docker Desktop (PID 1926).
- 2026-06-17 — Carrier breaker demo: SetFailureRate(1.0) to trip; breaker
  resets after 15s cooldown, 1 half-open probe allowed. ProvisionSIM(ctx, iccid)
  is the pre-flight call in activate; returns 502 on failure, 503 when open.
