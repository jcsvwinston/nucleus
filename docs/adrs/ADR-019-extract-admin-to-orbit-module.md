# ADR-019: Extract the Admin Panel into "orbit", a Separate Pluggable Module

Reference date: 2026-06-21.
Status: Accepted (Slice 1 + Slice 2 landed; the in-core admin was removed in the
clean break of 2026-06-21 and the panel now ships as the separate `orbit`
module).
Related: [ADR-010](ADR-010-fluent-api-v2-pkg-nucleus.md) (the `Module`/`Runtime`
extension surface orbit mounts through), [ADR-004](ADR-004-casbin-default-deny-mount.md)
(default-deny — orbit must seed its own bootstrap allow-list),
[ADR-013](ADR-013-real-app-readiness.md) (`WithoutDefaults` / reserved module
shape), [ADR-016](ADR-016-admin-api-authn-at-router-edge.md) (admin authn at the
router edge — carries into orbit), [ADR-018](ADR-018-admin-observability-bus-migration.md)
(the observability bus the live view consumes). Closes fleetdesk finding **#9**
(admin SPA not distributed to module consumers) and sets the home for **#31**
(live SQL coverage) and the admin UI/UX work.

## Context

The admin panel is a headline feature, but today it is **welded into the
core**:

- It is special-cased in the boot path — `pkg/app.attachDefaultSubsystems`
  constructs `admin.NewPanel(db, registry, logger, PanelConfig{...})` and
  `app.MountAdmin()` mounts it via `Router.Mount(prefix, Panel.Handler())`. It
  is **not** a `Module`/`Extension`; the `Extension` interface exists but the
  admin does not use it.
- Its React SPA is resolved **from disk at runtime** (`NUCLEUS_ADMIN_UI_DIR`
  env + a repo parent-walk for `pkg/admin/ui/dist`); only a placeholder
  (`ui_fallback/`) is `go:embed`-ed, and `dist` is `.gitignore`-d. So a
  consumer who `go get`s nucleus and runs their app gets the **placeholder**,
  not the real admin, while the quickstart promises `/admin` works
  out-of-the-box. This is finding #9 (HIGH): the promise is broken for every
  published-module consumer.

**Strategic direction (owner, 2026-06-19):** the admin becomes a separate,
pluggable **product — "orbit"** — with its own identity, versioning and release
cadence, not a core-welded panel.

The admin needs **deep in-process runtime introspection** — the model registry
(Data Studio), the observability bus (live SQL/HTTP feed, ADR-018), active
session enumeration, resource/runtime metrics, and the RBAC enforcer. All of
that lives **inside the running application process**. An out-of-process plugin
(the `nucleus-plugin-<binary>` model) cannot see it without reinventing a large
IPC surface; a downloadable release asset is operationally heavy and does not
fit a Go-module product. Therefore orbit must run **in-process, mounted as a Go
module via the ADR-010 extension surface**.

A 2026-06-20 recon confirmed feasibility. Most of the admin's dependencies are
**already public** (`db.DB` + a multi-DB handle map, `model.Registry`,
`authz.Enforcer`, `auth.SessionManager`, `observability.Bus`, `storage.Store`,
`signals.Bus`, `tasks.Inspector`, `mail`). The admin's **private machinery**
(live runtime ring buffers, audit store, feature flags, migration runtime,
tenant context, the `DatabaseAdminAuth` provider) **moves with it** into orbit
and stays internal there. The only thing missing is that a module receives a
`Runtime`, not a hand-wired `PanelConfig` — so a handful of additive `Runtime`
accessors are needed.

## Decision

### 1. Orbit is a separate Go module in its own repository

Orbit lives in its own repo and module path, depending on `nucleus`. It exposes
a `nucleus.ModuleSpec` — e.g. `orbit.Module(orbit.Config{Prefix: "/admin", …})`
— mounted through the builder (`app.Mount(orbit.Module(...))`). This gives it an
independent semver, release and (if desired) license, while reusing the
framework's stable mount contract. Orbit becomes the **second dogfooding
consumer** after fleetdesk: mounting it exercises and hardens the
extension/`Runtime` surface ahead of v1.0 (roadmap Pillar 4).

### 2. Orbit embeds its own SPA — resolving #9 by construction

Orbit `go:embed`s its **own** built `dist`. A consumer who mounts orbit gets the
full admin out-of-the-box, offline, in a single binary, with the UI version
pinned to the orbit module version via `go.mod`. The disk-lookup /
`NUCLEUS_ADMIN_UI_DIR` mechanism is dropped (or kept only as an orbit-internal
dev convenience). Orbit's CI builds `dist` and verifies it is fresh against the
UI source before tagging; the built assets are part of the orbit module tree so
the Go proxy serves them. **#9 ceases to exist**: the admin is distributed as a
normal Go dependency.

