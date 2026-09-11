---
title: Auth & sessions
covers:
  - pkg/auth.ContextWithClaims
  - pkg/auth.ClaimsFromContext
  - pkg/auth.Claims
config_keys: []
---

# Auth & sessions

Two packages cover this ground: `pkg/auth` handles authentication (sessions,
passwords, JWT, the backend chain) and `pkg/authz` handles authorization
(role-based access control). This section splits them by the question you
are actually asking:

- **[Your first login](./your-first-login.md)** — the complete worked
  slice: a `users` table, a `UserProvider`, `POST /login` through the
  authentication chain, session, logout, CSRF. Start here if you are
  building sign-in for your own application.
- **[RBAC & the middleware chain](./rbac-and-middleware.md)** — the
  default-deny policy gate, and why your first session-authenticated route
  answered 403. Read this one *before* adding more middleware.
- **[Sessions & passwords](./sessions-and-passwords.md)** — session
  stores, cookie defaults, `RenewToken`/`Destroy`, bcrypt hashing.
- **[JWT](./jwt.md)** — stateless auth, from a single secret to a rotating
  keyset with a public JWKS endpoint.
- **[Backends & federated sign-in](./backends-and-federation.md)** — the
  ordered `auth_backends` chain, LDAP, writing your own backend, the
  conformance suite, and OIDC/SAML identity providers.

## Which mechanism is which

| You want | Use | Page |
| --- | --- | --- |
| Sign in users of **your** app against **your** table | `auth.UserProvider` + the chain | [Your first login](./your-first-login.md) |
| Sign in against a corporate directory | `auth_backends: [ldap, local]` | [Backends](./backends-and-federation.md) |
| "Sign in with …" via an identity provider | `auth_federated` (OIDC/SAML) | [Backends → Federated](./backends-and-federation.md#federated-sign-in-oidc-saml) |
| Stateless tokens for APIs and services | `JWTManager` | [JWT](./jwt.md) |
| Decide who may reach which route | `pkg/authz` policy gate | [RBAC & middleware](./rbac-and-middleware.md) |
| Admin accounts for the orbit panel | `nucleus createuser` | [orbit](https://github.com/jcsvwinston/orbit) |

Identity travels through the request context as `auth.Claims`
(`auth.ContextWithClaims` to inject, `auth.ClaimsFromContext` to read); the
RBAC gate, log attribution and your handlers all read the same claims.
