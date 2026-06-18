---
sidebar_position: 2
title: Auth & sessions
covers:
  - pkg/auth.HashPassword
  - pkg/auth.CheckPassword
  - pkg/auth.NewJWTManager
  - pkg/auth.NewJWTManagerFromKeys
  - pkg/auth.JWTManager.Generate
  - pkg/auth.JWTManager.Validate
  - pkg/auth.JWTManager.RotateKey
  - pkg/auth.JWTManager.RemoveKey
  - pkg/auth.JWTManager.JWKSHandler
  - pkg/auth.NewSessionManager
  - pkg/auth.NewRedisSessionStore
  - pkg/auth.NewSQLSessionStore
  - pkg/auth.NewMemcachedSessionStore
  - pkg/auth.ContextWithClaims
  - pkg/auth.ClaimsFromContext
  - pkg/auth.Claims
  - pkg/authz.New
  - pkg/authz.NewFromModel
  - pkg/authz.Enforcer
  - pkg/authz.Enforcer.Can
  - pkg/authz.Enforcer.AddPolicy
  - pkg/authz.Enforcer.Deny
  - pkg/authz.Enforcer.RemovePolicy
  - pkg/authz.Enforcer.RequireRole
  - pkg/authz.Enforcer.AddRole
  - pkg/authz.Enforcer.RemoveRole
  - pkg/authz.Enforcer.GetRoles
  - pkg/authz.Enforcer.GetPolicy
  - pkg/authz.Enforcer.GetGroupingPolicy
  - pkg/authz.Enforcer.GetAllRoles
  - pkg/app.JWTKeySpec
  - pkg/nucleus.Runtime.Session
  - pkg/nucleus.Runtime.Authorizer
  - pkg/nucleus.Runtime.JWT
config_keys:
  - session_store
  - session_cookie_secure
  - session_cookie_samesite
  - session_lifetime
  - redis_url
  - jwt_secret
  - jwt_expiry
  - jwt_issuer
  - jwt_keys[]
  - jwt_current_kid
  - admin_rbac_policy_file
---

# Auth & sessions

`pkg/auth` and `pkg/authz` cover authentication, session management and
authorization.

## Sessions

The session manager is store-pluggable:

| Store    | When to use                                                |
| -------- | ---------------------------------------------------------- |
| `memory` | Development, single-process tests.                         |
| `sql`    | Single-binary production. Sessions live in your primary DB. |
| `redis`  | Multi-replica or container deployments.                    |

```yaml
session_store: redis
session_cookie_secure: true   # default: true — secure-by-default (SPEC §2.4)
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
instance — so the admin panel can show which replica handled which
session.

## Password hashing

Passwords are hashed with **Argon2id** by default. The hash format is
versioned, so increasing the cost parameters in a future release is a
seamless upgrade — old hashes continue to verify, and re-hashing happens
on the next successful login.

```go
import "github.com/jcsvwinston/nucleus/pkg/auth"

hash, err := auth.HashPassword("hunter2")
if err != nil {
    // handle hashing failure
}
ok := auth.CheckPassword("hunter2", hash) // (plaintext, hash) → bool
```

## JWT

`pkg/auth` exposes a `JWTManager` for stateless auth. It supports two
modes that coexist within the same process: a legacy single-secret
HS256 path for quick starts, and a multi-key keyset with rotation and
JWKS publication for production deployments.

### Single-secret HS256 (quick start)

```go
mgr := auth.NewJWTManager(secret, 24*time.Hour, "my-issuer")

token, err := mgr.Generate(userID, username, role)
claims, err := mgr.Validate(token)
```

The single secret is supplied through the `jwt_secret` config key. Being
sensitive, it is set via the `NUCLEUS_JWT_SECRET` environment variable
rather than written into `nucleus.yml` directly — config files end up
checked in. (`jwt_secret` is also a non-nullable security key: setting it
to `null`, or `NUCLEUS_JWT_SECRET` to empty, is a boot error.)

Tokens in this mode carry no `kid` header.

### Multi-key with rotation (production)

`App.New` builds `App.JWT` automatically when `jwt_keys[]` is set in
`nucleus.yml`. Operators do not call `auth.NewJWTManagerFromKeys`
themselves for the common case.

```yaml
# nucleus.yml
jwt_issuer: myapp
jwt_current_kid: 2026-q2-rsa
jwt_keys:
  - kid: 2026-q2-rsa
    algorithm: RS256
    pem_path: /run/secrets/jwt-rsa-q2.pem
  - kid: legacy-hs
    algorithm: HS256
    secret_env: JWT_LEGACY_SECRET
