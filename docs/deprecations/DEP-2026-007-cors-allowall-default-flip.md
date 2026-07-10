# Deprecation Notice: implicit allow-all CORS default flips to deny at v1.0.0

- ID: `DEP-2026-007`
- Status: `completed` (default flipped at v1.0.0, 2026-07-09)
- Announced in: `Unreleased` (v1 gate A-5a decision, 2026-07-08)
- Default change landed: `v1.0.0` (the major version ADR-013 R4 scheduled;
  this is a **default-value change**, not a key removal — the keys survive)
- Scope: `config` (behavioral default)
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

ADR-013 R4 (2026-05) recorded the allow-all CORS default as "a deliberate,
recorded interim posture against the security-by-default principle
(SPEC §2.4)" and scheduled the tightening — empty `cors_origins` meaning
deny rather than allow-all — "for a major version, routed through
migration-assistant and contract-guardian". v1.0.0 is the first major since
that promise; the maintainer decision (v1 gate A-5a, 2026-07-08) is to
**honor it there** rather than waive to v2.0.

What changes and when:

- **v0.11.0 (this notice):** an empty `cors_origins` (the default) emits a
  one-time startup WARN announcing the flip. Behavior is unchanged —
  allow-all (`Access-Control-Allow-Origin: *` for credential-less requests)
  still applies.
- **v1.0.0:** an empty `cors_origins` stops emitting CORS headers for
  cross-origin requests (deny). Keeping allow-all becomes an explicit,
  test-covered opt-in: `cors_origins: ["*"]`.

The dangerous half of the historical posture was already closed before this
notice: `cors_allow_credentials` is only honored with an explicit allow-list
(ADR-014, SEC-1, 2026-06-08), and `["*"]`+credentials reflects the request
origin instead of emitting `*` (FW-6 regression guard). This flip is the
remaining, promised half.

## Affected Surfaces

- `cors_origins` — the **default interpretation of empty** changes at
  v1.0.0; the key itself is unchanged and remains `stable`.
- `cors_allow_credentials` — unchanged (already requires an explicit
  allow-list).
- No Go API changes: `router.WithCORSOrigins`/`WithCORSCredentials` and
  `router.CORSOptions` keep their shapes.

## Migration Path

- Deployments serving browser clients from other origins: set the real
  allow-list — `cors_origins: ["https://app.example.com"]`.
- Deployments that genuinely want allow-all (public credential-less APIs):
  set `cors_origins: ["*"]` — explicit, and covered by
  `TestCORSMiddleware_WildcardWithoutCredentialsEmitsStar`.
- Same-origin-only deployments (no cross-origin browser clients): nothing
  to do; the v1.0.0 default matches their reality and removes headers they
  never needed.
- Behavior differences before v1.0.0: none — only the WARN is new.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-007-cors-explicit-origins.md`
- Detection rule: startup WARN matching `"cors_origins is empty"` in
  application logs, or absence of the `cors_origins` key from the loaded
  config while browser clients call the API cross-origin.
- Suggested rewrite: one YAML list — the real allow-list or `["*"]`.

## Validation

- Compatibility tests updated: `n/a at announcement` (no behavior change
  until v1.0.0; the flip release updates the CORS middleware defaults and
  their tests deliberately, with the migration note in the v1.0.0 release
  notes as ADR-013 R4 required).
- Release note updated: pending merge (conventional commit feeds
  release-please notes).
- Rollback plan documented: `yes` — pre-flip, unset keys keep today's
  behavior; post-flip, `cors_origins: ["*"]` restores allow-all exactly.

## Timeline

- Announcement date: `2026-07-08`
- Review checkpoint: at `v0.12.0` release prep — confirm the WARN shipped in
  v0.11.0 and gauge implicit-default usage.
- Default flip date: `v1.0.0` release branch — flip + migration note +
  ADR-013 R4 closure (successor note in the ADR, per its own text).
