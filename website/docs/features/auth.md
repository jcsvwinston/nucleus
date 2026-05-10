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

`pkg/auth` exposes JWT helpers for stateless auth:

```go
token, err := auth.IssueJWT(secret, claims, 24*time.Hour)
claims, err := auth.ParseJWT(secret, token)
```

The JWT secret is read from the env var named in `auth.jwt_secret_env`
(`NUCLEUS_JWT_SECRET` by default). It is never read from `nucleus.yml`
directly, by design — config files end up checked in.

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
[Concepts → Routing & middleware](../concepts/routing). Defaults are
production-safe (CSRF on for form posts, CORS denies unknown origins,
rate limit at a sensible threshold), and every value is reachable from
`nucleus.yml`.
