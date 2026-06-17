# Iteration Archive — fleetdesk prototype (S7 close)

> Archived 2026-06-17 by `session-curator`.
> All acceptance-criteria boxes satisfied; iteration COMPLETE (S1–S7b).

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

## Acceptance criteria (framework-coverage matrix) — ALL SATISFIED

- [x] `pkg/nucleus` fluent builder: `New().FromConfigFile(...)` + `Mount(Module[C])`
      with typed module config (`default:`/`validate:` tags), `Runtime` hooks,
      `WithOpenAPI`, `.Start()` terminal.
- [x] Multi-tenant: `multitenant.*` config (header resolver to start), ≥2 seed
      tenants, `RequestScope`/`TenantFromContext`, tenant-scoped data isolation,
      admin auto-filter.
- [x] `pkg/model`: registered models (BaseModel, FKs, indexes, tenant field),
      CRUD with `QueryOpts` (search/filters/multi-column order_by), migrations
      via `nucleus makemigrations`/`migrate`.
- [x] SSR UI: `c.HTML` pages from `App.Templates` + React islands (Vite,
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
- [x] `pkg/validate`: form/API validation incl. one custom rule via `RegisterRule`
      (`validate.RegisterRule("iccid_luhn",…)` in fleetops).
- [x] `pkg/errors`: DomainError-based handlers end to end
      (`pkg/errors.DomainError` via `platform.ErrStatus`).
- [x] Router: groups, rate limiting (`RateLimitMiddleware` scopes), CORS
      config keys, `Resource` REST, `FromHTTP`.
- [x] `pkg/circuit`: breaker around a simulated carrier API call.
- [x] OpenAPI served (`WithOpenAPI` + `nucleus openapi`).
- [x] CLI exercised: `new`, `generate resource`, `makemigrations`, `migrate`,
      `doctor`, `config`, `openapi`, `serve` — all green on a fresh `/tmp` scaffold
      (real-user path, no replace) AND against fleetdesk itself.
- [x] E2E smoke green (12/12 sub-tests: anon/gated, session+CSRF, per-role RBAC
      incl. deny-override, inactive+bad-cred rejection, tenant isolation, JWT API
      anon→401/bearer→200/viewer-write→403/cross-tenant→403); README documents
      the coverage matrix; zero `replace` confirmed.

## Final status: COMPLETE (2026-06-17)

All slices done: S1, S2, S3, S4, S5, S6, S7, S7b.
Nucleus fixes landed: PRs #117, #118, #119, #120, #122, #123, #125, #126, #127, #129, #131.
Final fleetdesk commit: 6c09cc0 ("feat(s7): close the prototype — JWT API auth, E2E smoke, coverage matrix").
Nucleus pin: v0.9.1-0.20260616174301-084a4b5689ca (no replace directive).
Repo: local-only, no remote.

## Complete Done log

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
- 2026-06-17 — PR #131 (084a4b5): admin live SQL feed now consumes the
  observability bus (`Panel.ConsumeObservability`). getCRUD skips redundant
  per-CRUD observer when bus is connected. Drain goroutine stops cleanly via
  done + sync.Once. ADR-018 records the transition. Clean under -race x3.
  Fixes finding root cause; level-2 (#31) deferred by user choice.

**Fleetdesk prototype slices:**

- S1 (2026-06-10): scaffold + git init + first boot + multi-tenant config (2 tenants) + base models + first migration.
- S2 (2026-06-10): 7 server-rendered list pages + chrome partials + dashboard refactor.
- S3 (2026-06-11): Tickets full CRUD + React islands (Vite + go:embed, self-contained binary).
- S4 (2026-06-14): sessions + casbin RBAC (admin/operator/viewer) + CSRF + CORS.
  - S4a: per-tenant staff login + server-side sessions (e81e87e).
  - S4b: casbin RBAC + deny-override + /access inspector (09878b9).
  - S4c: CSRF (two-layer) + CORS allow-list + Chrome view-model refactor (2dbbe82).
- S5 (2026-06-15): tasks/signals/mail/storage — ops console, usage rollup, CSV export (4ce01df).
- S6 (2026-06-17): admin/observability/openapi/limits/circuit.
  - Rate limit 600/min/IP + burst 30 + by_route (nucleus.yml).
  - CarrierClient + pkg/circuit.Breaker (3-failure trip, 15s cooldown) (15210ce).
  - OpenAPI: WithOpenAPI + nucleus openapi, bit-identical 100,695-byte output (a4ca8b3).
  - /healthz + /metrics verified live (3 DBs + storage, Prometheus + otel).
  - Nucleus re-pinned to 084a4b5.
- S7 (2026-06-17): prototype closure (6c09cc0):
  - CLI exercised end-to-end on fresh /tmp scaffold + against fleetdesk.
  - E2E smoke: 12/12 sub-tests green (all roles, CSRF, tenant isolation, JWT API).
  - README rewritten with framework-coverage matrix + demo logins.
  - Zero `replace` confirmed.
- S7b (2026-06-17): security fix — `/api/*` was unauthenticated (folded into 6c09cc0):
  - `internal/apiauth`: POST /api/token mints HS256 JWT; timing-equalized credential check.
  - `platform.APIAuth` bearer middleware on all 4 /api/* modules.
  - security-auditor P1s fixed; catalog API read-only; /api/usage/summary `days` clamped [1,365].
  - Findings #32 (Runtime.JWT()), #33 (openapi security schemes), #34 (reachability-row footgun) logged.

**Findings status (final):**
- FIXED (9): #11, #13, #15, #16, #17, #19, #20, #21, #28
- OPEN — v0.9.x PR candidates: #18, #24, #25, #26, #27 (HIGH), #29, #30, #32, #33, #34
- OPEN — deferred/architectural: #4, #5, #9, #12, #14 (mitigated), #22, #23, #31 (level-2 deferred)

## Files of interest (final state)

- ~/GolandProjects/fleetdesk/ (entire prototype repo, local-only)
- ~/GolandProjects/fleetdesk/FINDINGS.md (friction ledger)
- ~/GolandProjects/fleetdesk/go.mod (nucleus pin v0.9.1-0.20260616174301-084a4b5689ca)
- ~/GolandProjects/fleetdesk/e2e_smoke_test.go (build tag `e2e`; 12 sub-tests)
- ~/GolandProjects/fleetdesk/internal/apiauth/ (JWT issuance + bearer middleware)
- ~/GolandProjects/fleetdesk/internal/platform/ (Authenticate, APIAuth, ErrStatus)
- ~/GolandProjects/fleetdesk/internal/contracts/ (openapi.go + schemas.go)
- ~/GolandProjects/fleetdesk/internal/connectivity/carrier.go (CarrierClient + breaker)
- ~/GolandProjects/fleetdesk/README.md (coverage matrix + demo logins)
- Nucleus main: pkg/admin/live.go, pkg/auth/runtime.go, pkg/router/context.go

## Key decisions preserved

- Demo logins: admin@acme.example / ops@acme.example / viewer@acme.example,
  password "fleetdesk"; eve@acme.example is a seeded inactive account.
  Admin panel: admin / fleetdesk-demo.
- API: POST /api/token → bearer JWT; token subject is tenant-scoped (tenant/email).
- App URLs need ≥3 host labels (acme.fleetdesk.localhost:8080). Port 8080 may be
  occupied by Docker Desktop; use NUCLEUS_PORT=18080 for CLI probes.
- `nucleus routes` only lists framework routes, not module-registered ones (CLI
  does not run main.go Mount calls) — minor observation, not a formal finding.
- finding #31 (direct *sql.DB queries invisible in live feed) deferred by user;
  needs database/sql/driver wrapper + dedup with model.CRUD observer — ADR territory.
- v0.9.0 published (tag 2026-06-09, commit 929234e, on the Go proxy);
  9 S1–S6 nucleus fixes live on main ahead of the tag (no patch release cut yet).
