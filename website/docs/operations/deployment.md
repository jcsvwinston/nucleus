---
sidebar_position: 1
title: Deployment
covers:
  - pkg/app.App.Run
  - pkg/app.App.Shutdown
  - pkg/nucleus.EnvProduction
  - pkg/router.WithHSTS
  - pkg/router.WithTrustedProxies
config_keys:
  - env
  - host
  - port
  - tls_cert_file
  - tls_key_file
  - log_level
  - log_format
  - trusted_proxies[]
  - session_store
---

# Deployment

A Nucleus application deploys as **one Go binary**. There is no application
server, no separate worker daemon, and no runtime dependency beyond the
databases and services you configure. This page walks the path from `go build`
to a hardened production process.

## Build the binary

A project scaffolded with `nucleus new` is a normal Go module — its entry
point is the project root. Build it like any Go program:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/app .
```

- `CGO_ENABLED=0` produces a static binary. The default SQLite driver is
  pure Go, so no C toolchain is needed for any of the default databases
  (SQLite, PostgreSQL, MySQL).
- SQL Server and Oracle support are opt-in **build tags**: add
  `-tags mssql` or `-tags oracle` to include those drivers.
- Cross-compile with the usual `GOOS`/`GOARCH` variables
  (`GOOS=linux GOARCH=amd64 go build …`).

You will usually also want the `nucleus` CLI on the deploy host (or in the
deploy pipeline) to run migrations and preflight checks:

```bash
go install github.com/jcsvwinston/nucleus/cmd/nucleus@latest
```

## Configuration in production

Configuration resolves as `struct defaults < config file(s) < NUCLEUS_* env
vars` — see the
[Configuration reference](../reference/configuration.md) for every key. A
typical production layout keeps one base `nucleus.yml` plus environment
overrides:

```yaml
env: production
debug: false

host: 0.0.0.0
port: 8080

log_level: info
log_format: json         # one JSON object per line — for your log shipper

databases:
  default:
    url: postgres://app@db.internal:5432/app?sslmode=require

session_store: redis     # memory is process-local; use sql or redis behind a load balancer
redis_url: redis://cache.internal:6379/0
```

Secrets never belong in the file — supply them through the environment
(`NUCLEUS_JWT_SECRET`, `NUCLEUS_DATABASES__DEFAULT__URL`, …) or, for JWT key
material, through the `jwt_keys[]` references that read env vars, PEM files,
or AWS Secrets Manager. See
[Security](./security.md#secrets) for the full secrets posture.

### What `env: production` and `NUCLEUS_ENV=production` change

Two related switches, two different jobs:

- **`env: production`** (the config key) tells the *runtime* it is in
  production. Verified effects: `Strict-Transport-Security` is emitted on
  every response even when TLS is terminated upstream (behind a proxy,
  where the connection Nucleus sees is plain HTTP), and
  `nucleus health --deploy` stops warning about the environment.
- **`NUCLEUS_ENV=production`** (the OS environment variable) hardens the
  *config loader*: unknown keys in config files are always a boot error,
  even if the code downgraded them to warnings with
  `WithUnknownFields("warn")` during development. A typo in a production
  config file fails the deploy instead of silently doing nothing.

Set both in production.

### Preflight checks

Before routing traffic, run the built-in hardening check against the exact
config the process will use:

```bash
nucleus health --deploy --config nucleus.yml
```

It verifies connectivity to the configured dependencies and flags
deployment smells: `env` not `production`, `debug` enabled, a missing or
short `jwt_secret`, rate limiting disabled, non-JSON log format, and a
process-local (`memory`) session store, among others.

## TLS

Nucleus can terminate TLS itself: set both `tls_cert_file` and
`tls_key_file` (PEM paths) and the server starts with `ListenAndServeTLS`.
When either key is empty the server speaks plain HTTP — the common setup
behind a TLS-terminating reverse proxy or load balancer. In that setup,
remember `env: production` so HSTS is still emitted (see above).

## Migrations are a deploy step

Nucleus **does not apply migrations at startup**. Starting the binary starts
the HTTP server; nothing else. Apply pending migrations explicitly as part of
the deploy, before the new binary takes traffic:

```bash
nucleus migrate --config nucleus.yml status   # what would run
nucleus migrate --config nucleus.yml up       # apply pending migrations
```

`nucleus migrate drift` detects applied migrations whose source file is
missing on disk and exits non-zero, which makes it a good CI guard. The
programmatic `App.AutoMigrate` helper creates missing tables for registered
models, but it never alters existing tables — treat it as a development
convenience, not a deployment mechanism.

## systemd unit

```ini
[Unit]
Description=My Nucleus app
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=app
Group=app
WorkingDirectory=/srv/myapp
ExecStart=/srv/myapp/bin/app
Environment=NUCLEUS_ENV=production
# Secrets: prefer an EnvironmentFile owned by root, mode 0600
EnvironmentFile=/etc/myapp/secrets.env
Restart=on-failure
RestartSec=2
# Graceful stop: systemd sends SIGTERM; Nucleus drains in-flight requests
TimeoutStopSec=90
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/srv/myapp/storage
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