```

### AWS Secrets Manager key references

For keys stored in AWS Secrets Manager, use the `aws-sm:` scheme in the
`secret_env` or `pem_env` field instead of a plain environment variable
name:

```yaml
jwt_keys:
  - kid: 2026-q2-rsa
    algorithm: RS256
    # Fetch the whole SecretString as the PEM document:
    pem_env: aws-sm:myapp/prod/jwt-rsa-q2

  - kid: 2026-q2-hs
    algorithm: HS256
    # Fetch the "signing" JSON key out of a JSON-object secret:
    secret_env: aws-sm:myapp/prod/jwt-secrets#signing
```

Reference forms:

| Form                                  | Resolution                                                       |
| ------------------------------------- | ---------------------------------------------------------------- |
| `aws-sm:<secret-id>`                  | The full `SecretString` of the named secret.                     |
| `aws-sm:<secret-id>#<json-key>`       | One string-valued key from a JSON-object `SecretString`.         |
| `env:NAME` or bare `NAME`             | The value of the named environment variable (existing behaviour).|

`App.New` builds the AWS SDK client lazily — only when at least one
`jwt_keys[]` entry uses an `aws-sm:` reference. Deployments that do not
use AWS Secrets Manager never trigger AWS credential resolution. The SDK
uses the standard AWS credential chain (environment variables, shared
config, IAM role, etc.).

Binary secrets (no `SecretString`) are not supported for JWT key
material. Only text-valued secrets (UTF-8 HMAC secrets or PEM documents)
are accepted. Attempting to resolve a binary-only secret returns an
error at startup.

`App.New` selects the construction path automatically:

- `jwt_keys[]` non-empty: multi-key manager; `jwt_secret` is ignored.
- `jwt_keys[]` empty, `jwt_secret` set: legacy single-secret HS256 manager.
- Both unset: `App.JWT == nil` with a startup `WARN`. Tokens are never
  signed with an empty HMAC key.

For programmatic / non-config use cases:

```go
mgr, err := auth.NewJWTManagerFromKeys([]auth.SigningKey{
    {KID: "2026-q2-rsa", Algorithm: auth.RS256, RSAPrivate: priv},
}, "2026-q2-rsa", 24*time.Hour, "my-issuer")

token, _ := mgr.Generate(userID, username, role)
claims, _ := mgr.Validate(token)
```

Tokens carry a `kid` header identifying the signing key. `Validate`
looks the key up in the keyset, rejecting tokens whose `kid` is
unknown.

To rotate signing keys without invalidating outstanding tokens:

```go
// 1. Add a new key, mark it as current. New tokens are signed with it.
err := mgr.RotateKey(auth.SigningKey{
    KID: "2026-q3-rsa", Algorithm: auth.RS256, RSAPrivate: nextPriv,
}, true)

// 2. Existing tokens (signed with the previous key) keep validating
//    until they expire on their own.

// 3. After the access-token lifetime has passed, drop the old key.
err = mgr.RemoveKey("2026-q2-rsa")
```

`HS256` keys are also supported in the keyset (use `SigningKey.HMACSecret`
instead of `RSAPrivate`); the same rotation primitives apply.

### Module access via Runtime

Fluent modules that need to mint or verify tokens should use the manager the
framework already built from `jwt_secret` / `jwt_keys[]`, rather than
constructing a second `auth.JWTManager` from a duplicated secret. Capture it
once in `OnStart`:

```go
var jwtMgr *auth.JWTManager

var tokenModule = nucleus.Module[struct{}]{
    Name:   "tokens",
    Prefix: "/tokens",
    OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
        jwtMgr = rt.JWT() // *auth.JWTManager; nil when no signing material is configured
        return nil
    },
    Routes: func(r nucleus.Router, _ struct{}) {
        r.Post("/issue", issueToken)
    },
}

func issueToken(c *nucleus.Context) error {
    if jwtMgr == nil {
        return errors.Unauthorized("JWT not configured")
    }
    token, err := jwtMgr.Generate(userID, username, role)
    if err != nil {
        return err
    }
    return c.JSON(http.StatusOK, map[string]string{"token": token})
}
```

`rt.JWT()` returns nil on an unbacked runtime AND when no signing material is
configured (`jwt_secret` unset and `jwt_keys[]` empty). Always guard before
use.

`RotateKey` and `RemoveKey` are operator-level key-lifecycle operations — they
mutate shared state and are not safe to call from per-request module code. Use
them only in admin or startup paths, exactly as with `rt.Authorizer()`'s
in-memory policy mutations.

### JWKS endpoint

