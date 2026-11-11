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
  - pkg/nucleus.WithRateLimit
  - pkg/nucleus.WithAuthz
config_keys:
  - jwt_secret
  - session_cookie_secure
  - csrf_enabled
  - cors_origins[]
  - trusted_proxies[]
  - metrics_public
  - log_redact_extra_keys[]
  - rate_limit_requests
  - rbac_policy_file
  - profiling_enabled
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
| Authorization | Default-deny RBAC: registered routes outside the bootstrap allow-list return 403 for anonymous callers; a path no route serves answers 404. |
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

## How these defaults are kept honest

A security checklist written by hand drifts: it keeps claiming a protection
long after the code stopped emitting it, and nobody notices until a pentest.

So the table above is not maintained by hand. A test boots a real
application, sends it a real request, and records what actually comes back —
every security header, the attributes of every cookie, and what a
cross-origin caller receives — for both a development and a production
profile. That recording is compared, byte for byte, against a checked-in
baseline (`contracts/baseline/security_posture.txt`).

The comparison is exact in both directions. A **loosened** default fails the
build, which is the point. A **tightened** one fails too, because it changes
the behavior of deployments that relied on the old posture — so it gets
reviewed rather than slipped in. Either way the baseline has to be
regenerated deliberately, with the reason stated in the change.

If you want to know what this release actually does, that file is the
answer, and it cannot be out of date.

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
  allow-list (`/healthz`, `/livez`, `/readyz`, `/login`,
  `/.well-known/jwks.json`, `/static/*`, and `/metrics` unless
  `metrics_public: false`); every other registered route answers 403 until
  a policy grants access. A path no route serves
  answers the plain 404 — the gate runs after routing, so a 403 always
  means "this route exists and is not granted". Policies live in a CSV file
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

The limiter is one of the default subsystems. An application built
`WithoutDefaults()` — the `api` starter's shape — mounts it only with
`WithRateLimit()`, which the starter carries; without that option the
`rate_limit_*` keys enforce nothing, and the boot log says so in one ERROR
line (refused from v2.0.0, DEP-2026-016). Such an application decodes a
bearer token ahead of the limiter only with `WithAuthz()` (below): without
it the limiter keys a token's requests by their address, with it by the
token's user, as on the default stack. `nucleus doctor --check security`
and `nucleus health --deploy` read the composition root beside the
configuration and report that combination; where they cannot read it — a
deployed image carries the binary, not `main.go` — they say the limit holds
only on the default stack or with `WithRateLimit()`, rather than reporting
it in force.

Two things about `trusted_proxies` are worth knowing before you write it:

- **An entry that is not an IP or a CIDR fails to load.** It used to be
  discarded quietly, and with a single entry and a typo the list came out
  empty — the rule above then applied to nobody and forwarding headers were
  never read. It failed in the safe direction, which is precisely why it
  went unnoticed for so long. A typo now stops the boot and names the entry.
- **Trusting everything is trusting everything, however you spell it.**
  `nucleus doctor --check security` judges the ranges *together*, so
  `0.0.0.0/1` plus `128.0.0.0/1` is flagged exactly like `0.0.0.0/0`. Under
  a catch-all the header that becomes attacker-controlled is `X-Real-IP`,
  which is honoured unconditionally once the peer is trusted — not
  `X-Forwarded-For`, which is walked right to left skipping trusted hops and
  therefore falls through when every hop is trusted.

## Core-only applications

`WithoutDefaults()` leaves authorization out: no RBAC enforcer, no
default-deny middleware, no global bearer decode, and every route answers
anyone the handler lets through. The "defaults deny" summary at the top of
this page is about the default stack — and about a core-only application
that opts back in with `WithAuthz()`.

### `WithAuthz()`: the default stack's authorization, opt-in

```go
nucleus.New().
    FromConfigFile("nucleus.yml").
    WithoutDefaults().
    WithAuthz().
    Start()
```

`WithAuthz()` (`app.WithAuthz()` for `app.New`) mounts the default stack's
authorization and nothing else of that stack, in the default stack's order:

1. the bearer decode — a valid token's claims are in the context from here
   on, so the API-key read, the rate limiter (`WithRateLimit()`), the
   request interceptors and the handlers all see who is calling;
2. the API-key read (`WithAPIKeys()`), the rate limiter and the
   interceptors, as without the option;
3. the default-deny gate, last: a request passes when one of its subjects —
   the token's user and role, an API key's owner and its scopes as
   `scope:<name>`, the signed-in account, then `anonymous` — is allowed.

The enforcer loads `rbac_policy_file` (or a policy file found in the default
locations) and the bootstrap allow-list, leaving `/metrics` out under
`metrics_public: false`; the rows mounted modules declare in `Policies` load
into it. The profiler and the `/realtime/{topic}` channels sit behind the
gate like any route. `WithOpenAuthz()` switches the gate off here as on the
default stack. On the default stack `WithAuthz()` changes nothing.

