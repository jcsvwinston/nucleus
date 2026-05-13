# ADR-004: Casbin Enforcer Mounted with Default-Deny by `App.New`

**Status:** Proposed
**Date:** 2026-05-13
**Superseded:** No

## Context

[#41](https://github.com/jcsvwinston/nucleus/pull/41) introduced deny-override semantics to the Casbin enforcer and added `Enforcer.Deny` for explicit override rules. The package now correctly denies any request not matched by an `allow` policy.

But the framework does not actually mount the enforcer by default. `App.New` does not construct `*authz.Enforcer`, does not attach `Enforcer.Middleware()` to the router, and does not invoke any of the policy-management API. An operator who calls `app.New(cfg)` today gets a process where *every* request passes through with no authorization check — regardless of whether they configured a policy file. The primitive ships; the product does not consume it.

This is the same "primitive exists ≠ product uses it" gap that motivated the integration sprint. The other two PRs in that sprint (JWT rotation, circuit breaker) have the same shape, but RBAC is the most consequential of the three: every other security control in the system assumes that authorization is enforced somewhere downstream. If `App.New` never mounts it, no other framework default closes that hole.

Two reasonable alternatives:

1. **Mount only when `admin_rbac_policy_file` is set.** Conservative — never changes behaviour for existing apps. Cost: operators who forget to set the config key get an unprotected app, and the framework happily ships in that state. This is the status quo and it is the gap.
2. **Mount with an allow-all default policy.** Avoids the breaking change. Cost: defeats the entire point of "default-deny". An enterprise review will reasonably refuse to credit the feature.

## Decision

`App.New` mounts the Casbin enforcer and its `Middleware` by default. The enforcer arrives with an **empty user policy** but a **seeded bootstrap allow-list** for routes that the framework itself owns and should never be gated by RBAC:

- `/healthz`
- `/metrics`
- `/admin/login`
- `/login` (when the auth flow is mounted)
- `/.well-known/jwks.json` (when JWKS is mounted per ADR-004's sibling work)
- Static asset paths under `static_url_prefix`

These bootstrap entries are added programmatically before the middleware mounts; they are not part of the operator's `admin_rbac_policy_file`. Removing them at runtime requires explicit code action, not a config flip.

The user-facing policy is whatever the operator loads via `admin_rbac_policy_file`. If that file is empty or unset, **every non-bootstrap request returns `403 Forbidden`**. `App.New` logs a `WARN` once at startup when this is the case:

```
authz: no user policies loaded; only bootstrap routes (/healthz, /metrics, /admin/login, /login) will respond. Set admin_rbac_policy_file or call Enforcer.AddPolicy programmatically.
```

For deployments that genuinely want unauthenticated apps (early development, internal tooling, demos), `app.WithOpenAuthz()` is the explicit opt-out. It skips mounting the middleware entirely, and logs a `WARN`:

```
authz: WithOpenAuthz() in effect — no authorization checks will run. This is unsafe outside development.
```

`WithOpenAuthz()` is not a config flag. Reaching for it requires touching code and surfacing the decision in PR review.

## Consequences

### Positive

- The default `app.New(cfg)` produces an app where every business endpoint requires an explicit allow rule. This matches the "secure by default" framing in `SPEC.md` and the "default-deny with deny-override" semantics already documented in `pkg/authz`.
- A misconfigured deployment (no policy file, no `WithOpenAuthz`) fails closed rather than silently exposing endpoints. The startup WARN tells the operator exactly which knob to turn.
- Framework-internal routes (`/healthz`, `/metrics`, login endpoints) keep working without operator action — Kubernetes probes, Prometheus scrapes and the bootstrap login flow do not require the operator to remember to whitelist them.
- The CHANGELOG entry for [#41](https://github.com/jcsvwinston/nucleus/pull/41) becomes truthful: deny-override is not just "available", it is "applied to every request by default".

### Negative

- **Breaking change for any app upgrading.** Existing applications that called `app.New(cfg)` without `admin_rbac_policy_file` set will return 403 on every business endpoint after the upgrade. The escape hatches are `WithOpenAuthz()` (explicit opt-out, traced in logs) or loading a policy file with the desired allow rules.
- Examples under `examples/*` that currently do not ship policy files need a policy file (or `WithOpenAuthz`) for their MVC routes to keep responding. Acceptable cost — the example apps already declare admin auth, JWT, etc., so adding RBAC is consistent. We update each example as part of the integration sprint that lands this ADR.
- Some test harnesses construct `app.New` inline and expect 200s on arbitrary routes. Those tests need to either load policies or use `WithOpenAuthz`. The change surfaces in CI on the first integration-sprint PR and is part of its acceptance criteria.

### Neutral

- The bootstrap allow-list is a small, framework-owned source-of-truth file. It expands when new framework-owned routes appear (e.g. a `/oauth2/callback` if that ever lands). Each addition is a deliberate code change, not a config knob, so the list stays auditable.

## Compliance

After this ADR is accepted:

1. `pkg/app/app.go` mounts `*authz.Enforcer.Middleware` on the router as part of `New`, after the request-scope middleware and before user-mounted routes.
2. The bootstrap allow-list is seeded into the enforcer programmatically before the middleware mounts. The list lives in `pkg/authz/policies.go` as `bootstrapAllowList()` or similar — it is not a config-file artefact and cannot be overridden by `admin_rbac_policy_file`.
3. `app.WithOpenAuthz()` is the only escape hatch; it skips mounting and logs a `WARN`. There is no `Config.OpenAuthz: true` config key — opt-out requires code.
4. Examples under `examples/*` ship a minimal `policy.csv` or use `WithOpenAuthz` with a justifying comment.
5. The CHANGELOG entry for the integration-sprint PR flips [#41](https://github.com/jcsvwinston/nucleus/pull/41) from "primitive added" to "primitive added + wired by default" and calls out the breaking-change path under `### Changed`.
6. `docs/guides/AUTH_GUIDE.md` is updated to document the bootstrap allow-list, the WARN log on empty user policy, and the `WithOpenAuthz` escape hatch.

## Related

- [`pkg/authz`](../../pkg/authz) — enforcer + middleware.
- [#41](https://github.com/jcsvwinston/nucleus/pull/41) — deny-override + `Enforcer.Deny`.
- `.claude/state/HANDOFF.md` — integration-sprint iteration that consumes this ADR.
- ADR-001: stdlib-first runtime — explicitly chooses the smaller, more invasive default rather than an optional layer.
- `SPEC.md` §"security-by-default" — the framing this ADR makes concrete.
