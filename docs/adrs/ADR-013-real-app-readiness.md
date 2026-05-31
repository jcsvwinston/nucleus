# ADR-013: Real-App Readiness Decisions

- **Status:** Accepted
- **Date:** 2026-05-31
- **Deciders:** Core maintainers
- **Supersedes:** none
- **Related:** ADR-004 (Casbin default-deny mount), ADR-006 (CSRF hardening),
  ADR-010 (fluent API v2 / `pkg/nucleus`)

---

## Context

The real-app readiness review (`docs/audits/2026-05-31-real-app-readiness.md`)
built two applications from the scaffolds — a REST API and a server-rendered
MVC app — and wired a feature end-to-end using only the public API, the CLI,
and the docs. The core path (config → app → router → handlers → DB → migrate →
serve) works. The review found a set of sharp edges between "works in the demo"
and "works in a real app": three reserved-but-unwired `nucleus.Module` fields
that fail silently, a `nucleus serve` that has only one boot mode, a CORS
middleware with no configuration surface, and two coexisting project layouts
with no guidance on which to use.

None of these are showstoppers, but several actively mislead a developer (a
field documented as "migrations to apply" that is silently ignored is a
truth-in-advertising problem). This ADR records the decisions taken in
response, so the rationale is durable and the deferred follow-ups are tracked
rather than forgotten. Findings are referenced by their review IDs (R1–R8).

Note on SQL-first: this framework has no single ADR titled "SQL-first
migrations"; the SQL-first posture is asserted in `SPEC.md` §2 (principle 5,
"SQL-first operations") and reflected throughout the migration tooling (the
`nucleus migrate` command reads `*.up.sql` / `*.down.sql` files; the fluent
`Run`/`Start` path deliberately does not auto-migrate). The R1 decision below
upholds that posture.

---

## Decision

### R1 — `Module.Migrations` is retained but not auto-applied at boot

`nucleus.Module` keeps its `Migrations` field. The runtime does **not** apply
module-embedded migrations at boot. This upholds the SQL-first principle
(`SPEC.md` §2.5): schema changes must be reviewable and applied deliberately
via `nucleus migrate`, not as a side effect of process start. The fluent
`Run`/`Start` path already declines to auto-migrate `Module.Models` for the
same reason (see `examples/mvc_api/README.md`, "Migration note").

To remove the silent no-op, the boot path emits a **WARN** when a registered
module carries a non-empty `Migrations` value, telling the developer that
module-embedded migrations are not applied at boot and to run
`nucleus migrate`. The migration source of truth remains the configured
migrations directory consumed by `nucleus migrate`.

Teaching `nucleus migrate` to optionally consume module-embedded migrations
(so the field becomes wired rather than merely advisory) is recorded as
**future work** — a deferred, Code-implemented follow-up. It must preserve the
invariant that application boot never mutates the schema.

### R2 — `Module.Jobs` and `Module.Webhooks` are retained as reserved shape

`nucleus.Module` keeps its `Jobs` and `Webhooks` fields. They are **reserved
shape** for a later phase: there is no scheduler and no webhook dispatch yet,
and the review confirmed nothing runs when these are populated (the builder
invokes them once against a `nil` registry as a Phase 2+ stub). We retain the
fields rather than remove them so the `nucleus.Module` contract is stable
across the phase that wires them (compatibility-by-contract, `SPEC.md` §2.3).

As with R1, the boot path emits a **WARN** when a registered module carries a
non-empty `Jobs` or `Webhooks` value, so the reserved shape is not mistaken
for working wiring. Execution lands in Phase 2+.

### R3 — `nucleus serve` is full-stack by default, with `--without-defaults`

`nucleus serve` continues to boot the full-stack server (admin, sessions,
default-deny authz) by default — this is the right default for the `mvc`
scaffold and the common case. It gains a `--without-defaults` flag that boots a
**core-only** server (no admin panel, no default-deny Casbin enforcer, no
bootstrap admin user), matching the `api` scaffold's `go run .` path, which
builds the app with `WithoutDefaults()`.

This resolves the review's R3 surprise: the same `api` project run two
"official" ways (`go run .` vs `nucleus serve`) behaved differently —
`go run .` served an open `/healthz`, while `nucleus serve` mounted `/admin`,
activated default-deny authz (surprise 403 on app routes), and created a
bootstrap admin user. `nucleus serve --without-defaults` now gives the
`api`-scaffold developer a CLI on-ramp with the same shape as `go run .`,
instead of forcing a choice between the CLI and a hand-wired `main.go`. The
flag is additive and opt-in.

### R4 — CORS origins become configurable; empty preserves back-compat

The CORS middleware gains a configuration surface: two new config keys,
`cors_origins` (a list of allowed origins) and `cors_allow_credentials` (a
bool). When `cors_origins` is **empty**, behaviour is unchanged from today —
allow-all (`*`) — preserving back-compatibility for existing deployments. The
keys thread into the existing `router.CORSOptions` (`AllowedOrigins`,
`AllowCredentials`) which already ship on the public surface.

This is a deliberate, recorded interim posture against the security-by-default
principle (`SPEC.md` §2.4). A strict reading would default CORS to deny /
same-origin. We are **not** changing the runtime default in this slice,
because flipping a wildcard CORS default is a breaking change for every
existing API deployment and warrants its own deprecation window. The decision
here is to make the secure configuration *possible and documented now*
(developers can set an explicit origin allow-list), and to record tightening
the default — empty meaning "deny" rather than "allow-all" — as a follow-up
scheduled for a major version, routed through `migration-assistant` and
`contract-guardian`. (The existing `*`-plus-credentials footgun is already
closed: when credentials are enabled the middleware reflects the request
origin against the allow-list rather than emitting `*` — see the v0.8.0
CHANGELOG security note.)

