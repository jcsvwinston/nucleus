# Behaviour-Change Notice: `session_idle_timeout` ships as 30m at v2.0.0

- ID: `DEP-2026-021`
- Status: `active`
- Announced in: the next minor after `v1.32.0` (A12 `M0-N`; decision D1 of the owner, 2026-10-16)
- Behaviour flip: `v2.0.0`, no earlier than 2027-01-17 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md`; the suite groups every breaking
  change into the one major at the close of its A12 arc (QADR-0010). This is a
  **default-value change of a configuration key**: the key survives, no
  `// Deprecated:` marker carries it.
- Scope: `config`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

`session_idle_timeout` is honoured and ships disabled: its default is `0`, so
a session that is never used again stays valid for its whole
`session_lifetime` (72 hours by default). `nucleus doctor security` warns
about the `0` in production and ASVS V3.3.2 asks for an inactivity timeout.
Turning it on by default in a minor would start expiring sessions in every
deployment that upgrades, which is why it waited for the major (NU-72,
QADR-0010).

What changes and when:

- **This release:** nothing. The default stays `0`; this notice and the pinned
  test say so.
- **v2.0.0:** the default becomes `30m` — what `docs/guides/AUTH_GUIDE.md`
  shows and what `doctor security` asks for. A session with no request for
  30 minutes expires, whatever its lifetime says. An explicit
  `session_idle_timeout: 0` keeps the behaviour of today: the loader applies
  the struct defaults first and the configuration file on top, so a zero
  written in the file survives a non-zero default (pinned by
  `TestLoadConfig_AnExplicitZeroSurvivesANonZeroDefault`).

## Affected Surfaces

- `session_idle_timeout` in the configuration (`Config.SessionIdleTimeout`),
  and `NUCLEUS_SESSION_IDLE_TIMEOUT`.
- Every session-authenticated route of an application that does not set the
  key.
- Not affected: applications that already set the key, to any value including
  `0`; API-key and bearer-token authentication, which have no idle timer.

## Migration Path

- Replacement: decide the value before the upgrade and write it down. To keep
  today's behaviour: `session_idle_timeout: 0`. To adopt the new default
  early and stop `doctor security` asking: `session_idle_timeout: 30m`.
- Behavior differences: at v2.0.0 an application that sets nothing expires
  idle sessions after 30 minutes; its users sign in again after a pause.
- Required app changes: one configuration key, or none to accept the default.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-021-session-idle-timeout.md`
- Detection rule: `nucleus doctor security` reporting `session_idle_timeout=0`
  (an explicit zero shows the same warning; it is the intended choice when
  written); a configuration without the key.
- Suggested rewrite: manual — one key.

## Validation

- Compatibility tests updated: `yes` —
  `TestSessionIdleTimeout_DefaultIsZeroInV1` pins the v1 default and
  `TestLoadConfig_AnExplicitZeroSurvivesANonZeroDefault` the mechanism the
  flip relies on (both in `pkg/app/session_idle_timeout_v1_test.go`). The
  2.0 PR changes the first in the same commit as the default.
- Release note updated: through the squash commit.
- Rollback plan documented: `yes` — before v2.0.0 nothing to roll back; after
  it, `session_idle_timeout: 0` restores today's behaviour.

## Timeline

- Announcement date: `2026-10-16`
- Review checkpoint: `2027-01-17`, when the window opens.
- Behaviour flip decision date: the `v2.0.0` major.

## Notes

The umbrella guard reads this notice through its `Status` and «no earlier
than» lines and the suite's list of the major
(`quantum/docs/planes/quantum-2.0.md` §3.0, row 23).