### 3. Clean break in the core — no compatibility shim

The core **stops auto-wiring and mounting the admin**. The admin block in
`pkg/app.attachDefaultSubsystems` and `app.MountAdmin()` are removed, and
`pkg/admin` is relocated into orbit. There is **no default-on shim**: the admin
is opt-in, mounted explicitly. This is a behaviour change (the framework no
longer serves `/admin` by default) and is recorded in `CHANGELOG.md` with a
`BREAKING` label, following the pre-`v1.0` ADR-006/008/010 precedent
(single-maintainer repo, no external consumers — clean break is acceptable and
matches the product-separation intent).

### 4. Grow the `Runtime`/extension surface (additive) so a module can host a deep admin

Add to `nucleus.Runtime`:

- `Models() *model.Registry` — Data Studio enumeration + per-model CRUD.
  (`pkg/model` is `stable`; `model.Registry` is leak-free. Today `app.App.Models`
  is private.)
- `Observability() EventBus` — the live SQL/HTTP feed (ADR-018), returned as a
  **narrow first-party interface defined in `pkg/nucleus`**, NOT the concrete
  `*observability.Bus`. See the decision note below.
- `Databases() map[string]*sql.DB` — every configured handle as **stdlib types**,
  for cross-database Data Studio (`Runtime.DB()` returns only the module's default
  handle). The returned map is a **snapshot copy**; mutating it must not affect
  the framework's internal alias registry.

**`Observability()` returns a first-party interface, not `*observability.Bus`
(architect-reviewed 2026-06-20).** `pkg/observability` is `experimental` and its
surface may change before v1.0. Returning the concrete `*observability.Bus` on
the `stable` `Runtime` would force every caller (orbit) to import the
experimental package and recompile-break on each pre-v1.0 change — a layering
violation, and a *stronger* coupling than ADR-010's `openapi.DocumentProvider`
case (an interface, not a concrete type). Instead `pkg/nucleus` defines a narrow
`EventBus` interface exposing only what a consumer needs (`Subscribe(...)`,
`HasSubscribers(kind)`), backed by `*observability.Bus`. This mirrors
`Runtime.Authorizer() *authz.Enforcer` (stable→stable). When `pkg/observability`
promotes to `stable`, the interface can collapse onto the concrete type.

Add to `pkg/auth`:

- `SessionManager.ActiveSessions(ctx) []auth.SessionInfo` — session enumeration,
  backed by the SQL session store's `All`/`AllWithContext`. `auth.SessionInfo` is
  a **new dedicated `stable` type in `pkg/auth`**, deliberately distinct from the
  `experimental` `pkg/observability/hooks.SessionInfo`, so the stable surface does
  not anchor to the experimental one. (Today the admin type-asserts an
  **unexported** iterable store interface.)

Formalise in `pkg/app`:

- `RequestScope` + `RequestScopeFromContext(ctx)` are **already exported**
  (`pkg/app/requestscope.go`) but are not tracked in the contract inventory. The
  work is to add them to the `pkg/app` stable row and rebaseline the freeze — not
  a fresh export. (Today the admin reaches multi-tenant scope via an unexported
  helper; orbit uses these instead.)

RBAC needs **nothing new** — `Runtime.Authorizer()` already exposes the full
enforcer (read + write). Storage, mail, JWT, tasks are already reachable.

`Runtime` is an interface **implemented only by the framework** (ADR-010
explicitly reserves the right to add methods in minor versions), so these
additions do not break module authors. All additions are baseline-additive (no
removals).

### 5. Carry over the security boundary into orbit

Orbit re-applies the existing posture rather than reinventing it:

- **ADR-016** — authenticate `/api/*` at the router edge inside orbit's mount;
  authz per action on top. The `WARN`-when-`Auth==nil` posture moves with it.
- **ADR-004** — orbit seeds its own bootstrap allow-list
  (`authz.BootstrapAllowList`) for its prefix so the framework default-deny gate
  does not 403 the panel's own routes (the existing exact-prefix + wildcard
  rows, finding #19, move with orbit).
- **ADR-018** — orbit consumes the observability bus via `Runtime.Observability()`
  for the live feed.
- The `DatabaseAdminAuth` provider, the `nucleus_admin_users` table, and the
  `__nucleus_admin_*` session-key namespace move into orbit, which owns them as
  its product surface.

### 6. Move, don't rebuild

