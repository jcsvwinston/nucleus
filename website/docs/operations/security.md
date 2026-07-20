---
sidebar_position: 2
title: Security
covers:
  - pkg/observe.NewLogger
  - pkg/observe.NewLoggerWithRedaction
  - pkg/observe.DefaultRedactedKeys
  - pkg/observe.RedactionPlaceholder
  - pkg/router.SecurityHeaders
  - pkg/router.WithCSRF
  - pkg/auth.NewJWTManagerFromKeys
config_keys:
  - jwt_secret
  - session_cookie_secure
  - csrf_enabled
  - cors_origins[]
  - trusted_proxies[]
  - metrics_public
  - log_redact_extra_keys[]
  - rate_limit_requests
---

# Security

This page describes the practical security model: what Nucleus does for you
out of the box, what each default protects against, and what remains the
operator's responsibility. Every claim below reflects the shipped behavior of
the current release.

The short version: **the defaults deny**. Cross-origin requests, proxy
headers, unauthenticated access to your routes, insecure session cookies, and
unknown production config keys are all rejected until you explicitly allow
them.

## What you get by default

| Surface | Default behavior |
| --- | --- |
| Response headers | Hardened set on every response (see below). |
| CORS | Cross-origin requests denied until `cors_origins` lists origins. |
| Client IP | Proxy headers ignored until `trusted_proxies` is set. |
| Authorization | Default-deny RBAC: routes outside the bootstrap allow-list return 403 for anonymous callers. |
| Session cookie | `Secure`, `HttpOnly`, `SameSite=Lax`. |
| Log output | Secret-bearing attributes redacted. |
| Passwords | bcrypt, cost 12. |
| JWT secret | Rejected at boot if shorter than 32 bytes. |
| Production config | Unknown keys are a boot error under `NUCLEUS_ENV=production`. |

## Security headers

The default middleware stack sets these on **every** response:

- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `X-XSS-Protection: 0` (the legacy filter is disabled deliberately;
  CSP is the real control)
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Content-Security-Policy: default-src 'self'; style-src 'self'
  'unsafe-inline'; script-src 'self'; font-src 'self' data:`
- `Permissions-Policy: camera=(), microphone=(), geolocation=()`
- `Cross-Origin-Opener-Policy: same-origin`
- `Cross-Origin-Resource-Policy: same-origin`
- `Strict-Transport-Security: max-age=31536000; includeSubDomains` — over a
  direct TLS connection, or on every response when `env: production` (so the
  header survives TLS termination at a proxy). Never on plain-HTTP
  development runs, so localhost is not pinned to HTTPS.

Handlers run after the middleware, so an app that needs a different CSP or
`Permissions-Policy` can override the header for its own routes.

## Secrets in logs are redacted

The structured logger redacts the **values** of secret-bearing attributes
before they reach any sink. A curated, case-insensitive denylist —
`authorization`, `cookie`, `password`, `token`, `api_key`, `private_key`,
DSN-style connection strings, and a few dozen more (`observe.DefaultRedactedKeys`
returns the full list) — is matched **exactly** against attribute keys, and
matching values are replaced with `[REDACTED]`:

```go
logger.Info("login", "user", "ada", "password", supplied)
// {"msg":"login","user":"ada","password":"[REDACTED]"}
```

Exact matching is deliberate: suffix patterns like `*_key` would silently
swallow benign fields (`cache_key`, `page_token`) and hide debugging
information. Add app-specific fields via config instead:

```yaml
log_redact_extra_keys:
  - ssn
  - card_number
```

There is intentionally **no config key to disable redaction** — a config
typo must never turn secret logging back on. Disabling it requires the
explicit code-level constructor (`observe.NewLoggerWithRedaction` with
redaction turned off). `nucleus config print --effective` applies the same
redaction to secret config values.

## Sessions

Server-side sessions are managed with a pluggable store (`memory`, `sql`,
`redis`). The session cookie ships with:

- **`Secure: true` by default** — the cookie refuses to travel over plain
  HTTP. Local development over `http://` must opt out explicitly with
  `session_cookie_secure: false`; production should never set that.
- **`HttpOnly: true`** — not readable from JavaScript.
- **`SameSite=Lax` by default** (`session_cookie_samesite`).
- Absolute lifetime `session_lifetime` (default 72h) plus an optional
  rolling `session_idle_timeout`.

The cookie name supports the `__Host-` and `__Secure-` prefixes, and the
prefix preconditions are **validated at startup**: `__Host-` requires
`Secure`, path `/`, and no domain; `__Secure-` requires `Secure`. A
misconfigured prefix fails the boot instead of issuing a cookie every
browser would silently drop.

Operational note: the `memory` store is process-local — use `sql` or
`redis` with more than one replica. The `sql` store is dialect-aware for
SQLite, PostgreSQL, and MySQL.

## CSRF protection

CSRF protection is **opt-in** via `csrf_enabled: true`, because it only
makes sense for cookie/session-authenticated browser routes — a pure
Bearer-token API does not need it (the `mvc` scaffold enables it; the `api`
scaffold does not). When enabled, verification is layered:

1. **Origin verification** via the `Sec-Fetch-Site` request header
   (`same-origin` / `same-site` requests pass).
2. **Double-submit token** as the fallback for clients that do not send
   `Sec-Fetch-Site`.

The CSRF token cookie is `Secure` by default and deliberately **not**
`HttpOnly` — client-side code must read it to echo it back. Exempt
Bearer-only subtrees or signature-authenticated webhook receivers with
`csrf_exempt_paths` (e.g. `/api/`).