Relying parties consuming RS256 tokens (other services, API gateways,
identity proxies) fetch the public key set from a well-known URL.

When at least one RS256 key is present in `jwt_keys[]`, `App.New`
auto-mounts the handler at `/.well-known/jwks.json`. The bootstrap
allow-list already permits anonymous access to that path. No
application code is needed.

For non-default paths or a programmatic manager, mount manually:

```go
a.Router.Get("/.well-known/jwks.json", router.FromHTTP(mgr.JWKSHandler()))
```

The handler emits the standard RFC 7517 / RFC 7518 shape:

```json
{
  "keys": [
    {
      "kid": "2026-q2-rsa",
      "kty": "RSA",
      "alg": "RS256",
      "use": "sig",
      "n": "<base64url(modulus)>",
      "e": "<base64url(exponent)>"
    }
  ]
}
```

`HS256` keys are intentionally excluded from the JWKS response — the
endpoint is public and HMAC keys are shared secrets. Callers using
HS256-only managers will see an empty `keys` array.

## RBAC

`pkg/authz` integrates Casbin. Provide a policy file, and the framework
loads an enforcer accessible from the application:

```yaml
admin_rbac_policy_file: ./auth/policy.csv
```

```go
allowed := a.Authorizer.Can(userID, "articles", "edit") // returns bool
```

The admin panel exposes a UI for policy and role management, backed by
the same enforcer. A superuser bypass is built in for the bootstrap
case.

### Default-deny with deny-override

The built-in Casbin model is **default-deny with deny-override
semantics**:

- A request with **no matching policy** is denied. Operators must
  grant access explicitly.
- A request matching an **allow** rule is permitted — unless a
  matching **deny** rule also exists, in which case it is denied.
  Deny rules always override allows.

The programmatic API mirrors that:

```go
// Grant a role full access to an API surface.
e.AddPolicy("admin", "/api/*", "*")

// Block a specific user from one endpoint, even though their role
// would otherwise allow it.
e.Deny("alice", "/api/users/1", "delete")

// RemovePolicy lifts BOTH the allow and the deny variants matching
// (sub, obj, act) — operators say "stop applying this rule" without
// having to know which effect was originally written.
e.RemovePolicy("alice", "/api/users/1", "delete")
```

CSV policy files now carry an `eft` column. A row reads
`p, <subject>, <object>, <action>, <effect>` where effect is `allow`
or `deny`. Programmatic callers use `AddPolicy` (which stamps `allow`)
and `Deny` to manage policy effects. The Casbin library is an internal
implementation detail of `authz.Enforcer` — its concrete type is not
part of the public API and is not accessible to callers (ADR-015).

### Reading policy state

Three read-only forwarders expose the live ruleset without requiring
access to the underlying Casbin implementation:

```go
// All permission rules as (sub, obj, act, eft) tuples.
rules, err := e.GetPolicy()

// All role-assignment rules as (user, role) tuples.
groupings, err := e.GetGroupingPolicy()

// All role names referenced by a grouping policy.
roles, err := e.GetAllRoles()
```

These are used by the admin RBAC inspector and are available to
application code that needs to audit the live policy (e.g. for display
in a custom UI or an audit log export).

## Authentication middleware

### Session lifecycle — what the framework does for you

The framework mounts the session middleware globally during startup
(`pkg/app/app.go`). Every request that reaches a handler already has
an active session loaded from the store and will have it saved after
the handler returns. **You must not mount the session middleware a
second time** — doing so wraps the session twice and produces
double-commit errors.

Handlers read and write session values through the request context
immediately, using the high-level helpers on `*router.Context` (e.g.
`c.SessionPutString`, `c.SessionGetString`). No extra wiring is needed
for simple key/value use.

For operations that go beyond get/put — `RenewToken` after a successful
login (session-fixation defence), `Destroy`/`Invalidate` on logout,
and flash messaging — modules capture the session manager once in their
`OnStart` hook via `rt.Session()` and call it directly:

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

### Protected routes — understanding the middleware chain

#### How the global gate and module middleware interact

The framework mounts a **global default-deny authorizer** as the last
item in the core middleware chain, before any module routes are
registered (see `pkg/app/app.go`, `r.Use(buildDefaultAuthzMiddleware(...))`).
Module middleware attaches later, inside a chi sub-mux created by
`mountModule` — meaning the global default-deny **always fires before**
any middleware declared in `Module[C].Middleware`.

This has a critical consequence for session-authenticated applications:
when the global gate evaluates a request, no `auth.Claims` have been
injected into the context yet. The enforcer reads the subject via
`auth.ClaimsFromContext`; finding none, it treats the request as
`anonymous` and denies it unless an explicit policy row permits
anonymous access to that path.

