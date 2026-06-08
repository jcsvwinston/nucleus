# ADR-014: CORS `corsAllowCredentials` Default Flipped to `false` (SEC-1)

Reference date: 2026-06-08.
Status: Accepted.
Supersedes (in part): the interim CORS-default posture recorded in
[ADR-013 §R4](ADR-013-real-app-readiness.md).

## Context

ADR-013 §R4 introduced the `cors_origins` / `cors_allow_credentials` config
keys and **deliberately preserved** the existing runtime default — allow-all
(`*`) origins with `corsAllowCredentials: true` — recording the tightening as a
follow-up "scheduled for a major version, routed through `migration-assistant`
and `contract-guardian`." That posture treated the change as a
breaking-change-for-convenience that deserved a deprecation window.

The 2026-06-07 exhaustive audit (`docs/audits/2026-06-07-exhaustive-audit-v2.md`,
finding **SEC-1**, P0-before-v1.0, `[verified]`) reclassified it as a **security
defect**, not a convenience change:

- `router.New()` defaulted `corsAllowAll: true, corsAllowCredentials: true`.
- The v0.8.0 FW-6 fix stopped the spec-invalid `*` + credentials pair, but the
  default path then **reflected every request `Origin`** with
  `Access-Control-Allow-Credentials: true` (`pkg/router/corsmw.go`).
- `pkg/app` only restricted CORS when `cors_origins` was non-empty — never in a
  zero-config deployment.
- Blast radius is tempered today by `SameSite=Lax` session cookies, but any
  deployment running `session_cookie_samesite: none` becomes "any origin can
  read authenticated cross-origin responses." This violates `SPEC.md` §2.4
  (security-by-default).

A defect of this class should not wait for the next major version. (Note: the
`CONFIG_KEY_REGISTRY.md` already documented the default as `false`, so the code
was the outlier.)

## Decision

Flip the secure default ahead of the ADR-013 §R4 schedule:

1. `router.New()` defaults `corsAllowCredentials` to **`false`**.
2. `Access-Control-Allow-Credentials: true` is emitted **only** when the app
   provides an explicit origin allow-list (`cors_origins` /
   `router.WithCORSOrigins`) **and** opts in
   (`cors_allow_credentials: true` / `router.WithCORSCredentials(true)`). On that
   path the middleware reflects the specific allow-listed origin (never `*`),
   preserving the FW-6 invariant.
3. `pkg/app` emits a boot-time `WARN` when `cors_allow_credentials: true` is set
   while `cors_origins` is empty: the setting is ignored (credentials are not
   emitted with the allow-all default) rather than silently widening it.

The exported surface is unchanged — `WithCORSCredentials`,
`Config.CORSAllowCredentials`, `Config.CORSOrigins` all keep their signatures;
only the zero-value default changes.

## Alternatives considered

- **Hold for a major version (ADR-013 §R4 status quo).** Rejected: SEC-1 is a
  verified security defect; the security-by-default principle outranks the
  convenience-breaking-change schedule.
- **Hard boot error on `credentials && no origins`.** Rejected: the
  misconfiguration is self-neutralising (the `pkg/app` gate never passes
  credentials to the router without an allow-list), so a hard error would break
  deployments without removing any live risk. A `WARN` gives operators
  visibility without a failed boot. (Contrast: `SameSite=None` without `Secure`
  hard-errors because the browser silently drops the cookie — a real harm.)
- **Also flip origins to deny/same-origin by default.** Out of scope: only the
  credentials default is a security defect. Allow-all for *credential-less*
  requests stays the back-compatible default; tightening origins remains a
  separate, larger decision.

## Consequences

- **Behavioural breaking change (operational), pre-1.0.** Deployments that
  relied on credentialed CORS by default must now set an explicit `cors_origins`
  allow-list **and** `cors_allow_credentials: true`. Recorded as a `BREAKING
  (operational)` migration note in `CHANGELOG.md`; consistent with the
  `session_cookie_secure` default flip precedent — no `docs/deprecations/` entry
  or `migration-assistant` spec is required (confirmed by `contract-guardian`).
- **The boot `WARN` is a `pkg/app`-layer courtesy, not a framework-wide
  guardrail.** A consumer calling `router.New(logger, WithCORSCredentials(true))`
  directly (bypassing `pkg/app`) without an allow-list will not see the WARN; the
  router still simply will not emit credentials with `*`. Documented on the
  `WithCORSCredentials` godoc.
- No exported symbol or config key changes; the freeze test does not (and
  structurally cannot) catch a runtime-default change. Extending the freeze
  harness to cover security-boolean defaults is a tracked follow-up for
  `governance-checker`.
