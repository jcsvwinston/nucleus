# Migration Assistant: `auth.CookieSessionStore` → functional session stores

- ID: `MA-2026-006`
- Pairs with: `docs/deprecations/DEP-2026-006-cookiesessionstore-removal.md`
- Severity: `low` for correctness (the store never persisted anything, so
  there is no data to migrate), `medium` for surprise (applications using it
  have been silently losing sessions — the migration FIXES a live defect).
- Status: `current`

---

## Scope

Applications that construct `auth.CookieSessionStore` via
`auth.NewCookieSessionStore(encryptionKey)` and install it with
`SessionManager.SetStore`. The store was never reachable from configuration
(`session_store` accepts `memory`/`sql`/`redis` only), so the config path is
out of scope — only Go source is affected.

## Detection

**Go source — search for the deprecated symbols:**

```bash
# From the consumer repo root.
grep -rn "NewCookieSessionStore\|CookieSessionStore" --include="*.go" .
```

**Behavioural signal:** sessions that never survive a second request while
the rest of the auth stack works. There is no log signature — the store
fails silently; that silence is the defect being removed.

## Rewrite

Pick the store that matches the deployment:

```go
// Default: just delete the SetStore call — the manager's built-in
// memory store takes over (sessions survive within the process).

// SQL-backed (shares the app database):
store, err := auth.NewSQLSessionStore(sqlDB, auth.SQLSessionStoreConfig{ /* ... */ })

// Redis-backed:
store, _, err := auth.NewRedisSessionStoreFromURL(redisURL, prefix)
```

Then `sessionManager.SetStore(store)` as before. Configuration-driven apps
should prefer the `session_store` key (`memory`/`sql`/`redis`) over
programmatic construction.

## Rollback

None needed: the deprecated store never persisted data, so there is no
state to preserve or restore. Reverting the code change merely reinstates
the session-loss defect.

## Validation

After the move, boot the app and confirm:

1. a session created by login survives a second request;
2. the orbit admin session screen (if mounted) lists active sessions —
   the deprecated store always showed an empty list
   (`ErrSessionStoreNotIterable`).
