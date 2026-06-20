# Iteration Archive — Orbit extraction foundation (ADR-019 + Slice 1 + Router.Mount)

> Archived: 2026-06-20.
> Status: COMPLETE (nucleus PRs #148–#152 merged; orbit repo scaffolded and
> integrated). All nucleus-side prerequisites for the orbit extraction are done.
> Remaining work is orbit-side (Slice 2 onwards).

## Goal

Extract the admin panel from the nucleus core into **orbit** — a separate,
pluggable Go module (`github.com/jcsvwinths/orbit`) that mounts itself into a
running nucleus application via the public extension API. This keeps the core
framework lean, lets orbit evolve on its own release cadence, and serves as
the second dogfooding consumer of the Runtime accessor surface (first:
fleetdesk). Decided via ADR-019 (Proposed → Accepted at the clean break in
Slice 2.4).

## Why this iteration

Finding #9 in the fleetdesk FINDINGS.md (no hosted admin SPA distribution)
was identified as requiring an ADR before any coding. ADR-019 was that
decision. The orbit extraction is the owner-chosen active line of work; all
open v0.9.x friction candidates (#23, #34, #14, #29, #30, #18, #25) are
deferred until after Slice 2.4.

---

## Nucleus side — PRs #148–#152 (all merged → main, 2026-06-20)

### PR #148 — ADR-019 (`d85f3b1`)

**Commit subject:** `docs(adr): ADR-019 — extract the admin panel into "orbit"
(pluggable module) (#148)`

- `docs/adrs/ADR-019.md` written and merged. Status: Proposed (flips to
  Accepted at Slice 2.4 clean break).
- ADR index (`docs/adrs/README.md` or equivalent) backfilled for ADRs 015–019.
- Establishes the architecture: orbit is a standalone Go module; it consumes
  the nucleus Runtime accessor API (`rt.Models()`, `rt.Databases()`,
  `rt.Session().ActiveSessions()`, `rt.Authorizer()`,
  `rt.Observability()`, `rt.Storage()`, `app.RequestScopeFromContext`) and
  mounts via `nucleus.Router.Mount(pattern, http.Handler)`. Admin-specific
  symbols (`app.MountAdmin()`, `App.Admin`, config keys `admin_prefix`,
  `admin_title`) will be removed from nucleus in Slice 2.4 (BREAKING,
  guarded by deprecation notice).

### PR #149 — orbit Slice 1a (`72f95b4`)

**Commit subject:** `feat(nucleus): Runtime.Models() + Runtime.Databases()
accessors (ADR-019 orbit Slice 1a) (#149)`

**New stable symbols (additive):**

- `Runtime.Models() *model.Registry` — returns the application model registry.
- `Runtime.Databases() map[string]*sql.DB` — returns all registered database
  handles keyed by name (copy; mutations do not affect the live registry).
- `app.RequestScope` struct + `app.RequestScopeFromContext(ctx) *RequestScope`
  — per-request scope accessor; contract-tracked as part of this slice.

**Contract baseline delta:** additive only, 0 removals.
`contract-guardian` verdict: ADDITIVE-OK/minor.

### PR #150 — orbit Slice 1b (`2c7fa28`)

**Commit subject:** `feat(auth): SessionManager.ActiveSessions + SessionInfo
(ADR-019 orbit Slice 1b) (#150)`

**New stable symbols (additive):**

- `auth.SessionInfo` struct — snapshot of a live session
  (`UserID`, `Role`, `ExpiresAt`, `IP`, `UserAgent`).
- `(*auth.SessionManager).ActiveSessions(ctx context.Context) ([]SessionInfo, error)`
  — lists currently active sessions for admin introspection.
- `auth.ErrSessionStoreNotIterable` — sentinel returned when the backing store
  does not support enumeration.
- `auth.ErrNilSessionManager` — sentinel returned on a nil receiver call.

**Contract baseline delta:** additive only, 0 removals.
`contract-guardian` verdict: ADDITIVE-OK/minor.

### PR #151 — orbit Slice 1c (`481db78`)

**Commit subject:** `feat(nucleus): Runtime.Observability() + first-party
EventBus (ADR-019 orbit Slice 1c) (#151)`

**New stable symbols (additive):**

- `Runtime.Observability() EventBus` — returns the application-level event bus.
- `nucleus.EventBus` interface — A2 adapter; the internal `pkg/observability`
  package is not leaked through the public surface.
- `nucleus.SQLEvent` struct — event payload emitted on SQL query execution.
- `nucleus.HTTPEvent` struct — event payload emitted on HTTP request
  completion.

**Contract baseline delta:** additive only, 0 removals.
`contract-guardian` verdict: ADDITIVE-OK/minor.

### PR #152 — orbit Slice 2 prereq / Router.Mount (`daa6706`)

**Commit subject:** `feat(nucleus): Router.Mount(pattern, http.Handler) —
mount a sub-handler subtree (ADR-019 orbit Slice 2) (#152)`

**New stable symbols (additive):**

- `nucleus.Router.Mount(pattern string, h http.Handler)` — mounts an
  `http.Handler` subtree at the given pattern prefix, allowing orbit (or any
  pluggable module) to attach its full handler tree to the host application
  router at runtime.

**Contract baseline delta:** additive only, 0 removals.
`contract-guardian` verdict: ADDITIVE-OK/minor.

---

## Orbit repo — scaffold (`9cce16f`, 2026-06-20)

**Repository:** https://github.com/jcsvwinston/orbit (PRIVATE)
**Local clone:** `~/GolandProjects/orbit`
**Module path:** `github.com/jcsvwinston/orbit`
**HEAD:** `9cce16f`
**Nucleus pin:** `v0.9.1-0.20260620081822-481db78d3349` (= `481db78`, Slice 1c)

### What the scaffold delivers

- `orbit.Module(orbit.Config{Prefix string})` — entry point; returns a value
  that mounts itself into the host nucleus app via the Module API.
- A `<prefix>/_orbit/health` probe handler — liveness check; verifiable
  immediately after mount.
- Builds and tests green against the pinned nucleus pseudoversion.

### Pending re-pin

The scaffold pins nucleus at `481db78` (Slice 1c, before `Router.Mount`). It
must be re-pinned to `daa6706` before Slice 2.2 begins so `Router.Mount` is
available to the orbit module.

---

## Status after this foundation

- **All nucleus-side prerequisites for orbit are merged.** Nothing further
  is needed in the nucleus repo before orbit Slice 2.2 can proceed.
- **orbit repo exists, builds, and has a health probe.** It is a private repo
  at the coordinates above; the local clone is at `~/GolandProjects/orbit`.
- **Remaining work is entirely orbit-side** (Slices 2.2, 2.3, 2.4).

---

## Slice 2 plan (orbit-side, not yet started)

### Slice 2.2 — Move `pkg/admin` into orbit (the big move)

1. Re-pin orbit to nucleus `daa6706` (gains `Router.Mount`).
2. Copy `pkg/admin` (and `ui/`) from nucleus into orbit as the module's
   internal panel implementation.
3. Adapt panel construction: replace the hand-wired `admin.PanelConfig` with
   the Runtime accessor calls:
   - `rt.Models()` — model registry
   - `rt.Databases()` — database handles
   - `rt.Session().ActiveSessions(ctx)` — session introspection
   - `rt.Authorizer()` — permission check
   - `rt.Observability()` — event bus
   - `rt.Storage()` — file/blob access
   - `app.RequestScopeFromContext(ctx)` — per-request scope
4. Mount the panel via `r.Mount("/", panel.Handler())` using the new
   `Router.Mount` method (PR #152).
5. Seed orbit's own bootstrap allow-list (as per ADR-004 / fleetdesk finding
   #19 pattern).

### Slice 2.3 — Move `ui` SPA + `go:embed`

Move the admin SPA assets into orbit and wire them via `go:embed`. This is
the change that closes fleetdesk finding #9 (no hosted admin SPA).

### Slice 2.4 — Clean break in nucleus (BREAKING)

- Remove `app.MountAdmin()` and `App.Admin` from nucleus (breaking removals).
- Remove config keys `admin_prefix` and `admin_title` from nucleus (these
  move to `orbit.Config`).
- Flip ADR-019 status from Proposed to Accepted.
- Coordinate via `contract-guardian` + `migration-assistant` with a
  deprecation notice in the preceding minor release.

---

## Deferred (after orbit Slice 2.4)

The following v0.9.x friction candidates remain open but are explicitly
deferred until the orbit clean break ships:

- **#23 HIGH** — Global default-deny vs module-middleware order.
- **#34** — Anonymous reachability-row footgun (pre-authz identity hook).
- **#14** — No mux-level body cap (`http.MaxBytesReader`).
- **#29** — `Runtime` has no `DBForTenant` / tenant-enumeration for background workers.
- **#30** — `pkg/storage` local driver does not support `SignedURL`.
- **#18** — fluent Router has no `Router.Static(prefix, fs.FS)`.
- **#25** — keyMatch prefix-only footgun.

---

## Files of interest

- `docs/adrs/ADR-019.md` (architectural decision; Proposed)
- `pkg/nucleus/runtime.go` (`Runtime.Models`, `Runtime.Databases`,
  `Runtime.Observability` — Slices 1a/1c)
- `pkg/nucleus/eventbus.go` (`EventBus`, `SQLEvent`, `HTTPEvent` — Slice 1c)
- `pkg/auth/session.go` (`SessionInfo`, `ActiveSessions`,
  `ErrSessionStoreNotIterable`, `ErrNilSessionManager` — Slice 1b)
- `pkg/router/router.go` (`Router.Mount` — Slice 2 prereq)
- `pkg/app/scope.go` (`RequestScope`, `RequestScopeFromContext` — Slice 1a)
- `pkg/admin/` (move source for Slice 2.2)
- `contracts/baseline/api_exported_symbols.txt` (updated through each slice;
  additive only)
- `CHANGELOG.md` (Unreleased entries for each slice)
- `docs/reference/API_CONTRACT_INVENTORY.md` (updated through each slice)
- `~/GolandProjects/orbit` (orbit repo root; HEAD `9cce16f`)
- `~/GolandProjects/orbit/go.mod` (nucleus pin; to be bumped to `daa6706`)