**With no policy file and no module rows, default-deny means every
registered route outside the bootstrap allow-list answers an anonymous
request 403** — `/healthz`, `/livez`, `/readyz`, `/login`,
`/.well-known/jwks.json`, `/static/*` and (unless `metrics_public: false`)
`/metrics` still answer; a path no route serves answers 404. That is the
default stack's behaviour too, and the boot log says it in one line:

```
level=WARN msg="authz: default-deny with 0 policy rows — an anonymous request reaches only the bootstrap routes, and every other registered route answers 403 until a row allows it: …" bootstrap_routes="/healthz, /livez, /readyz, /metrics, /login, /.well-known/jwks.json, /static/*"
```

With a policy file the line is an INFO naming the file and how many rows
it carries (`authz: default-deny with 12 policy rows from rbac_policy.csv`);
each module whose rows load says so in a line of its own.

The `api` starter does not carry `WithAuthz()`: its documented contract is
"no authz", and `nucleus new --template api` names the option as the way to
add it. `nucleus serve --without-defaults` does not mount it either — the
plain `nucleus serve` is the configuration-only server with an enforcer.

### Without `WithAuthz()`

What the configuration or the application asks to have guarded is not, and
the boot log says so, one ERROR line each; from v2.0.0 each combination
refuses to start unless `WithAuthz()` guards it:

- **`rbac_policy_file` loads nothing and `metrics_public: false` gates
  nothing** — `authz configuration IGNORED` (DEP-2026-017).
  `nucleus doctor --check rbac` says the same when it finds the composition
  root.
- **The rows modules declare in `Policies` are discarded**, deny rows
  included: there is no enforcer to load them into. A module that lets
  anonymous callers read and keeps writes for a role — what
  `nucleus generate module` writes — answers every write to anyone here.
  The boot log line, `module policies DISCARDED`, names the modules, how
  many rows and deny rows they declare, and the routes their rows would
  have refused an anonymous caller (DEP-2026-017). A module whose rows
  grant anonymous callers every action on every route it serves — the
  accounts module — loses nothing and is not reported.
- **The profiler answers anyone.** `profiling_enabled: true` mounts
  `/debug/pprof`, and heap and goroutine dumps — live process memory —
  answer anyone who reaches the port, unless the application's own
  middleware refuses them. The boot log line, `profiler UNGUARDED`, names
  heap dumps of the production process outright in production;
  `nucleus doctor --check security` reports it (an error in production, a
  warning elsewhere) when it finds the composition root (DEP-2026-018).
  Leave `profiling_enabled` off on such an application, and when you need a
  profile serve `net/http/pprof` from a listener only operators reach.

Two more carry no ERROR line, because nothing in the configuration asks for
them: an API key's scopes authorize nothing — `apikeys.Require` on a route
is the only check — and every realtime topic is open to whoever reaches the
route, which the INFO line that announces the channels says.

## The metrics endpoint

`/metrics` (configurable via `metrics_path`) carries **no authentication of
its own** and is on the anonymous allow-list by default, matching the
common "scraper on a private network" setup. If your network layer does not
isolate it, either set `metrics_public: false` (putting it behind the RBAC
enforcer, so your scraper needs a policy and credentials) or firewall the
path at the proxy. Metric values are operational data — treat them
accordingly. On an application built `WithoutDefaults()` there is an
enforcer to put it behind only with `WithAuthz()`; without it, firewalling
is the only option.

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
- [ ] `rate_limit_requests` > 0 for internet-facing deployments — and, on
      an application built `WithoutDefaults()`, `WithRateLimit()` in the
      composition root (no `rate_limit_requests IGNORED` line at boot).
- [ ] On an application built `WithoutDefaults()`: `WithAuthz()` in the
      composition root if its routes are meant to be authorized — or else
      `profiling_enabled` off (no `profiler UNGUARDED` line at boot), and no
      `module policies DISCARDED` line, or the routes it names guarded by
      the application itself.
- [ ] `/metrics` network-restricted or `metrics_public: false`.
- [ ] RBAC policy reviewed: default-deny left intact, explicit `deny` rows
      for sensitive paths.
- [ ] Models bound to request payloads use DTOs or `RejectClientPK` — the
      client must not pick primary keys.
- [ ] Secret files at `0600`; secrets absent from tracked config.
- [ ] `nucleus doctor --check security` clean — it looks for settings that
      load fine and expose you anyway: a wildcard CORS allow-list, a
      catch-all `trusted_proxies` range, a guessable `jwt_secret`, a
      profiler on an application that builds no enforcer to guard it (one
      built `WithoutDefaults()` without `WithAuthz()`).
- [ ] `nucleus health --deploy` green in the release pipeline.
- [ ] The release archive you deployed verified against its signature and
      its build provenance — see
      [Verifying a release](./verifying-releases.md), which also says how to
      tell whether the release you have is one that carries them.

Report suspected vulnerabilities through the repository's security policy on
GitHub rather than a public issue.
