# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

Build prototype #1 — **fleetdesk**: a full-stack, multi-tenant MVC application
(IoT SIM/device fleet portal) with server-rendered UI + React islands, pinned
to the published nucleus v0.9.0, exercising **every** Nucleus capability with
maximal use of the framework's exposed methods and readable, elegant code.

## Scope

- in: new repo `~/GolandProjects/fleetdesk` (module `github.com/jcsvwinston/fleetdesk`),
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

### Done
- 2026-06-10 — Design decisions taken (domain, SSR+islands strategy, own repo).

### In progress
- Slice 1: scaffold `fleetdesk`, git init, first boot, multi-tenant config
  skeleton (2 tenants), base models + first migration.

### Blocked
- (none)

## Files of interest

- ~/GolandProjects/fleetdesk (the prototype repo)
- pkg/router/context.go:377 (c.HTML), pkg/app/requestscope.go (tenant scope)
- docs/guides/MULTISITE_GUIDE.md (rewritten 2026-06-09 — config truth)
- internal/cli/new.go (scaffold pins v0.9.0)

## Notes / decisions log

- 2026-06-10 — Prototype #1 = fleetdesk (IoT fleet portal). SSR = Go templates
  + React islands (NOT a Node sidecar): maximizes framework usage and satisfies
  "server-rendered React" literally. Repo pinned to published v0.9.0, no replace.
- 2026-06-10 — Distilled slices of this prototype later become the ADR-010
  Phase 4 Slice 2/3 reference apps + harness fixture profiles (v0.9.X).
- 2026-06-10 — Frictions found in nucleus → small PRs (the prototype is also
  the framework's validation vehicle).