## CORS

Cross-origin requests are **denied by default**: with an empty
`cors_origins` (the default) no CORS headers are emitted at all. List exact
origins to allow them; the historical allow-all is the explicit opt-in
`["*"]`. `cors_allow_credentials: true` is only honored with a non-wildcard
origin list — the Fetch standard forbids credentials with `*`, and Nucleus
enforces that rather than emitting an invalid combination.

## Authentication and authorization

- **Passwords** are hashed with bcrypt at cost 12.
- **JWT, single-secret mode** (`jwt_secret`): HS256. A secret shorter than
  32 bytes is a **boot error**, not a warning. Generate one with
  `openssl rand -base64 32`.
- **JWT, keyset mode** (`jwt_keys[]`): multiple keys with `kid` headers,
  HS256/RS256/ES256, and zero-downtime rotation — new tokens sign with
  `jwt_current_kid` while every listed key stays valid for verification.
  Key material is referenced (env var, PEM file, or AWS Secrets Manager),
  **never inlined in tracked config files**.
- **RBAC** is default-deny. Anonymous callers reach only the bootstrap
  allow-list (`/healthz`, `/login`, `/.well-known/jwks.json`, `/static/*`,
  and `/metrics` unless `metrics_public: false`); everything else answers
  403 until a policy grants access. Policies live in a CSV file
  (`rbac_policy_file`) with explicit `allow`/`deny` rows and deny-override
  semantics.

## Mass assignment and primary keys

`Create` respects a pre-assigned primary key: a non-zero key on the entity
travels in the `INSERT`. That is what makes client-generated UUIDs work —
and it also means a handler that decodes a request body **straight into the
entity** (`BindJSON` + `Create`) lets the HTTP client choose the row's key.

For models exposed to request payloads, do one of:

- decode into a DTO without a key field (or zero the key before `Create`) —
  the pattern that also protects every other field you did not mean to
  accept, or
- register the model with `RejectClientPK: true`, which makes `Create`
  refuse entities that arrive carrying a key.

The default accepts pre-assigned keys. See
[Models & database](../concepts/models-and-database.md#how-create-treats-the-primary-key)
for the exact semantics.

## Rate limiting and client identity

Rate limiting is off by default; enable it for internet-facing deployments
(`rate_limit_requests` > 0, with `rate_limit_window`, `rate_limit_burst`,
and optional per-route / per-role partitioning). Its notion of "client" is
the client IP — which is why the proxy-header rule matters: **forwarding
headers are ignored unless the immediate peer is in `trusted_proxies`**.
Without that rule, anyone could evade limits or poison audit logs by
sending a forged `X-Forwarded-For`.

## The metrics endpoint

`/metrics` (configurable via `metrics_path`) carries **no authentication of
its own** and is on the anonymous allow-list by default, matching the
common "scraper on a private network" setup. If your network layer does not
isolate it, either set `metrics_public: false` (putting it behind the RBAC
enforcer, so your scraper needs a policy and credentials) or firewall the
path at the proxy. Metric values are operational data — treat them
accordingly.

## Secrets

- Supply secrets through the **environment** (`NUCLEUS_JWT_SECRET`,
  `NUCLEUS_DATABASES__DEFAULT__URL`, `NUCLEUS_SMTP_PASS`, …) or through
  `jwt_keys[]` references — never in files you commit.
- `jwt_secret` is **non-nullable**: setting it to `null` in a file, or
  exporting an empty `NUCLEUS_JWT_SECRET=`, is a boot error rather than a
  silent fall-back to no secret.
- `NUCLEUS_ENV=production` forces strict config validation: unknown keys in
  config files fail the boot even if development code downgraded them to
  warnings.

### File permissions (operator's job)

Nucleus reads these files but does not manage their permissions — keep them
tight on the host:

- config files that contain connection strings: owner-only (`0600`),
- PEM private keys referenced by `jwt_keys[].pem_path`: `0600`,
- the systemd `EnvironmentFile` holding secrets: `0600`, owned by root,
- the RBAC policy CSV: writable only by the deploy user (it is an
  authorization database).

## Hardening checklist

- [ ] `env: production` and `NUCLEUS_ENV=production` set.
- [ ] TLS everywhere: terminate at the proxy (with `env: production` for
      HSTS) or serve directly with `tls_cert_file`/`tls_key_file`.
- [ ] `jwt_keys[]` with rotation (or a ≥32-byte `jwt_secret`), material
      referenced from env / files / secret manager.
- [ ] `session_cookie_secure: true` (default) untouched; consider a
      `__Host-` cookie name over HTTPS.
- [ ] `csrf_enabled: true` for any browser-facing, session-authenticated
      app.
- [ ] `cors_origins` lists exact origins — no `["*"]` unless the API is
      deliberately public.
- [ ] `trusted_proxies` set to the load balancer ranges, nothing wider.
- [ ] `rate_limit_requests` > 0 for internet-facing deployments.
- [ ] `/metrics` network-restricted or `metrics_public: false`.
- [ ] RBAC policy reviewed: default-deny left intact, explicit `deny` rows
      for sensitive paths.
- [ ] Models bound to request payloads use DTOs or `RejectClientPK` — the
      client must not pick primary keys.
- [ ] Secret files at `0600`; secrets absent from tracked config.
- [ ] `nucleus health --deploy` green in the release pipeline.

Report suspected vulnerabilities through the repository's security policy on
GitHub rather than a public issue.