**A session-identity bridge placed in `Module.Middleware` cannot
influence the global gate.** There is no pre-authz identity hook today,
and no such hook is promised in a future version.

#### The correct two-layer pattern

Session-authenticated modules use a two-layer composition:

1. **Operator grants reachability** — add policy rows in the
   `admin_rbac_policy_file` that permit the `anonymous` subject (or a
   named bootstrap subject) to reach the module's URL prefix. The
   global default-deny gate will then let those requests through.

   ```csv
   # auth/policy.csv — grant anonymous access to the /auth/* paths
   p, anonymous, /auth/login,  create, allow
   p, anonymous, /auth/logout, create, allow
   ```

   For entirely private surfaces where only authenticated users
   should ever reach (e.g. `/api/admin/*`), the operator grants
   access to the specific roles instead:

   ```csv
   p, admin, /api/admin,  *, allow
   p, admin, /api/admin/*, *, allow
   ```

2. **Module enforces identity and roles** — after the global gate passes
   the request, the module's own middleware chain runs. Place a
   session-to-claims bridge first, then a role guard:

   ```go
   // adminModule holds framework handles captured in OnStart.
   type adminModule struct {
       rt nucleus.Runtime
   }

   func (m *adminModule) build() nucleus.ModuleSpec {
       return nucleus.Module[struct{}]{
           Name:   "admin",
           Prefix: "/api/admin",
           // Module.Middleware entries are constructed before OnStart,
           // so they must close over the module struct, not rt directly.
           Middleware: []nucleus.Middleware{
               m.withIdentity,
               m.requireRole("admin"),
           },
           OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
               m.rt = rt // capture the runtime for per-request use
               return nil
           },
           Routes: func(r nucleus.Router, _ struct{}) {
               r.Get("/stats", adminStats)
           },
       }.Build()
   }

   // withIdentity reads the session-authenticated user ID and role,
   // builds auth.Claims, and injects them so that downstream
   // middleware and handlers can read the subject.
   func (m *adminModule) withIdentity(next http.Handler) http.Handler {
       return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
           sm := m.rt.Session() // *auth.SessionManager
           if sm == nil {
               http.Error(w, "unauthenticated", http.StatusUnauthorized)
               return
           }
           userID := sm.GetString(r.Context(), "user_id")
           if userID == "" {
               http.Error(w, "unauthenticated", http.StatusUnauthorized)
               return
           }
           role := sm.GetString(r.Context(), "role") // stored at login
           ctx := auth.ContextWithClaims(r.Context(), &auth.Claims{
               UserID: userID,
               Role:   role,
           })
           next.ServeHTTP(w, r.WithContext(ctx))
       })
   }

   // requireRole returns middleware that gates the request on the
   // claims injected by withIdentity. It delegates to the runtime
   // enforcer per request so that live policy changes are respected.
   func (m *adminModule) requireRole(roles ...string) nucleus.Middleware {
       return func(next http.Handler) http.Handler {
           return m.rt.Authorizer().RequireRole(roles...)(next)
       }
   }
   ```

   `Module.Middleware` entries are evaluated in registration order
   within the module's sub-mux. `withIdentity` must appear before
   `requireRole` so that `RequireRole` finds claims in the context.

`auth.ContextWithClaims` also propagates the user ID for log
attribution (`observe.CtxWithUserID` is called internally), so
structured logs for the request automatically carry the subject
without extra instrumentation.

### pkg/app users

Applications assembled directly with `pkg/app` (not `pkg/nucleus`) can
compose the same middleware on the Mux. The session middleware is
already mounted globally — only add it to a sub-route if you are
replacing the global mount with a scoped one for a specific reason.
Session middleware must never be mounted twice on the same request path.

```go
// pkg/app-level wiring (not module code)
a.Router.Mux.Route("/api/admin", func(sub *router.Mux) {
    sub.Use(sessionIdentityMiddleware)
    sub.Use(a.Authorizer.RequireRole("admin"))
    // ...
})
```

## CSRF, CORS and rate limiting

These are middleware-level concerns documented in
[Concepts → Routing & middleware](../concepts/routing.md). CORS denies
unknown origins by default and rate limiting is configured from
`nucleus.yml`. CSRF is **opt-in** — it is not auto-mounted. Mount
`router.CSRFMiddleware` explicitly on session-mutating routes such as
login and logout (see
[Routing & middleware → Built-in middleware](../concepts/routing.md)
for the mount pattern).