On `SIGTERM` (and `SIGINT`) the server stops accepting connections, drains
in-flight requests, and runs registered shutdown hooks in reverse order. The
drain window is derived from `write_timeout` (10 seconds when unset), so give
`TimeoutStopSec` comfortable headroom above that.

## Container image

A multi-stage build keeps the runtime image at a few megabytes:

```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app .

FROM alpine:3
RUN adduser -D -H app
WORKDIR /srv/app
COPY --from=build /out/app ./app
COPY nucleus.yml ./
USER app
EXPOSE 8080
ENTRYPOINT ["./app"]
```

Run migrations from the pipeline (or an init container / release job), not
from the app container's entrypoint — keeping schema changes a separate,
observable step is what lets you roll back a bad deploy without wondering
what half-ran.

## Health and metrics endpoints

Two endpoints matter to your orchestrator and your monitoring:

- **`GET /healthz`** — aggregated dependency health, registered on every
  app. It probes each configured database, Redis (when `redis_url` is set),
  object storage (when configured), and the mailer (when the driver supports
  probing), each with a 2-second per-dependency timeout. Response: `200`
  with a JSON body when everything is healthy, `503` otherwise — suitable
  for Kubernetes liveness/readiness probes and load-balancer target checks.
- **`GET /metrics`** (path set by `metrics_path`, empty disables) — a
  Prometheus/OpenMetrics scrape endpoint. It has **no authentication of its
  own** by default; restrict it at the network layer, or set
  `metrics_public: false` to put it behind the RBAC enforcer. Set
  `otlp_endpoint` to additionally push traces and metrics over OTLP-HTTP.

```json
{
  "status": "healthy",
  "checked_at": "2026-07-19T10:00:00Z",
  "checks": [
    {"name": "db:default", "status": "healthy", "latency_ms": 2}
  ]
}
```

## Behind a reverse proxy

Nucleus is deliberately skeptical of proxy headers:

- **Client IP.** `X-Forwarded-For` / `X-Real-IP` are **ignored by default**.
  Set `trusted_proxies` to the addresses or CIDR ranges of your load
  balancer, and only then are forwarding headers from those peers honored
  for rate limiting and logging. This prevents header-spoofed rate-limit
  evasion and log poisoning:

  ```yaml
  trusted_proxies:
    - 10.0.0.0/8
  ```

- **HSTS.** With TLS terminated at the proxy, set `env: production` so
  `Strict-Transport-Security` is emitted despite the plain-HTTP hop.
- **Cookies.** `session_cookie_secure` defaults to `true` and should stay
  that way behind an HTTPS proxy; the cookie is scoped to the browser-facing
  scheme, not the internal hop.
- **Proxy timeouts.** Align the proxy's read/send timeouts with
  `read_timeout` (30s default) and `write_timeout` (60s default) so the
  proxy does not cut off responses the app is still allowed to write.

There is no configuration for `X-Forwarded-Proto`-based redirects —
redirect HTTP to HTTPS at the proxy.

## Deploy checklist

- [ ] `env: production` in the config **and** `NUCLEUS_ENV=production` in
      the process environment.
- [ ] Secrets in env vars or secret-manager references — never in tracked
      files.
- [ ] `nucleus migrate … up` ran as a deploy step; `nucleus migrate drift`
      is green in CI.
- [ ] `nucleus health --deploy` reports no errors with the production
      config.
- [ ] `/healthz` wired to your orchestrator's probes; `/metrics` scraped
      and network-restricted.
- [ ] `trusted_proxies` set to your load balancer ranges.
- [ ] `session_store` is `sql` or `redis` if you run more than one replica.
- [ ] Log shipper consuming JSON logs (`log_format: json`).

Continue with [Security](./security.md) for the hardening details and
[Upgrades](./upgrade.md) for moving between framework versions.
