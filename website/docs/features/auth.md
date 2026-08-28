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
  - pkg/auth.BackendConfig
  - pkg/auth/backend.Config.Bind
  - pkg/auth/backend.Backend
  - pkg/auth/backend.Config
  - pkg/auth/backend.Register
  - pkg/auth.ChainConfig
  - pkg/auth.NewChainFrom
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
  - pkg/authz.Enforcer.MiddlewareWithOptions
  - pkg/authz.Enforcer.RequireRoleWithOptions
  - pkg/authz.AuthzOptions
  - pkg/authz.AuthzOptions.ResolveSubject
  - pkg/authz.AuthzOptions.ResolveAction
  - pkg/authz.SubjectResolver
  - pkg/authz.ActionResolver
  - pkg/authz.DenialHandler
  - pkg/authz.Denial
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
  - rbac_policy_file
---

# Auth & sessions

Two packages cover this ground: `pkg/auth` handles authentication (sessions,
passwords, JWT) and `pkg/authz` handles authorization (role-based access
control).

This is a long page. Jump to what you need:

- **[Sessions](#sessions)** and **[Password hashing](#password-hashing)** —
  the basics for a login form.
- **[JWT](#jwt)** — stateless auth, from a single secret to a rotating
  keyset with a public JWKS endpoint.
- **[RBAC](#rbac)** — the default-deny policy model, and how to customise
  what happens on denial.
- **[Authentication middleware](#authentication-middleware)** — how the
  global gate and your module's middleware fit together. Read this one if
  your session-authenticated routes return 403.

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

## JWT

`pkg/auth` exposes a `JWTManager` for stateless auth. It has two modes, which
can coexist in the same process:

- **Single secret, HS256** — one shared secret, no key IDs. Good for getting
  started.
- **Multi-key keyset** — several keys with rotation and a public JWKS
  endpoint. This is the production path.

### Single-secret HS256 (quick start)

```go
mgr := auth.NewJWTManager(secret, 24*time.Hour, "my-issuer")

token, err := mgr.Generate(userID, username, role)
claims, err := mgr.Validate(token)
```

The secret comes from the `jwt_secret` config key. Set it through the
`NUCLEUS_JWT_SECRET` environment variable rather than writing it into
`nucleus.yml` — config files end up committed.

`jwt_secret` is a non-nullable security key: setting it to `null`, or
exporting an empty `NUCLEUS_JWT_SECRET`, is a boot error rather than a silent
fall-back to no secret.

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

The AWS SDK client is built lazily — only when at least one `jwt_keys[]`
entry uses an `aws-sm:` reference. Deployments that do not use AWS Secrets
Manager never trigger AWS credential resolution. The SDK uses the standard
AWS credential chain: environment variables, shared config, IAM role, and so
on.

Only text-valued secrets are accepted — UTF-8 HMAC secrets or PEM documents.
Binary secrets (those with no `SecretString`) are not supported for JWT key
material, and resolving one fails at startup.

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

If your module mints or verifies tokens, use the manager the framework already
built from `jwt_secret` / `jwt_keys[]`. Do not construct a second
`auth.JWTManager` from a duplicated secret. Capture it once in `OnStart`:

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
rbac_policy_file: ./auth/policy.csv
```

```go
allowed := a.Authorizer.Can(userID, "articles", "edit") // returns bool
```

The enforcer is available to all extension modules. A superuser bypass is
built in for the bootstrap case.

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

CSV policy files carry an `eft` column, so a row reads
`p, <subject>, <object>, <action>, <effect>` where the effect is `allow` or
`deny`. From code, `AddPolicy` stamps `allow` and `Deny` writes the deny
variant.

Casbin itself is an implementation detail of `authz.Enforcer`. Its concrete
type is not part of the public API and callers cannot reach it, which is what
keeps Casbin replaceable without breaking your code.

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

These are available to application code that needs to audit the live
policy (e.g. for display in a custom UI or an audit log export).

### SSR-friendly denial handling

By default, `Middleware()` and `RequireRole(...)` write a JSON error envelope
on denial — 401 or 403. That is the right answer for an API, but not for a
server-rendered site, where an anonymous visitor should be redirected to a
login page and a signed-in user lacking the role should see a styled error
page.

To replace the default behaviour, use `MiddlewareWithOptions` and
`RequireRoleWithOptions`.

```go
import (
    "net/http"
    "github.com/jcsvwinston/nucleus/pkg/authz"
)

onDeny := authz.DenialHandler(func(w http.ResponseWriter, r *http.Request, d authz.Denial) {
    if !d.Authenticated {
        // Anonymous visitor — redirect to login.
        http.Redirect(w, r, "/auth/login", http.StatusFound)
        return
    }
    // Signed-in user without the required role — show a 403 page.
    http.Error(w, "Access denied", http.StatusForbidden)
})

// Global policy gate — SSR variant.
router.Use(enforcer.MiddlewareWithOptions(authz.AuthzOptions{OnDeny: onDeny}))

// Role guard on a single route — SSR variant.
router.With(enforcer.RequireRoleWithOptions(authz.AuthzOptions{OnDeny: onDeny}, "admin")).
    Get("/admin/dashboard", adminDashboard)
```

`authz.Denial` carries three fields set by the middleware before
calling `OnDeny`:

| Field           | Type   | Meaning                                                                                    |
| --------------- | ------ | ------------------------------------------------------------------------------------------ |
| `Status`        | `int`  | HTTP status the default path would use: 401 (no identity) or 403 (insufficient role).     |
| `Authenticated` | `bool` | `false` when the visitor is anonymous; `true` when signed in but lacking role/permission. |
| `Reason`        | `string` | Human-readable explanation, e.g. `"insufficient role"`.                                 |

The `OnDeny` handler owns the response — it must write a status and body
and must not call the next handler. Passing a nil `OnDeny` (the zero
`AuthzOptions`) reproduces the default JSON envelope exactly, so existing
callers are unaffected.

### Subject and action resolvers

`AuthzOptions` carries two additional optional fields that change _what_
the middleware checks, rather than just how it responds on denial.

**`ResolveSubject`** (`authz.SubjectResolver`) overrides the policy subject.

Its signature is `func(r *http.Request, claims *auth.Claims) string`, and the
default returns `claims.UserID`. If your policy table is keyed by role rather
than by individual user, return `claims.Role` instead — the enforcer then
looks the role up directly in the policy CSV.

**`ResolveAction`** (`authz.ActionResolver`) overrides the policy action.

Its signature is `func(r *http.Request) string`, and the default maps the HTTP
method: GET→`read`, POST→`create`, PUT/PATCH→`update`, DELETE→`delete`.
Pure-HTML forms can only POST, so a resolver can inspect the URL path and map
a POST ending in `/delete` to the `delete` action, making deny-override rules
fire correctly.

Both resolvers default to `nil`, which is the standard behaviour — callers
that set only `OnDeny` are unaffected.

Here is a combined server-rendered setup: role-keyed policies, path-aware
action mapping, and redirect-on-denial.

```go
import (
    "net/http"
    "strings"
    "github.com/jcsvwinston/nucleus/pkg/auth"
    "github.com/jcsvwinston/nucleus/pkg/authz"
)

opts := authz.AuthzOptions{
    // Check policies keyed by role, not by individual user ID.
    ResolveSubject: authz.SubjectResolver(func(r *http.Request, c *auth.Claims) string {
        return c.Role
    }),
    // Map pure-HTML form POSTs to the correct action.
    // Default: GET/HEAD→"read", POST→"create", PUT/PATCH→"update", DELETE→"delete".
    ResolveAction: authz.ActionResolver(func(r *http.Request) string {
        if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/delete") {
            return "delete"
        }
        return "create" // treat other POSTs as create
    }),
    // Redirect anonymous visitors; show a styled page for signed-in denials.
    OnDeny: authz.DenialHandler(func(w http.ResponseWriter, r *http.Request, d authz.Denial) {
        if !d.Authenticated {
            http.Redirect(w, r, "/auth/login", http.StatusFound)
            return
        }
        http.Error(w, "Access denied", http.StatusForbidden)
    }),
}

router.Use(enforcer.MiddlewareWithOptions(opts))
```

Two limits are worth knowing. `ResolveSubject` is honoured by
`MiddlewareWithOptions` only — `RequireRole` and `RequireRoleWithOptions`
match the claim's role directly and ignore it. And a resolver that returns an
empty string produces a policy query matching nothing: the request is denied
under default-deny, and a warning is logged so the misconfiguration is
auditable.

## Authentication middleware

### Session lifecycle — what the framework does for you

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

### Protected routes — understanding the middleware chain

#### How the global gate and module middleware interact

The framework mounts a **global default-deny authorizer** as the last item in
the core middleware chain, before any module routes are registered. Your
module's middleware attaches later, inside the module's own sub-router. So the
global default-deny **always fires first** — before anything declared in
`Module[C].Middleware`.

For session-authenticated applications that has one critical consequence: when
the global gate evaluates a request, no `auth.Claims` are in the context yet.
The enforcer looks for a subject, finds none, treats the request as
`anonymous`, and denies it — unless a policy row explicitly permits anonymous
access to that path.

**A session-identity bridge placed in `Module.Middleware` cannot influence the
global gate.** There is no pre-authz identity hook today, and none is promised
for a future version.

#### The correct two-layer pattern

Session-authenticated modules use a two-layer composition:

1. **Operator grants reachability** — add policy rows in the
   `rbac_policy_file` that permit the `anonymous` subject (or a
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

#### Module-declared policy rows (`Module.Policies`)

A module can carry the policy rows its own routes need, so mounting it
works without the operator editing `rbac_policy.file` by hand:

```go
nucleus.Module[struct{}]{
    Name: "articles",
    Policies: []nucleus.PolicyRule{
        // Same shape as a rbac_policy.csv row: subject, object, action, effect.
        {Subject: "anonymous", Object: "/articles", Action: "read"},
        {Subject: "anonymous", Object: "/articles", Action: "create"},
        {Subject: "anonymous", Object: "/articles/*", Action: "read"},
    },
    // JSON API paths the module needs exempted from CSRF protection.
    // Same raw-prefix matching as csrf_exempt_paths.
    CSRFExempt: []string{"/articles"},
    Routes: func(r nucleus.Router, _ struct{}) { /* … */ },
}
```

## Authenticating against a directory

Authentication backends are selected **by name**, so a backend lives in its
own module and you import it. The framework itself carries no LDAP client,
no SAML service provider and no OIDC client — an application that does not
authenticate against any of them should not download them.

### LDAP

LDAP ships with Nucleus, as a separate module:

```bash
go get github.com/jcsvwinston/nucleus/providers/ldap
```

Import it for its side effect — the shape `database/sql` drivers use — and
name it in the chain:

```go
import _ "github.com/jcsvwinston/nucleus/providers/ldap"
```

```yaml
# nucleus.yml
auth_backends: [ldap, local]

auth:
  ldap:
    url: "ldaps://dc.corp.local:636"
    base_dn: "ou=people,dc=corp,dc=local"
    bind_dn: "cn=svc-nucleus,ou=services,dc=corp,dc=local"
    bind_password: "${LDAP_BIND_PASSWORD}"
```

Every setting the backend takes is documented in [its README][ldap-readme],
along with what it refuses to do — an empty password is rejected before a
connection is opened, a username is filter-escaped before it reaches the
query, an ambiguous search is a rejection rather than a coin toss, and only
a directory that actually says "wrong credentials" produces a rejection.

[ldap-readme]: https://github.com/jcsvwinston/nucleus/tree/main/providers/ldap

Naming a backend you have not imported fails at boot with the two lines
that fix it — the `go get` and the `import`. It is not left for the first
person who tries to log in.

### Anything else

The same seam is open to you:

```go
package acmeauth

import "github.com/jcsvwinston/nucleus/pkg/auth/backend"

func init() {
    backend.Register("acme", New)
}
```

Import `pkg/auth/backend`, not `pkg/auth`. It is a leaf package holding the
contract and nothing else — the interface, the identity it returns, the two
sentinel errors, the configuration subtree and the registry — so writing a
backend does not drag in session stores, JWT, Redis, Prometheus and
OpenTelemetry to implement two methods. The names remain available from
`pkg/auth` as aliases, so existing code keeps compiling.

A backend answers one question — do these credentials belong to a real
user — and returns one of three things: the user, `ErrInvalidCredentials`
when the answer is certainly no, or `ErrBackendUnavailable` when it could
not reach the directory at all.

That third case is why backends are consulted as an **ordered chain**
rather than a set:

```yaml
auth_backends: [ldap, local]
```

The directory answers first; the local user table answers second. When the
directory is unreachable, the chain records it and moves on — so the
break-glass account you keep for exactly that morning still works. A set
could not express that, and neither could a single configured backend.

The distinction survives to the end. If every backend rejected, the caller
gets "invalid credentials", because that is what happened. If any backend
was unreachable, the caller gets an error saying which — "wrong password"
and "the directory is down" send an operator to very different places, and
guessing between them wastes the hour that matters.

### Each backend reads its own settings

A backend named in `auth_backends` owns the `auth.<name>.*` subtree. The
framework validates only that the section belongs to a registered name and
hands the contents over; the backend declares their shape and validates
them:

```go
func New(bc auth.BackendConfig) (auth.Backend, error) {
    var cfg struct {
        URL     string        `koanf:"url" validate:"required"`
        Timeout time.Duration `koanf:"timeout" default:"5s"`
    }
    if err := bc.Bind(&cfg); err != nil {
        return nil, err
    }
    // …
}
```

Two things fail rather than pass quietly:

- **A key the backend does not declare.** A misspelled directory URL would
  otherwise sit unnoticed until the day the setting mattered.
- **A section for a backend the chain does not name.** `auth.ldap.*`
  without `ldap` in `auth_backends` is read by nobody — the chain is its
  only consumer — so it boots clean and the login page never consults the
  directory you configured. There is no reading of that file under which it
  does something, so it is an error.

A misspelled section under `auth.` is still an unknown key, exactly as
before: the exemption is per registered name, never for the namespace.

Your own user table joins that list like anything else. Implement
`auth.UserProvider` — the interface that describes how to reach your users
— and register it:

```go
nucleus.New().
    FromConfigFile("nucleus.yml").
    WithUserProvider(myUserStore)
```

It appears in the chain as `local` (or under a name you choose with
`WithUserProviderNamed`). Modules reach the assembled chain through
`rt.AuthChain()`, so a module that owns a sign-in page authenticates
through the order the operator declared rather than going straight to the
user table — the point of declaring an order is that it applies everywhere.

A backend named in `auth_backends` that nobody registered fails at **boot**,
naming what is registered — and, when it is one Nucleus publishes, naming
the `go get` and the import that would register it. A typo in an
authentication list should not wait until the first person tries to log in.

`nucleus doctor --check auth` reviews the chain from the configuration
alone: a backend declared with no settings of its own, and a chain whose
every entry depends on an outside system — which is the deployment where
nobody can log in on the morning the directory is down, including whoever
would fix it.

One rule for anyone writing a backend: reject an unknown user and a wrong
password **identically**, and in the same time. A backend that answers
faster for a user that does not exist has published a list of your users,
and because the chain stops on rejection, it publishes it for every
backend behind it too.

The contract, precisely:

- **Objects are relative to the module's `Prefix`** (a module without a
  prefix declares full paths); keyMatch wildcards work (`/articles/*`).
- **To declare the module's own root**, use `Object: "/"` — it means the
  root *and* the subtree, and emits both rows. Use `Object: ""` for the
  root alone, when the landing page is public but what hangs off it is
  not. The distinction exists because the enforcer matches with keyMatch,
  where `/articles` and `/articles/` are different paths and neither
  implies the other.
- **`Action` is a CRUD verb** (`read`|`create`|`update`|`delete`) or
  `*` — not an HTTP method. `Effect` is `allow` (the default when
  empty) or `deny`.
- **The operator always wins.** Module rows join the live in-memory
  ruleset only — the policy file is never written — and the Casbin
  policy effect (`some(allow) && !some(deny)`) means a `deny` row in
  the host's CSV overrides any module `allow`.
- **Malformed declarations fail boot** with `ErrInvalidModulePolicy`, naming
  the module and the entry. A row that was silently skipped would leave its
  route answering a mute 403.
- `CSRFExempt` entries are declarative (not a closure) because the
  exemption list is frozen inside the middleware stack at `app.New`,
  before any module closure runs — the same constraint behind the
  automatic webhook-prefix exemption. `"/"` (or `""`) means the module's
  own surface and resolves to the bare prefix, which the raw-prefix
  matcher extends over everything below it — including a POST to the
  collection path itself.
- **A module cannot exempt the whole application.** An exemption that
  resolves to `"/"` — which is what a module *without* a `Prefix`
  declaring `"/"` would mean — fails boot. Mounting a module is trusting
  its routes; it is not agreeing to let it unprotect its siblings.
- **Exemptions are logged at boot**, module by module, with the resolved
  paths. They are the one declaration that *removes* a protection, so they
  leave the loudest trace.

Rows that depend on the module's bound config can still be added
imperatively from `OnStart` via `rt.Authorizer().AddPolicy(...)`; the
declarative field is for the common case and gets boot-time validation.

### Applications built directly on `pkg/app`

Applications assembled with `pkg/app` rather than `pkg/nucleus` compose the
same middleware on the Mux. The session middleware is already mounted
globally; add it to a sub-route only if you are deliberately replacing the
global mount with a scoped one. It must never be mounted twice on the same
request path.

```go
// pkg/app-level wiring (not module code)
a.Router.Mux.Route("/api/admin", func(sub *router.Mux) {
    sub.Use(sessionIdentityMiddleware)
    sub.Use(a.Authorizer.RequireRole("admin"))
    // ...
})
```

## CSRF, CORS and rate limiting

These are middleware-level concerns, documented in
[Concepts → Routing & middleware](../concepts/routing.md). In short: CORS
denies unknown origins by default, and rate limiting is configured from
`nucleus.yml`.

CSRF is **opt-in** — it is not auto-mounted. Mount `router.CSRFMiddleware`
explicitly on session-mutating routes such as login and logout (see
[Routing & middleware → Built-in middleware](../concepts/routing.md)
for the mount pattern).
