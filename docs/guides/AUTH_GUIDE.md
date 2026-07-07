# Authentication & Authorization Guide

Reference date: 2026-06-21.
Status: Current.

This guide covers Nucleus's authentication (`pkg/auth`) and authorization (`pkg/authz`) systems, including JWT flows, session management, password handling, and Casbin-backed policy enforcement.

## Table of Contents

- [Overview](#overview)
- [Authentication (`pkg/auth`)](#authentication-pkgauth)
  - [JWT Authentication](#jwt-authentication)
  - [Server-Side Sessions](#server-side-sessions)
  - [Password Hashing](#password-hashing)
  - [User Model](#user-model)
- [Authorization (`pkg/authz`)](#authorization-pkgauthz)
  - [Casbin Integration](#casbin-integration)
  - [Policy Files](#policy-files)
  - [Authorization Middleware](#authorization-middleware)
- [Admin Authentication Flow](#admin-authentication-flow)
- [Production Checklist](#production-checklist)

---

## Overview

Nucleus provides two complementary authentication mechanisms:

| Mechanism | Best For | Storage Options |
|-----------|----------|-----------------|
| **JWT** | Stateless APIs, mobile apps, microservices | None (token-only) |
| **Sessions** | Server-rendered apps, admin panel, web UIs | Memory, SQL, Redis |

Authorization is handled separately by `pkg/authz`, which integrates with Casbin for policy-based access control.

---

## Authentication (`pkg/auth`)

### JWT Authentication

JWT (JSON Web Token) authentication is ideal for stateless API endpoints.

#### Configuration

```yaml
# nucleus.yml
jwt_secret: your-super-secret-key-change-in-production
jwt_expiry: 24h
jwt_issuer: myapp
```

#### Generating Tokens

```go
import "github.com/jcsvwinston/nucleus/pkg/auth"

// Create a JWT manager
manager := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTExpiry, cfg.JWTIssuer)

// Generate token for a user (userID, username, role)
token, err := manager.Generate("user-123", "alice", "admin")
if err != nil {
    return err
}

// Return to client
return ctx.JSON(200, map[string]string{"access_token": token})
```

#### Validating Tokens (Middleware)

Nucleus's router includes JWT middleware that validates tokens and enriches the request context:

```go
import "github.com/jcsvwinston/nucleus/pkg/router"

r := router.New()

// Protect routes with JWT middleware
r.Use(manager.Middleware())

// Handlers can access user context
r.GET("/api/profile", func(w http.ResponseWriter, r *http.Request) {
    userID := observe.UserIDFromCtx(r.Context())
    requestID := observe.RequestIDFromCtx(r.Context())
    traceID := observe.TraceIDFromCtx(r.Context())

    // JWT middleware enriches context with these values
    ctx := r.Context()
    // ... use context values
})
```

#### Token Refresh Flow

For security-sensitive applications, implement short-lived access tokens with refresh tokens. Use two managers with different expiries — one for short-lived access tokens, one for long-lived refresh tokens stored server-side:

```go
// Access token manager: short-lived (15m)
accessManager := auth.NewJWTManager(cfg.JWTSecret, 15*time.Minute, "myapp")

// Refresh token manager: long-lived (7d), refresh tokens stored server-side
refreshManager := auth.NewJWTManager(cfg.JWTSecret, 7*24*time.Hour, "myapp")

// Generate both tokens on login (userID, username, role)
accessToken, _ := accessManager.Generate("user-123", "alice", "admin")
refreshToken, _ := refreshManager.Generate("user-123", "alice", "admin")

// Persist the refresh token (or its ID) server-side so it can be revoked.
```

#### Token Revocation

JWTs are stateless by design. To revoke tokens before expiry:

1. **Short expiry + refresh tokens**: Use 15-minute access tokens; revoke refresh tokens server-side.
2. **Token blacklist**: Store revoked JWT IDs (`jti`) in Redis with TTL matching token expiry.
3. **Version field**: Add a `token_version` claim; increment on password change/logout.

#### Supported algorithms

| Algorithm | Key material | Notes |
|-----------|--------------|-------|
| `HS256`   | shared HMAC secret via `secret_env` | symmetric — never published in JWKS |
| `RS256`   | RSA private key via `pem_path` / `pem_env` (PKCS#1 or PKCS#8) | published in JWKS as `kty: RSA` |
| `ES256`   | ECDSA **P-256** private key via `pem_path` / `pem_env` (SEC1 or PKCS#8) | published in JWKS as `kty: EC`, `crv: P-256` |

ES256 is the modern asymmetric choice — smaller keys and signatures than RS256 at equivalent security. Only the P-256 curve is supported; a P-384/P-521 key with `algorithm: ES256` fails fast at `App.New` (see ADR-005). The same `JWTManager` instance handles all three algorithms simultaneously, so a keyset can mix HS256, RS256 and ES256 entries for staged migration.

#### Key Rotation with Multiple Active Keys

`JWTManager` supports a keyset with explicit `kid` headers so secrets / asymmetric keys can be rotated without invalidating outstanding tokens.

**Config-driven setup (recommended)** — `App.New` builds `App.JWT` automatically from `nucleus.yml` when `jwt_keys[]` is non-empty. Operators do not call `auth.NewJWTManagerFromKeys` themselves unless they have a non-config use case (e.g. dynamic key loading at runtime).

```yaml
# nucleus.yml
jwt_issuer: myapp
jwt_current_kid: 2026-q2-ec
jwt_keys:
  - kid: 2026-q2-ec
    algorithm: ES256
    pem_path: /run/secrets/jwt-ec-q2.pem
  - kid: 2026-q1-rsa
    algorithm: RS256
    pem_env: JWT_RSA_Q1_PEM
  - kid: legacy-hs
    algorithm: HS256
    secret_env: JWT_LEGACY_SECRET
```

#### Key material references (`secret_env` / `pem_env`)

`secret_env` and `pem_env` are **resolver references**, not just raw env-var names. The value is dispatched by scheme:

| Reference form        | Resolved from                          |
|-----------------------|-----------------------------------------|
| `MY_VAR`              | environment variable `MY_VAR`            |
| `env:MY_VAR`          | environment variable `MY_VAR` (explicit) |
| `aws-sm:<secret-id>`  | AWS Secrets Manager secret `<secret-id>` |
| `aws-sm:<id>#<key>`   | one JSON key out of a JSON-object secret |

`pem_path` is unchanged — it is always a filesystem path.

The AWS Secrets Manager resolver is **lazy**: the AWS SDK client is only constructed when at least one `jwt_keys[]` entry uses an `aws-sm:` reference. Deployments that stick to env vars / files never trigger AWS credential resolution. The resolver uses the standard AWS credential chain (env vars, shared config, IAM role). Example:

```yaml
jwt_keys:
  - kid: 2026-q2-ec
    algorithm: ES256
    # The signing key lives in AWS Secrets Manager, never on disk.
    pem_env: aws-sm:prod/nucleus/jwt-signing-key
  - kid: 2026-q2-hmac
    algorithm: HS256
    # One field of a JSON-object secret.
    secret_env: aws-sm:prod/nucleus/jwt-secrets#current
```

Only P-256 / RS256 / HS256 are in scope for `v0.7.x`; GCP Secret Manager, Azure Key Vault and HashiCorp Vault resolvers are future work tracked under ADR-005.

`App.New` selects the construction path based on config:

- `jwt_keys[]` non-empty: builds a multi-key manager via `auth.NewJWTManagerFromKeys`; `jwt_secret` is ignored.
- `jwt_keys[]` empty and `jwt_secret` set: builds a legacy single-secret HS256 manager.
- Both empty/unset: `App.JWT == nil` and a startup `WARN` is emitted. The framework does not sign tokens with an empty HMAC key (that would forge globally-known signatures).

**Programmatic setup** (non-config use cases):

```go
mgr, err := auth.NewJWTManagerFromKeys([]auth.SigningKey{
    {KID: "2026-q2-rsa", Algorithm: auth.RS256, RSAPrivate: priv},
}, "2026-q2-rsa", 24*time.Hour, "my-issuer")

token, _ := mgr.Generate(userID, username, role)   // header carries kid
claims, _ := mgr.Validate(token)                    // looks key up by kid
```

Operator rotation flow (zero-downtime):

```go
// 1. Add a new key, mark it current. New tokens are signed with it.
err := mgr.RotateKey(auth.SigningKey{
    KID: "2026-q3-rsa", Algorithm: auth.RS256, RSAPrivate: nextPriv,
}, true)

// 2. Existing tokens (signed with the previous key) keep validating
//    until they expire on their own.

// 3. After the access-token lifetime elapses, drop the old key.
err = mgr.RemoveKey("2026-q2-rsa")
```

`RemoveKey` refuses to remove the current signing key — promote a different key first. Tokens whose `kid` is unknown are rejected with an explicit error; algorithm mismatch (`kid` says HS256, token is RS256) is also rejected.

The legacy `auth.NewJWTManager(secret, expiry, issuer)` constructor is unchanged. Tokens it issues carry no `kid` and validate against the single secret — useful for quick starts and tests; multi-key mode is the recommended path for production.

#### JWKS Endpoint

Relying parties consuming RS256 or ES256 tokens (other services, API gateways, identity proxies) typically fetch the public key set from a well-known URL.

**Auto-mounted** — when at least one asymmetric key (RS256 or ES256) is present in `jwt_keys[]`, `App.New` mounts the handler at `/.well-known/jwks.json` automatically. The ADR-004 bootstrap allow-list already reserves that path so the default-deny middleware does not gate it. No application code is needed.

**Manual mount** (non-default path or programmatic manager only):

```go
a.Router.Get(
    "/.well-known/jwks.json",
    router.FromHTTP(mgr.JWKSHandler()),
)
```

The handler emits one entry per asymmetric key — `kty: RSA` for RS256, `kty: EC` for ES256:

```json
{
  "keys": [
    {
      "kid": "2026-q1-rsa",
      "kty": "RSA",
      "alg": "RS256",
      "use": "sig",
      "n": "<base64url(modulus)>",
      "e": "<base64url(exponent)>"
    },
    {
      "kid": "2026-q2-ec",
      "kty": "EC",
      "alg": "ES256",
      "use": "sig",
      "crv": "P-256",
      "x": "<base64url(32-byte X coordinate)>",
      "y": "<base64url(32-byte Y coordinate)>"
    }
  ]
}
```

ES256 coordinates are emitted as the fixed-length (32-byte, left-padded) big-endian form required by RFC 7518 §6.2 — not the minimal-length `big.Int` output. HMAC keys are **intentionally excluded** from the JWKS response — the endpoint is public and HMAC keys are shared secrets. An HS256-only manager returns `{"keys": []}`. `JWKS()` returns the same `JWKSet` data structure for callers that need to embed it in an OIDC discovery document.

Content-Type is `application/jwk-set+json; charset=utf-8` with a 5-minute `Cache-Control` hint so relying-party clients cache reasonably.

### Server-Side Sessions

Sessions are required for server-rendered applications, admin panel, and CSRF-protected forms.

#### Configuration

```yaml
# nucleus.yml
session_store: sql          # Options: memory, sql, redis
session_cookie_name: nucleus_session
session_cookie_secure: true # Default: true (HTTPS only). Set false only for plain-HTTP dev/test environments.
session_cookie_samesite: strict
session_idle_timeout: 30m
session_table: nucleus_sessions
```

#### Store Backends

| Store | Use Case | Configuration |
|-------|----------|---------------|
| **Memory** | Development, single-instance testing | `session_store: memory` |
| **SQL** | Production, multi-replica without Redis | `session_store: sql`, `session_table: nucleus_sessions` |
| **Redis** | High-scale, distributed sessions | `session_store: redis`, `session_redis_url: redis://localhost:6379/0` |

#### Session Usage

```go
import "github.com/jcsvwinston/nucleus/pkg/auth"

// The session manager is created once at wiring time (app.New does this
// for you) and shared across handlers; its methods take the request context.
session := auth.NewSessionManager(cfg)

// Set session data
session.Put(r.Context(), "user_id", "user-123")
session.Put(r.Context(), "role", "admin")

// Get session data
userID := session.GetString(r.Context(), "user_id")
if userID == "" {
    http.Redirect(w, r, "/login", http.StatusSeeOther)
    return
}

// Destroy session (logout)
session.Destroy(r.Context())
```

#### Session Runtime Metadata

Nucleus automatically enriches sessions with serving-node identity for cluster diagnostics:

```go
// Session metadata includes:
// - first_seen: When session was created
// - last_seen: Last activity timestamp
// - pod: Pod/container identifier
// - host: Hostname
// - instance: Process instance ID
```

View active sessions via admin UI at `/admin#/sessions` or API at `GET /admin/api/sessions`.

#### Session Maintenance Commands

```bash
# Create SQL session table (if using sql store)
nucleus createcachetable --config nucleus.yml

# Clear expired sessions (production-safe)
nucleus clearsessions --config nucleus.yml

# Clear all sessions (use with caution)
nucleus clearsessions --all --force --config nucleus.yml
```

### Password Hashing

Nucleus uses `bcrypt` for password hashing via `golang.org/x/crypto/bcrypt`.

```go
import "github.com/jcsvwinston/nucleus/pkg/auth"

// Hash a password (use default cost=10 in production)
hash, err := auth.HashPassword("plaintext_password")
if err != nil {
    return err
}

// Verify a password
isValid := auth.CheckPassword("plaintext_password", hash)
if !isValid {
    return fmt.Errorf("invalid credentials")
}
```

`HashPassword` uses bcrypt at the library default cost; `CheckPassword` returns a single `bool` (no error) so a hash/no-match comparison is a one-liner.

**Security recommendations:**

- Never log or store plaintext passwords.
- `HashPassword` applies the bcrypt default cost (10), which suits most applications.
- Implement rate limiting on login endpoints.

### User Model

Nucleus provides a minimal user structure in `pkg/auth`:

```go
type User struct {
    ID          string
    Username    string
    Email       string
    Role        string
    IsSuperuser bool
}
```

Admin users are managed via CLI commands:

```bash
# Create admin user
nucleus createuser --config nucleus.yml --username admin --email admin@example.com

# Interactive password prompt
nucleus createuser --config nucleus.yml --username admin

# Non-interactive (CI/CD)
nucleus createuser --config nucleus.yml --username admin --password "secure-password" --no-input

# Change password
nucleus changepassword --config nucleus.yml --username admin
nucleus changepassword --config nucleus.yml --username admin --password "new-password" --no-input
```

---

## Authorization (`pkg/authz`)

### Default-deny mount (ADR-004)

`App.New` mounts the Casbin enforcer and its default-deny middleware
on the router by default. There is no config switch to disable it —
the only escape hatch is `app.WithOpenAuthz()`, which requires
touching code and surfaces in PR review.

This means the framework's baseline is:

- A request matching a framework-owned bootstrap route (`/healthz`,
  `/metrics`, `/login`, `/.well-known/jwks.json`, `/static/*`) responds
  normally — `App.New` seeds the anonymous allow for those paths before
  mounting the middleware.
- Any **other** request returns `403 Forbidden` until the operator
  loads policies via `rbac_policy_file` or calls
  `App.Authorizer.AddPolicy` programmatically.

> **Note:** In earlier versions, `App.New` also seeded the allow for the
> configured `admin_prefix` (e.g. `/admin`, `/admin/*`) and the `/_/config`
> endpoint. Both were removed in ADR-019 (2026-06-21) when the in-core admin
> panel was extracted to the `orbit` module. The orbit module is responsible
> for seeding its own bootstrap allow entries when mounted.

When no user policies are loaded, `App.New` emits a startup `WARN`:

```
authz: no user policies loaded; only bootstrap routes will respond — 
set rbac_policy_file or call App.Authorizer.AddPolicy programmatically, 
or pass app.WithOpenAuthz() to skip enforcement entirely (see ADR-004).
```

The middleware uses `authz.BootstrapSubject` (literal `"anonymous"`)
as the subject when a request carries no JWT claims, so operators
write policies for anonymous access exactly like they would for any
user:

```go
// Grant unauthenticated access to the public API surface.
a.Authorizer.AddPolicy("anonymous", "/api/public/*", "*")
```

#### Opt-out: `WithOpenAuthz()`

For early development, internal tooling, or demos where every
endpoint is intentionally unauthenticated, pass the option at
construction:

```go
a, err := app.New(cfg, app.WithOpenAuthz())
```

`App.New` then skips mounting the middleware entirely, emits a
startup `WARN` flagging the choice, but **still constructs**
`App.Authorizer` so RBAC-protected paths (such as the mounted orbit
admin module's) keep working. The option is deliberately not exposed as a config flag —
opting out of default-deny is meant to be a deliberate code change
visible in `git blame`.

### Casbin Integration

Nucleus integrates with [Casbin](https://casbin.org/) for policy-based authorization.

#### Configuration

```yaml
# nucleus.yml
authz_model_path: internal/config/authz_model.conf
authz_policy_path: internal/config/authz_policy.csv
```

#### Model File (`authz_model.conf`)

Define your access control model:

```ini
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act, eft

[policy_effect]
e = some(where (p.eft == allow)) && !some(where (p.eft == deny))

[matchers]
m = g(r.sub, p.sub) && keyMatch(r.obj, p.obj) && (r.act == p.act || p.act == "*")
```

This is **default-deny with deny-override**:

- A request with no matching policy is denied (the `some allow` half fails).
- A request matching an `allow` rule is permitted — **unless** a matching `deny` rule also exists, in which case it is denied. Deny rules always override allows.

#### Policy File (`authz_policy.csv`)

Policies now include a 4th column (`allow` or `deny`):

```csv
p, admin, /admin/*, *, allow
p, admin, /api/*, *, allow
p, editor, /api/articles, POST, allow
p, editor, /api/articles/*, PUT, allow
p, editor, /api/articles/*, DELETE, allow
p, viewer, /api/*, GET, allow
p, anonymous, /api/health, GET, allow
p, anonymous, /login, GET, allow
p, anonymous, /login, POST, allow

# Explicit deny: block one user from a destructive endpoint
# even though their role normally allows it.
p, alice, /api/users/*, DELETE, deny
```

Programmatic callers use `AddPolicy` and `Deny`:

```go
e.AddPolicy("admin", "/api/*", "*")              // auto-stamps allow
e.Deny("alice", "/api/users/1", "delete")         // explicit deny override
e.RemovePolicy("alice", "/api/users/1", "delete") // removes both effects
```

`RemovePolicy` is symmetric: a single call drops both the allow and deny variants matching `(sub, obj, act)`. Operators say "stop applying this rule" without having to remember which effect was originally written.

#### Enforcer Usage

```go
import "github.com/jcsvwinston/nucleus/pkg/authz"

// Initialize enforcer (logger, then zero or more policy file paths)
enforcer, err := authz.New(logger, cfg.RBACPolicyFile)
if err != nil {
    return err
}

// Check permissions — Can returns a single bool (subject, object, action)
if !enforcer.Can("alice", "/api/articles", "GET") {
    return fmt.Errorf("forbidden")
}
```

#### Authorization Middleware

Apply authorization middleware to routes:

```go
import "github.com/jcsvwinston/nucleus/pkg/authz"

// Role-based middleware (method on the enforcer)
r.Use(enforcer.RequireRole("admin", "editor"))

// Or mount the default-deny enforcer middleware, which derives the
// subject from the request's JWT claims and the object/action from the
// path and method automatically.
r.Use(enforcer.Middleware())

// Custom middleware with dynamic subject
r.Use(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        role := observe.UserIDFromCtx(r.Context()) // Or extract from JWT claims
        if !enforcer.Can(role, r.URL.Path, r.Method) {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }
        next.ServeHTTP(w, r)
    })
})
```

#### SSR-Friendly Denial Handling

By default, `Middleware` and `RequireRole` answer a rejection with a JSON error
envelope (`401 Unauthorized` or `403 Forbidden`). For server-rendered
applications that need to redirect anonymous visitors to a login page or render
a styled 403 page for authenticated users who lack the required role, use the
`WithOptions` variants and supply an `authz.DenialHandler` via `AuthzOptions`.

```go
import "github.com/jcsvwinston/nucleus/pkg/authz"

// DenialHandler that redirects anonymous visitors and renders a 403 for
// authenticated users who lack the required role.
onDeny := func(w http.ResponseWriter, r *http.Request, d authz.Denial) {
    if !d.Authenticated {
        http.Redirect(w, r, "/login", http.StatusFound)
        return
    }
    // d.Authenticated == true: signed in but missing role/permission.
    w.WriteHeader(http.StatusForbidden)
    // Render a styled 403 page here, e.g. using html/template.
}

// Role check — SSR variant
r.Use(enforcer.RequireRoleWithOptions(authz.AuthzOptions{OnDeny: onDeny}, "admin"))

// Policy enforcer — SSR variant
r.Use(enforcer.MiddlewareWithOptions(authz.AuthzOptions{OnDeny: onDeny}))
```

The `authz.Denial` value passed to the handler carries three fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `Status` | `int` | HTTP status the default renderer would have used (`401` or `403`). |
| `Authenticated` | `bool` | `false` = anonymous visitor; `true` = signed-in but insufficient role/permission. |
| `Reason` | `string` | Human-readable explanation (e.g. `"insufficient role"`). |

Callers that do not set `OnDeny` see no change in behaviour — the zero value of
`AuthzOptions{}` preserves the existing JSON envelope path exactly.

#### Custom Subject and Action Resolvers

`MiddlewareWithOptions` accepts two additional fields on `AuthzOptions` that control
how the policy subject and action are derived from each request. Both default to `nil`,
meaning the built-in logic applies unchanged.

**`ResolveSubject SubjectResolver`**

```go
type SubjectResolver func(r *http.Request, claims *auth.Claims) string
```

By default, `Middleware` checks policies keyed by `claims.UserID`. Applications whose
Casbin policy table is keyed by role — rather than relying on Casbin role-grouping rules
— pass a resolver that returns `claims.Role`:

```go
import (
    "net/http"
    "github.com/jcsvwinston/nucleus/pkg/auth"
    "github.com/jcsvwinston/nucleus/pkg/authz"
)

r.Use(enforcer.MiddlewareWithOptions(authz.AuthzOptions{
    OnDeny: onDeny,
    ResolveSubject: func(r *http.Request, c *auth.Claims) string {
        return c.Role // policy table is keyed by role, not by user ID
    },
}))
```

**`ResolveAction ActionResolver`**

```go
type ActionResolver func(r *http.Request) string
```

HTML forms cannot send `DELETE` or `PUT` requests — they are limited to `GET` and `POST`.
A common SSR convention is to POST to a path ending in `/delete` for destructive actions.
The default HTTP-method mapping would classify that POST as the `"create"` action, which
is wrong. A resolver corrects this:

```go
import (
    "net/http"
    "strings"
    "github.com/jcsvwinston/nucleus/pkg/auth"
    "github.com/jcsvwinston/nucleus/pkg/authz"
)

resolveAction := func(r *http.Request) string {
    if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/delete") {
        return "delete"
    }
    // Fall back to the standard mapping for everything else.
    switch r.Method {
    case http.MethodGet, http.MethodHead:
        return "read"
    case http.MethodPost:
        return "create"
    case http.MethodPut, http.MethodPatch:
        return "update"
    case http.MethodDelete:
        return "delete"
    default:
        return "read"
    }
}

r.Use(enforcer.MiddlewareWithOptions(authz.AuthzOptions{
    OnDeny:        onDeny,
    ResolveSubject: func(r *http.Request, c *auth.Claims) string { return c.Role },
    ResolveAction: resolveAction,
}))
```

**Combining both resolvers (role-keyed policy, delete-override on POST)**

The complete pattern for an SSR application that uses role-keyed policies and
HTML-form delete conventions:

```go
import (
    "net/http"
    "strings"
    "github.com/jcsvwinston/nucleus/pkg/auth"
    "github.com/jcsvwinston/nucleus/pkg/authz"
)

onDeny := func(w http.ResponseWriter, r *http.Request, d authz.Denial) {
    if !d.Authenticated {
        http.Redirect(w, r, "/login", http.StatusFound)
        return
    }
    w.WriteHeader(http.StatusForbidden)
    // render styled 403 page
}

r.Use(enforcer.MiddlewareWithOptions(authz.AuthzOptions{
    OnDeny: onDeny,
    ResolveSubject: func(r *http.Request, c *auth.Claims) string {
        return c.Role
    },
    ResolveAction: func(r *http.Request) string {
        if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/delete") {
            return "delete"
        }
        switch r.Method {
        case http.MethodGet, http.MethodHead:
            return "read"
        case http.MethodPost:
            return "create"
        case http.MethodPut, http.MethodPatch:
            return "update"
        case http.MethodDelete:
            return "delete"
        default:
            return "read"
        }
    },
}))
```

**Operational note — structured log fields**

When `ResolveSubject` is set and a request is denied, the `authz denied` log line emits
two distinct fields:

| Field | Value |
| --- | --- |
| `subject` | The policy key actually checked (e.g. `"editor"` when resolving by role). |
| `user` | The raw `claims.UserID` — the identity of the human behind the request. |

They are identical when `ResolveSubject` is `nil` (the default). They diverge when the
resolver returns something other than `claims.UserID` — for example, when a user whose
`UserID` is `"u-42"` and whose `Role` is `"editor"` hits a denied endpoint, the log line
will contain `subject=editor user=u-42`. This makes it easy to audit whether a denial is
a policy gap (the role has no policy) versus an identity gap (the wrong user is assigned
the wrong role), without re-implementing the resolver logic in your log query.

`RequireRoleWithOptions` does **not** use `ResolveSubject` or `ResolveAction` — it
matches the JWT claims role directly against the supplied list and ignores both fields.

#### Policy Management

```go
// Add policy at runtime (auto-stamps the allow effect)
enforcer.AddPolicy("editor", "/api/drafts", "POST")

// Remove policy (drops both allow and deny variants for the tuple)
enforcer.RemovePolicy("editor", "/api/drafts", "POST")

// Check an effective permission for a subject
allowed := enforcer.Can("admin", "/admin/dashboard", "GET")

// List the roles assigned to a subject
roles := enforcer.GetRoles("alice")
```

---

## Admin Authentication Flow

> The admin panel and its `/admin` login are provided by the separate
> [orbit](https://github.com/jcsvwinston/orbit) module, mounted in-process — the
> framework core no longer ships an admin panel (ADR-019). The flow below applies
> when orbit is mounted; `nucleus createuser` (a framework CLI command) manages
> the admin user in the `nucleus_admin_users` store that orbit owns. See the orbit
> repository for its authoritative behaviour.

The admin panel has two authentication modes:

### Bootstrap Mode

When there are **no rows** in `nucleus_admin_users`, `/admin` is accessible without login to help with initial setup.

```bash
# First access after install - no authentication required
# Open http://localhost:8080/admin
```

### Protected Mode

Once at least one admin user exists, `/admin` requires login at `/admin/login`.

```bash
# Create first admin user
nucleus createuser --config nucleus.yml --username admin --email admin@example.com

# All subsequent accesses require login
# Login at http://localhost:8080/admin/login
```

### Admin Session Security

- Admin sessions use the configured session store (`sql` or `redis` recommended for production).
- Session cookies are marked `HttpOnly` (always) and `Secure` by default (opt out with `session_cookie_secure: false` only for plain-HTTP dev), with a configurable `SameSite` policy (`session_cookie_samesite`, default `lax`).
- View active admin sessions at `/admin/api/sessions` or UI at `/admin#/sessions`.

---

## Production Checklist

- [ ] Set strong `jwt_secret` (random 64-byte hex key) for single-secret mode, or configure `jwt_keys[]` + `jwt_current_kid` for multi-key/RS256 mode.
- [ ] Use `session_store: redis` or `sql` for multi-replica deployments.
- [ ] Verify `session_cookie_secure` is `true` (the default); set `false` only for plain-HTTP dev/test environments that cannot use HTTPS.
- [ ] Set `session_cookie_samesite: strict` for CSRF protection.
- [ ] Implement rate limiting on login endpoints (`rate_limit_by_route` or `rate_limit_burst`).
- [ ] Use Casbin policies for fine-grained authorization.
- [ ] Store `authz_policy.csv` in version control; reload on changes.
- [ ] Run `nucleus clearsessions` on a cron schedule to clean expired sessions.
- [ ] Monitor admin session dashboard for unusual access patterns.
- [ ] Rotate `jwt_secret` periodically (requires token re-issuance).
