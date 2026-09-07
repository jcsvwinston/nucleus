---
sidebar_position: 2
title: Sessions & passwords
covers:
  - pkg/auth.HashPassword
  - pkg/auth.CheckPassword
  - pkg/auth.NewSessionManager
  - pkg/auth.NewRedisSessionStore
  - pkg/auth.NewSQLSessionStore
  - pkg/auth.NewMemcachedSessionStore
  - pkg/nucleus.Runtime.Session
config_keys:
  - session_store
  - session_cookie_secure
  - session_cookie_samesite
  - session_lifetime
  - redis_url
---

# Sessions & passwords

The basics for a login form: where sessions live, what the cookie defaults
are, and how passwords are hashed. The complete login walkthrough is
[Your first login](./your-first-login.md).

## Sessions

The session manager is store-pluggable:

| Store    | When to use                                                |
| -------- | ---------------------------------------------------------- |
| `memory` | Development, single-process tests.                         |
| `sql`    | Single-binary production. Sessions live in your primary DB. |
| `redis`  | Multi-replica or container deployments.                    |

```yaml
session_store: redis
session_cookie_secure: true   # default: true — secure by default
session_cookie_samesite: lax
session_lifetime: 24h
redis_url: redis://localhost:6379
```

`session_cookie_secure` defaults to `true`. The session cookie will not ride
over plain HTTP unless you opt out explicitly. Local development over
`http://localhost` must set `session_cookie_secure: false` in
`nucleus.yml` (or `NUCLEUS_SESSION_COOKIE_SECURE=false` in the environment)
— browsers reject `Secure` cookies on non-HTTPS origins. Production deployments
should never set this to `false`.

Each session record is enriched with runtime metadata — pod, host,
instance — for attribution in audit logs and observability tooling.

## Session lifecycle — what the framework does for you

The framework mounts the session middleware globally at startup. By the time a
request reaches your handler, its session is already loaded from the store,
and it is saved again after the handler returns.

**Do not mount the session middleware a second time.** Doing so wraps the
session twice and produces double-commit errors.

For simple key/value use, no extra wiring is needed: handlers read and write
session values straight through the request context, using the helpers on
`*router.Context` such as `c.SessionPutString` and `c.SessionGetString`.

Some operations go beyond get/put — `RenewToken` after a successful login
(the defence against session fixation), `Destroy`/`Invalidate` on logout, and
flash messaging. For those, capture the session manager once in `OnStart` via
`rt.Session()` and call it directly:

```go
var authModule = nucleus.Module[struct{}]{
    Name:   "auth",
    Prefix: "/auth",
    OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
        sm = rt.Session()    // *auth.SessionManager; nil only if session is unconfigured
        az = rt.Authorizer() // *authz.Enforcer
        return nil
    },
    Routes: func(r nucleus.Router, _ struct{}) {
        r.Post("/login",  loginHandler)
        r.Post("/logout", logoutHandler)
    },
}

// loginHandler: validate credentials, then renew the session token.
func loginHandler(c *nucleus.Context) error {
    // ... verify user credentials ...
    if err := sm.RenewToken(c.Request.Context()); err != nil {
        return err
    }
    c.SessionPutString("user_id", user.ID)
    return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// logoutHandler: destroy the session entirely.
func logoutHandler(c *nucleus.Context) error {
    return sm.Destroy(c.Request.Context())
}
```

## Password hashing

Passwords are hashed with **bcrypt at cost 12**. Each hash embeds the cost it
was produced with, so raising the cost in a future release does not invalidate
existing hashes — they keep verifying at their original cost.

Re-hashing a stored password at a higher cost is your application's job: do it
after a successful `CheckPassword`, by calling `HashPassword` again and saving
the new value.

```go
import "github.com/jcsvwinston/nucleus/pkg/auth"

hash, err := auth.HashPassword("hunter2")
if err != nil {
    // handle hashing failure
}
ok := auth.CheckPassword("hunter2", hash) // (plaintext, hash) → bool
```

## CSRF, CORS and rate limiting

These are middleware-level concerns, documented in
[Concepts → Routing & middleware](../../concepts/routing.md). In short: CORS
denies unknown origins by default, and rate limiting is configured from
`nucleus.yml`.

CSRF protection is configured per scaffold: the mvc template ships
`csrf_enabled: true`, the api template leaves it off. Session-mutating
routes such as login and logout are exactly the ones it protects — see
the [quickstart's CSRF note](../../getting-started/quickstart.md#a-note-on-csrf)
and [Your first login](./your-first-login.md) for how the token reaches
forms and JSON clients.
