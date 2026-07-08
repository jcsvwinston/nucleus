# Deprecation Notice: `auth.CookieSessionStore` scheduled for removal

- ID: `DEP-2026-006`
- Status: `active`
- Announced in: `Unreleased` (v1 gate A-3 decision, 2026-07-08)
- Earliest removal: `v0.12.0` (pre-`v1.0` removal, exception-only per
  `docs/governance/DEPRECATION_TEMPLATE.md` — explicit maintainer approval
  recorded in `docs/V1_GATE.md` §A-3; same train as DEP-2026-004/005)
- Scope: `api`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

`auth.CookieSessionStore` has **never been functional**: `CommitCtx` encrypts
the session payload and then discards the ciphertext
(`pkg/auth/session_store_cookie.go`, `_ = encoded`), because the frozen
`SessionStore` contract (`Commit(token, payload, expiry)`) has no access to
the HTTP response and therefore cannot set a cookie. Sessions written through
this store are silently lost. The defect is architectural — no fix exists
inside the current interface — and was carried through three audits (N-1, P1).

The maintainer decision (v1 gate A-3, 2026-07-08) is **removal via the
deprecation train** rather than wiring or freezing it:

- Wiring requires a response conduit, a 4KB cookie budget policy, and a
  revocation story — a new feature nobody has requested, rushed before a
  freeze.
- Freezing it "as documented non-functional" contradicts the gate's honesty
  rule (a v1.0 promise on a symbol that cannot work).
- Removal is contained: the store was never selectable via config
  (`session_store` accepts `memory`/`sql`/`redis` and errors on anything
  else), so only direct programmatic constructors are affected. It also
  breaks the session-enumeration surface the orbit admin consumes
  (`All`/`AllCtx` return an empty map; `ErrSessionStoreNotIterable` exists
  because of this store).

`v0.11.0` ships the `Deprecated:` godoc markers on `CookieSessionStore` and
`NewCookieSessionStore`; `v0.12.0` removes the type, constructor, and the
`cookieSessionData` internals, with a deliberate freeze-baseline rebaseline.
A response-aware cookie-session feature may return post-`v1.0` under a
contract designed for it.

## Affected Surfaces

- `auth.CookieSessionStore` (type, all methods: `Delete`, `Find`, `Commit`,
  `All`, `DeleteCtx`, `FindCtx`, `CommitCtx`, `AllCtx`).
- `auth.NewCookieSessionStore(encryptionKey string)`.
- No config keys are affected (`session_store=cookie` was never a valid
  value).

## Migration Path

- Replacement: the functional stores — `memory` (default), `sql`
  (`auth.NewSQLSessionStore`), `redis` (`auth.NewRedisSessionStore` /
  `FromURL`), or memcached (`auth.NewMemcachedSessionStore`).
- Behavior differences: any of the above actually persists sessions;
  applications that constructed a `CookieSessionStore` were losing every
  session write, so switching stores is strictly an improvement. There is
  no data to migrate (the store never stored anything).
- Required app changes: replace `auth.NewCookieSessionStore(key)` +
  `SessionManager.SetStore(...)` with one of the functional constructors, or
  simply delete the call (the manager's default memory store takes over).

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-006-cookiesessionstore-to-functional-stores.md`
- Detection rule: use of `auth.NewCookieSessionStore` or the
  `auth.CookieSessionStore` type in Go source; there is no config or log
  signature (the store emits nothing — that silence is the defect).
- Suggested rewrite: swap the constructor for a functional store or remove
  the `SetStore` call entirely.

## Validation

- Compatibility tests updated: `n/a at announcement` — symbols remain in the
  freeze baseline until the v0.12.0 removal, which rebaselines deliberately
  (v1 gate A-3 records the maintainer approval the hard rule requires).
- Release note updated: pending merge (conventional commit feeds
  release-please notes).
- Rollback plan documented: `yes` — re-adding the deprecated symbols restores
  the prior (non-functional) state; no consumer data is involved.

## Timeline

- Announcement date: `2026-07-08`
- Review checkpoint: at `v0.11.0` release prep — confirm the `Deprecated:`
  markers ship and no new consumers appeared.
- Removal decision date: `v0.12.0` release prep — remove type + constructor,
  rebaseline, close v1 gate A-3's removal step.
