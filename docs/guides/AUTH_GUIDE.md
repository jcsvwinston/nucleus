# Authentication & Authorization Guide

Reference date: 2026-05-13.
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

// Generate token for a user
token, err := manager.GenerateToken(auth.JWTClaims{
    UserID:   "user-123",
    Email:    "alice@example.com",
    Role:     "admin",
    CustomClaims: map[string]any{
        "tenant_id": "acme",
    },
})
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
r.Use(router.JWTMiddleware(manager))

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

For security-sensitive applications, implement short-lived access tokens with refresh tokens:

```go
// Access token: short-lived (15m)
accessCfg := auth.JWTConfig{
    Secret: cfg.JWTSecret,
    Expiry: 15 * time.Minute,
    Issuer: "myapp",
}

// Refresh token: long-lived (7d), stored server-side
refreshCfg := auth.JWTConfig{
    Secret: cfg.JWTSecret,
    Expiry: 7 * 24 * time.Hour,
    Issuer: "myapp",
}

// Generate both tokens on login
accessToken, _ := accessManager.GenerateToken(claims)
refreshToken, _ := refreshManager.GenerateToken(auth.JWTClaims{
    UserID: claims.UserID,
    CustomClaims: map[string]any{
        "token_type": "refresh",
        "jti": generateUniqueID(), // Store in DB for revocation
    },
})
```

#### Token Revocation

JWTs are stateless by design. To revoke tokens before expiry:

1. **Short expiry + refresh tokens**: Use 15-minute access tokens; revoke refresh tokens server-side.
2. **Token blacklist**: Store revoked JWT IDs (`jti`) in Redis with TTL matching token expiry.
3. **Version field**: Add a `token_version` claim; increment on password change/logout.

#### Key Rotation with Multiple Active Keys

`JWTManager` supports a keyset with explicit `kid` headers so secrets / asymmetric keys can be rotated without invalidating outstanding tokens. The same instance handles HS256 and RS256 simultaneously.

**Config-driven setup (recommended)** — `App.New` builds `App.JWT` automatically from `nucleus.yml` when `jwt_keys[]` is non-empty. Operators do not call `auth.NewJWTManagerFromKeys` themselves unless they have a non-config use case (e.g. dynamic key loading from a KMS).

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

Relying parties consuming RS256 tokens (other services, API gateways, identity proxies) typically fetch the public key set from a well-known URL.

**Auto-mounted** — when at least one RS256 key is present in `jwt_keys[]`, `App.New` mounts the handler at `/.well-known/jwks.json` automatically. The ADR-004 bootstrap allow-list already reserves that path so the default-deny middleware does not gate it. No application code is needed.

**Manual mount** (non-default path or programmatic manager only):

```go
a.Router.Get(
    "/.well-known/jwks.json",
    router.FromHTTP(mgr.JWKSHandler()),
)
```

The handler emits:

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

HMAC keys are **intentionally excluded** from the JWKS response — the endpoint is public and HMAC keys are shared secrets. An HS256-only manager returns `{"keys": []}`. `JWKS()` returns the same `JWKSet` data structure for callers that need to embed it in an OIDC discovery document.

Content-Type is `application/jwk-set+json; charset=utf-8` with a 5-minute `Cache-Control` hint so relying-party clients cache reasonably.

### Server-Side Sessions

Sessions are required for server-rendered applications, admin panel, and CSRF-protected forms.

#### Configuration

```yaml
# nucleus.yml
session_store: sql          # Options: memory, sql, redis
session_cookie_name: nucleus_session
session_cookie_secure: true # Set true in production (HTTPS only)
session_cookie_http_only: true
session_cookie_same_site: strict
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

// In handler (session middleware wired by app.New)
session := auth.SessionFromContext(r.Context())

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
isValid := auth.CheckPasswordHash("plaintext_password", hash)
if !isValid {
    return fmt.Errorf("invalid credentials")
}

// Custom cost (higher = slower, more secure)
hash, err = auth.HashPasswordWithCost("password", bcrypt.MaxCost)
```

**Security recommendations:**

- Never log or store plaintext passwords.
- Use bcrypt default cost (10) for most applications.
- Increase cost for high-security applications (cost 12-14).
- Implement rate limiting on login endpoints.

### User Model

Nucleus provides a minimal user structure in `pkg/auth`:

```go
type User struct {
    ID       string
    Username string
    Email    string
    Role     string
    HashedPassword string
    CreatedAt time.Time
    UpdatedAt time.Time
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
  `/metrics`, `/login`, `/.well-known/jwks.json`, `/static/*`, and
  the configured `admin_prefix`) responds normally — `App.New` seeds
  the anonymous allow for those paths before mounting the middleware.
- Any **other** request returns `403 Forbidden` until the operator
  loads policies via `admin_rbac_policy_file` or calls
  `App.Authorizer.AddPolicy` programmatically.

When no user policies are loaded, `App.New` emits a startup `WARN`:

```
authz: no user policies loaded; only bootstrap routes will respond — 
set admin_rbac_policy_file or call App.Authorizer.AddPolicy programmatically, 
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
`App.Authorizer` so the admin panel's internal RBAC paths keep
working. The option is deliberately not exposed as a config flag —
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

// Initialize enforcer
enforcer, err := authz.NewEnforcer(cfg.AuthzModelPath, cfg.AuthzPolicyPath)
if err != nil {
    return err
}

// Check permissions
allowed, err := enforcer.Enforce("alice", "/api/articles", "GET")
if err != nil {
    return err
}
if !allowed {
    return fmt.Errorf("forbidden")
}
```

#### Authorization Middleware

Apply authorization middleware to routes:

```go
import "github.com/jcsvwinston/nucleus/pkg/authz"

// Role-based middleware
r.Use(authz.RoleMiddleware(enforcer, "admin", "editor"))

// Custom middleware with dynamic subject
r.Use(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        role := observe.UserIDFromCtx(r.Context()) // Or extract from JWT claims
        allowed, _ := enforcer.Enforce(role, r.URL.Path, r.Method)
        if !allowed {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }
        next.ServeHTTP(w, r)
    })
})
```

#### Policy Management

```go
// Add policy at runtime
enforcer.AddPolicy("editor", "/api/drafts", "POST")

// Remove policy
enforcer.RemovePolicy("editor", "/api/drafts", "POST")

// Check if policy exists
hasPolicy := enforcer.HasPolicy("admin", "/admin/*", "*")

// Get all policies
policies := enforcer.GetPolicy()
```

---

## Admin Authentication Flow

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
- Session cookies are marked `HttpOnly`, `Secure` (in production), and `SameSite=Strict`.
- View active admin sessions at `/admin/api/sessions` or UI at `/admin#/sessions`.

---

## Production Checklist

- [ ] Set strong `jwt_secret` (random 64-byte hex key) for single-secret mode, or configure `jwt_keys[]` + `jwt_current_kid` for multi-key/RS256 mode.
- [ ] Use `session_store: redis` or `sql` for multi-replica deployments.
- [ ] Set `session_cookie_secure: true` when using HTTPS.
- [ ] Set `session_cookie_same_site: strict` for CSRF protection.
- [ ] Implement rate limiting on login endpoints (`rate_limit_by_route` or `rate_limit_burst`).
- [ ] Use Casbin policies for fine-grained authorization.
- [ ] Store `authz_policy.csv` in version control; reload on changes.
- [ ] Run `nucleus clearsessions` on a cron schedule to clean expired sessions.
- [ ] Monitor admin session dashboard for unusual access patterns.
- [ ] Rotate `jwt_secret` periodically (requires token re-issuance).
