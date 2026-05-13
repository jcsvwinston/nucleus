---
sidebar_position: 2
title: Auth & sessions
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