Registration of `cors_origins` and `cors_allow_credentials` in
`docs/reference/CONFIG_KEY_REGISTRY.md` is owned by `contract-guardian` and
lands together with the implementing code; until then the keys appear only as
commented hints in the scaffold templates and are not advertised as live,
validated keys in the guides.

### R7 — Two project layouts coexist, documented; generator unification deferred

Nucleus supports two project layouts, and both are valid:

1. The **layered layout** emitted by `nucleus generate resource`:
   `internal/models/`, `internal/controllers/`, `internal/services/`,
   `internal/repositories/`, `internal/contracts/` — code grouped by
   architectural role.
2. The **feature-folder (module) layout** used by `examples/mvc_api`:
   `internal/<feature>/` (e.g. `internal/notes/`), where each feature owns its
   routes, controller, model, and service behind a single `nucleus.Module`
   registration.

We document both layouts and when to use each in the project-structure docs
rather than declaring one canonical. Unifying the `generate` family on the
feature-folder convention (so the generator and the reference app agree) is
recorded as a **deferred, Code-implemented follow-up**; it touches the
generator's output and is therefore sequenced behind a `contract-guardian`
review.

---

## Consequences

### Positive

- The three reserved/unwired `nucleus.Module` fields (`Migrations`, `Jobs`,
  `Webhooks`) stop failing silently; a boot WARN makes the gap visible without
  changing the stable struct shape.
- The `api`-scaffold developer gets a CLI boot mode
  (`nucleus serve --without-defaults`) that matches the lean app the scaffold
  produced.
- Operators can pin CORS to an explicit origin allow-list today, instead of
  shipping wildcard CORS with no recourse.
- The two project layouts are no longer an undocumented trap; developers get
  explicit guidance.
- Every decision is durable and its deferred follow-up is tracked, not lost.

### Negative

- `Module.Migrations`, `Jobs`, and `Webhooks` remain partially advisory until
  the follow-ups land: the WARN tells the truth, but the fields are not yet
  fully wired. This is honest but not yet ergonomic.
- The CORS default stays allow-all for back-compat, so the secure posture is
  opt-in until the default is tightened in a future major. The
  security-by-default ideal is not fully met for CORS in this slice; the gap is
  explicit and scheduled.
- Two supported layouts is more surface to document and reason about than one
  canonical layout, until the generator is unified.

---

## Alternatives considered

### R1 — auto-apply module migrations at boot

Rejected: violates the SQL-first invariant that boot never mutates the schema
(`SPEC.md` §2.5). Convenient in development, dangerous in production, and it
would couple schema changes to process start. The WARN-plus-`nucleus migrate`
path keeps schema evolution deliberate.

### R2 — remove `Jobs` / `Webhooks` until they are wired

Rejected: removing then re-adding fields churns the `nucleus.Module` contract
and breaks any code that already references the shape. Retaining the reserved
fields with a boot WARN is the compatibility-by-contract choice.

### R3 — make `nucleus serve` core-only by default, or add a separate command

Rejected. Core-only-by-default would regress the `mvc` scaffold's common case
(it expects admin, sessions, default-deny authz). A separate top-level command
(e.g. `nucleus serve-core`) inflates the CLI surface for what is a mode of one
command; a `--without-defaults` flag is the smaller, more discoverable
surface, and it mirrors the existing `WithoutDefaults()` builder method.

### R4 — flip the CORS default to deny / same-origin now

Rejected for this slice: it is a breaking change for every existing API
deployment relying on the wildcard default and needs a deprecation window.
Recorded as a future major-version change instead, so the back-compat promise
holds while the secure configuration becomes available immediately.

### R4 — a single boolean `cors_enabled` flag

Rejected: a boolean cannot express an origin allow-list, which is the actual
production need. `cors_origins` (list) plus `cors_allow_credentials` (bool)
matches how CORS is actually configured and maps onto the existing
`router.CORSOptions` fields.

### R7 — pick one layout and rewrite the other

Rejected as premature. Forcing the example onto the layered layout, or the
generator onto the feature-folder layout, is a larger change with output
implications (the generator's emitted tree). Documenting both now and unifying
the generator later is the lower-risk sequence.

---

## Status notes

This ADR is **Accepted**. The R1/R2 boot WARNs, the R3 `--without-defaults`
flag, and the R4 `cors_origins` / `cors_allow_credentials` keys are
implemented by Code in the accompanying slice and registered through
`contract-guardian` where they touch stable surfaces. The R7 documentation
lands with this slice. The deferred follow-ups — wiring module-embedded
migrations into `nucleus migrate` (R1), executing `Jobs`/`Webhooks` (R2,
Phase 2+), tightening the CORS default in a future major (R4), and unifying
the generator on the feature-folder layout (R7) — are tracked here and
sequenced behind their respective reviews.

The two smaller fixes in the same readiness batch — `nucleus doctor` probing
the scaffold's `rbac_policy.csv` filename (R5) and the admin bootstrap
password / example working-directory documentation (R6, R8) — are
straightforward and do not require an architectural decision; they are
recorded in `CHANGELOG.md` only.

End of ADR.
