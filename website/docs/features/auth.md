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
  - pkg/authz.New
  - pkg/authz.Enforcer
  - pkg/authz.Enforcer.AddPolicy
  - pkg/authz.Enforcer.Deny
  - pkg/authz.Enforcer.RemovePolicy
  - pkg/authz.Enforcer.RequireRole
  - pkg/app.JWTKeySpec
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
session:
  store: redis
  cookie_secure: true
  cookie_same_site: lax
  ttl: 24h
redis:
  addr: localhost:6379
```

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
ok,   err := auth.VerifyPassword(hash, "hunter2")
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

The JWT secret is read from the env var named in `auth.jwt_secret_env`
(`NUCLEUS_JWT_SECRET` by default). It is never read from `nucleus.yml`
directly, by design — config files end up checked in.

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
admin:
  rbac_policy_file: ./auth/policy.csv
```

```go
allowed, err := a.Authorizer.Enforce(userID, "articles", "edit")
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
or `deny`. Programmatic callers should keep using `AddPolicy` (which
stamps `allow`) and `Deny` rather than reaching into the Casbin
backing enforcer directly.

## Authentication middleware

For routes that require an authenticated session, plug the auth
middleware:

```go
r.Use(auth.SessionRequired(a.Sessions))

r.Group("/api/admin", func(g *router.Group) {
    g.Use(auth.RequireRole("admin"))
    // ...
})
```

`SessionRequired` rejects anonymous requests; `RequireRole` checks the
RBAC enforcer for a named role.

## CSRF, CORS and rate limiting

These are middleware-level concerns documented in
[Concepts → Routing & middleware](../concepts/routing.md). Defaults are
production-safe (CSRF on for form posts, CORS denies unknown origins,
rate limit at a sensible threshold), and every value is reachable from
`nucleus.yml`.
