# Migration Assistant: `session_idle_timeout` from `0` to `30m` by default

- ID: `MA-2026-021`
- Pairs with: `docs/deprecations/DEP-2026-021-session-idle-timeout-default.md`
- Severity: `medium` at v2.0.0 for applications that set nothing (users of
  an idle session sign in again after 30 minutes); `low` before it.
- Status: `current`

---

## Scope

Session-authenticated applications whose configuration does not set
`session_idle_timeout` (nor `NUCLEUS_SESSION_IDLE_TIMEOUT`).

Out of scope: applications that set the key to any value, `0` included;
API-key and bearer authentication.

## Detection

```bash
nucleus doctor security              # warns: session_idle_timeout=0 in production
grep -n 'session_idle_timeout' nucleus.yml config/*.yml 2>/dev/null || echo "not set: the default applies"
```

## Rewrite

| Before | After | Kind |
|---|---|---|
| key absent, today's behaviour wanted | `session_idle_timeout: 0` — an explicit zero survives the new default (`TestLoadConfig_AnExplicitZeroSurvivesANonZeroDefault`) | manual (one key) |
| key absent, the new default wanted early | `session_idle_timeout: 30m` — `doctor security` stops asking | manual (one key) |
| key set | nothing | — |

## Verification

```bash
nucleus doctor security
nucleus diffsettings --json | grep session_idle_timeout
```

## Rollback

`session_idle_timeout: 0` restores today's behaviour at any version.
