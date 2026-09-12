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

## Measured posture

Two documents in `contracts/baseline/` say what this framework's security
actually is, and both are **observed**, never transcribed:

- `security_posture.txt` — the default configuration, the response headers a
  real booted application emits, its cookies, and what answers an unknown
  route. Loosening any of them fails the build.
- `asvs_l2.txt` — those values mapped onto OWASP ASVS 4.0.3 level 2, one row
  per requirement, each with a probe that fails when the control stops
  holding.

Today: **22 met, 2 the application's to complete, 1 not met**.

The two that need you:

- **V2.2.3** — the framework sends verification, reset and magic-link mail;
  notifying a user that their password *changed* is your message to write.
- **V3.3.2** — `session_idle_timeout` is honoured and ships **disabled**.
  Turning it on by default would start expiring sessions in every deployment
  that upgrades, so it waits for the next major; set it (30 minutes is
  common) and `nucleus doctor security` stops asking.

And the one that is not met:

- **V2.7.2** asks for out-of-band verifiers to expire within 10 minutes;
  magic links default to 15, because a link that expires before a mail
  client has finished scanning it is worse in practice. Set
  `MagicLinkTTL: 10 * time.Minute` if you need the requirement as written.

A posture document that cannot say *no* is a marketing page. This one can,
and does.