Slice 2 relocates `pkg/admin` (+ `ui`) into orbit **wholesale** — feature parity
first. Completeness (#31, the Live Runtime Inspector vNext) and the UI/UX-to-100
push are later slices, done inside orbit.

## Implementation phases (slicing)

1. **Slice 1 — core foundation (nucleus, additive).** The `Runtime` accessors
   (`Models`, `Observability`, multi-DB), `SessionManager.ActiveSessions`, and
   public `RequestScope`/`RequestScopeFromContext`. Independently valuable to any
   module; unblocks orbit. Full iteration loop + additive contract rebaseline.
2. **Slice 2 — orbit module + core clean break.** Stand up the orbit repo/module,
   move `pkg/admin` (+ `ui`) into it, wire it through the Module API using the
   Slice-1 accessors, embed its SPA. Remove the built-in admin from the core.
   This is the #9 fix.
3. **Slice 3 — completeness (orbit).** Live SQL beyond `model.CRUD` (#31),
   real-time WebSocket streaming (Live Runtime Inspector vNext), resource depth.
4. **Slice 4 — UI/UX to 100 (orbit).** Audit + polish across every module.

## Contract impact (compliance checklist)

**Slice 1 (additive, nucleus) — no removals, CHANGELOG `Added`:**
- New methods on the `stable` `pkg/nucleus.Runtime` interface: `Models()`,
  `Observability()`, `Databases()`. Framework-implemented-only → additive
  (ADR-010). Rebaseline `contracts/baseline/api_exported_symbols.txt`.
- New `stable` `pkg/nucleus` type `EventBus` (+ any thin `SubscribeOptions` /
  `EventKind` / `Subscription` aliases it needs). Baseline-additive; firewall-clean
  (first-party only).
- New `stable` `pkg/auth` type `SessionInfo` + method `SessionManager.ActiveSessions`.
  Baseline-additive.
- `app.RequestScope` + `app.RequestScopeFromContext` already exported → ADD to the
  `pkg/app` row of `docs/reference/API_CONTRACT_INVENTORY.md`; rebaseline.

**Slice 2 (BREAKING clean break, nucleus + new orbit repo):**
- REMOVE `app.MountAdmin()` (currently `stable`) → CHANGELOG `BREAKING (pkg/app)`;
  update the `pkg/app` row in `API_CONTRACT_INVENTORY.md`; rebaseline.
- REMOVE `App.Admin *admin.Panel` field and the `pkg/admin` package from core →
  same governance.
- Config keys `admin_prefix`, `admin_title` (currently `stable`) move to
  `orbit.Config` → mark `removed` in `CONFIG_KEY_REGISTRY.md` with an orbit
  migration note.
- Pre-v1.0 clean break (single-maintainer, no external consumers): no deprecation
  cycle / DEP / MA artefacts per the ADR-006/008/010 precedent — but the CHANGELOG
  `BREAKING` label and the contract-inventory + config-registry updates are
  mandatory. Route via `contract-guardian` (+ `migration-assistant` to assess)
  before merge.

Also: add an `ADR-019` line to the `docs/adrs/README.md` index, and (Slice 2) the
`scripts/website/check-coverage.sh` admin pages move to orbit's own docs.

## Consequences

**Positive.**
- #9 is resolved by construction; the admin ships as a normal Go dependency.
- The admin becomes an independently-versioned (and potentially separately
  licensed) product.
- The core gets leaner; apps that do not want an admin carry none of it.
- The extension/`Runtime` surface is hardened by a second real consumer before
  v1.0 (validates roadmap Pillar 4).

**Negative / costs.**
- Behaviour change: the admin is no longer auto-on; existing apps must mount
  orbit. (Loud CHANGELOG `BREAKING` entry + migration note.)
- Cross-repo development friction during the extraction (pseudo-versions /
  `replace` directives while Slice 1 lands and orbit catches up).
- Orbit must keep its committed `dist` fresh in CI.
- The new core accessors are permanent stable contract.

**Risks / resolved items.**
- **Stable ↔ experimental coupling — RESOLVED (see §4):** `Runtime.Observability()`
  returns the first-party `nucleus.EventBus` interface (not the concrete
  `*observability.Bus`), and `SessionManager.ActiveSessions` returns the dedicated
  `auth.SessionInfo` (not the experimental `hooks.SessionInfo`) — so no stable
  surface anchors to an experimental package.
- **Firewall:** the multi-DB accessor returns `map[string]*sql.DB` (stdlib), not
  `map[string]*db.DB` — leak-free, mirroring `Runtime.DB()`.
- `RequestScope` is already exported with a string-only shape; Slice 1 only
  formalises it in the contract inventory (no shape change).
- The move must keep `contracts/` + all tests green; the clean break must be
  unmistakable in docs and CHANGELOG (enumerated under Contract impact above).

## Status note

This ADR is `Accepted`. Slices 1 and 2 landed 2026-06-21: the Runtime accessors
(`Models`, `Observability`, `DatabaseHandle`/`DatabaseHandles`) and
`Router.Mount` shipped in Slice 1; the in-core admin panel was removed in PR #155
(Slice 2) and the observability subsystem in PR #159. Slices 3 and 4
(completeness and UI/UX polish) are orbit-side work, tracked in the orbit
repository (github.com/jcsvwinston/orbit).
