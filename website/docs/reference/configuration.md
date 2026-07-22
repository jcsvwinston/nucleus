---
sidebar_position: 2
title: Configuration reference
description: Every configuration key Nucleus recognizes — default, lifecycle, and semantics.
covers: []
config_keys: []
---

{/* GENERATED — edit CONFIG_KEY_REGISTRY.md and re-run go run ./scripts/website/gen-config-reference */}

# Configuration reference

This page lists every configuration key the framework recognizes, with its
default value and lifecycle. For how configuration is loaded and merged —
file formats, the multi-file loader, list operators, module config — see
[Concepts → Configuration](../concepts/configuration.md).

## How to read this page

**Precedence.** Values are resolved lowest-to-highest:

```
struct defaults  <  config file(s)  <  NUCLEUS_* env vars
```

**Environment variables.** Every key maps to a `NUCLEUS_`-prefixed
variable. Flat keys use one underscore (`port` →
`NUCLEUS_PORT`); nested keys join segments with a **double**
underscore (`databases.<alias>.url` →
`NUCLEUS_DATABASES__<ALIAS>__URL`).

**Lifecycle.**

| Tag | Meaning |
| --- | --- |
| `stable` | Key name and semantics are contract surfaces on the v1.x line. |
| `transitional` | Supported, but semantics may still refine before freezing. |
| `experimental` | No compatibility guarantee yet. |
| `removed` | No longer accepted; the notes name the replacement. |

To see the value every key actually resolves to in a running deployment —
and which file or variable set it — use
`nucleus config print --effective`
([CLI overview](../cli/overview.md#effective-config-nucleus-config-print---effective)).

## Server

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `host` | `0.0.0.0` | `stable` | Bind host. |
| `port` | `8080` | `stable` | Bind port. |
| `read_timeout` | `30s` | `stable` | HTTP read timeout. |
| `write_timeout` | `60s` | `stable` | HTTP write timeout. |
| `idle_timeout` | `120s` | `stable` | HTTP idle timeout. |
| `tls_cert_file` | `""` | `transitional` | PEM certificate (chain) file. When both `tls_cert_file` and `tls_key_file` are set, `App.Run` serves HTTPS directly (`ListenAndServeTLS`); when either is empty the server speaks plain HTTP (terminate TLS at a reverse proxy instead). |
| `tls_key_file` | `""` | `transitional` | PEM private-key file paired with `tls_cert_file`. |

## Database

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `database_default` | `default` | `stable` | Primary DB alias used by `app.DB`. |
| `databases.<alias>.url` | `databases.default.url=sqlite://nucleus.db` | `stable` + `experimental` | Stable schemes: `sqlite://`, `postgres://`, `postgresql://`, `mysql://`; exploratory schemes: `sqlserver://`/`mssql://`, `oracle://`. |
| `databases.<alias>.max_open` | `25` | `stable` | Per-alias pool max open conns (inherits primary if omitted). |
| `databases.<alias>.max_idle` | `5` | `stable` | Per-alias pool max idle conns (inherits primary if omitted). |
| `databases.<alias>.max_lifetime` | `5m` | `stable` | Per-alias conn max lifetime (inherits primary if omitted). |

## MultiSite and MultiTenant

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `multisite.enabled` | `false` | `stable` | Enable host-based site resolution. |
| `multisite.default_site` | `default` | `stable` | Fallback site when host does not match configured patterns. |
| `multisite.sites.<site>.hosts[]` | `[]` | `stable` | Exact host or wildcard (`*.example.com`) patterns per site. |
| `multisite.sites.<site>.database` | `database_default` | `stable` | Default DB alias for the site. |
| `multisite.sites.<site>.tenant_database_alias_template` | `""` | `stable` | Optional per-site tenant DB alias template (`tenant_%s` or `{tenant}`). |
| `multitenant.enabled` | `false` | `stable` | Enable tenant resolution. |
| `multitenant.resolver` | `subdomain` | `stable` | `subdomain` or `header`. |
| `multitenant.header` | `X-Tenant-ID` | `stable` | Header used when resolver is `header`. |
| `multitenant.default_tenant` | `""` | `stable` | Optional fallback tenant id. |
| `multitenant.require_isolated_db` | `true` | `stable` | Security-by-default guard: rejects shared DB alias routing across tenants. |
| `multitenant.database_alias_template` | `tenant_%s` | `stable` | Global tenant DB alias template (`%s` or `{tenant}`). |
| `multitenant.tenants.<tenant>.site` | `""` | `stable` | Optional site binding for a tenant mapping. |
| `multitenant.tenants.<tenant>.database` | `""` | `stable` | Explicit tenant DB alias mapping. |

## Redis and Sessions

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `redis_url` | `""` | `stable` | Optional Redis endpoint for queue/session features. |
| `session_lifetime` | `72h` | `stable` | Server-side session lifetime. |
| `session_store` | `memory` | `stable` | Supported values: `memory`, `sql`, `redis`. |
| `session_redis_url` | `""` | `stable` | Redis override for session backend. |
| `session_table` | `nucleus_sessions` | `stable` | SQL session table name. |
| `session_cookie_name` | `session` | `stable` | Session cookie name. |
| `session_cookie_domain` | `""` | `stable` | Session cookie domain. |
| `session_cookie_path` | `/` | `stable` | Session cookie path. |
| `session_cookie_secure` | `true` | `stable` | Session cookie `Secure` attribute. Secure-by-default — the cookie refuses to ride over plain HTTP. Local development over `http://` must opt out with `session_cookie_secure: false`. Mirrors the CSRF cookie posture. |
| `session_cookie_samesite` | `lax` | `stable` | SameSite policy string. |
| `session_idle_timeout` | `0` | `stable` | Optional idle timeout override. |
| `session_redis_prefix` | `nucleus:sessions:` | `stable` | Session Redis key prefix. |

## Auth

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `jwt_secret` | `""` | `stable` | Single-secret HS256 used as legacy fallback when `jwt_keys` is empty. Must be at least 32 bytes when set — a shorter secret is a boot error (enforced since v1.2.0; generate one with `openssl rand -base64 32`). Tokens carry no `kid` header. When `jwt_keys[]` is non-empty this key is ignored. See `jwt_keys[]` for the production multi-key path. |
| `jwt_expiry` | `24h` | `stable` | JWT lifetime default. |
| `jwt_issuer` | `""` | `stable` | Issuer claim (`iss`) stamped into every token minted by `App.JWT`. Used by both single-secret and multi-key managers. |
| `jwt_keys[]` | `[]` | `stable` | Ordered keyset consumed by `App.New` to build a `*auth.JWTManager` via `auth.NewJWTManagerFromKeys`. Each entry is a `JWTKeySpec` sub-object — see table below. When non-empty, `jwt_secret` is ignored. |
| `jwt_current_kid` | `""` | `stable` | `kid` value that identifies the active signing key within `jwt_keys[]`. Must match one entry's `kid`. New tokens are signed with this key; all keyset keys remain valid for validation. |

#### `jwt_keys[]` entry fields (`JWTKeySpec`)

Exactly one of `secret_env` / `pem_path` / `pem_env` must be set per entry — key material is never read from tracked config files:

| Field | Type | Notes |
| --- | --- | --- |
| `kid` | string | Unique key identifier stamped in token `kid` header. Required. |
| `algorithm` | string | `HS256`, `RS256`, or `ES256`. Required. |
| `secret_env` | string | Resolver reference to the HMAC secret (HS256 only). See reference forms below. |
| `pem_path` | string | Filesystem path to a PEM-encoded private key (RS256: RSA, PKCS#1/PKCS#8; ES256: ECDSA P-256, SEC1/PKCS#8). Rejects PEM with trailing content. |
| `pem_env` | string | Resolver reference to PEM bytes (RS256 / ES256). See reference forms below. |

Reference forms accepted by `secret_env` and `pem_env` (plain names read the environment; the `aws-sm:` scheme reads AWS Secrets Manager via the standard credential chain):

| Reference | Resolved from |
| --- | --- |
| `MY_VAR` | environment variable `MY_VAR` (historical behaviour) |
| `env:MY_VAR` | environment variable `MY_VAR` (explicit form) |
| `aws-sm:<secret-id>` | AWS Secrets Manager secret `<secret-id>` |
| `aws-sm:<secret-id>#<json-key>` | one string field of a JSON-object AWS secret |

## RBAC

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `rbac_policy_file` | `""` | `stable` | Path to Casbin RBAC CSV policy file. Feeds the core authz enforcer (`pkg/authz.Enforcer`). **CSV rows require a 4th column** (`allow` / `deny`) — the model uses deny-override semantics. Programmatic callers use `Enforcer.AddPolicy` (auto-stamps `allow`) and `Enforcer.Deny`. Auto-discovered at `rbac_policy.csv`, `config/rbac_policy.csv`, or `rbac/rbac_policy.csv` when the key is empty. |
| `admin_rbac_policy_file` | `""` | `removed` | Removed in v0.12.0. Use `rbac_policy_file` |

## Admin (removed — moved to the orbit module)

| Key | Former default | Lifecycle | Migration |
| --- | --- | --- | --- |
| `admin_prefix` | `/admin` | `removed` | Use `modules.orbit.prefix` (see orbit module docs). |
| `admin_title` | `Nucleus Admin` | `removed` | Use `modules.orbit.title`. |
| `admin_auth_database` | `""` | `removed` | Use `modules.orbit.auth_database`. |
| `admin_bootstrap_username` | `""` | `removed` | Use `modules.orbit.bootstrap_username`. |
| `admin_bootstrap_email` | `""` | `removed` | Use `modules.orbit.bootstrap_email`. |
| `admin_bootstrap_password` | `""` | `removed` | Use `modules.orbit.bootstrap_password`. |
| `admin_live_exclude_patterns[]` | `[/admin]` | `removed` | Use `modules.orbit.live_exclude_patterns`. |
| `admin_cluster_enabled` | `false` | `removed` | Use `modules.orbit.cluster_enabled`. |
| `admin_cluster_redis_url` | `""` | `removed` | Use `modules.orbit.cluster_redis_url`. |
| `admin_cluster_channel` | `nucleus:admin:live:v1` | `removed` | Use `modules.orbit.cluster_channel`. |
| `admin_cluster_node_id` | `""` | `removed` | Use `modules.orbit.cluster_node_id`. |
| `admin_cluster_token` | `""` | `removed` | Use `modules.orbit.cluster_token`. |
| `admin_trace_url_template` | `""` | `removed` | Use `modules.orbit.trace_url_template`. |

## Mail

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `mail_driver` | `noop` | `stable` | Built-in and plugin-backed provider selection. |
| `mail_from` | `noreply@localhost` | `stable` | Default sender. |
| `smtp_host` | `""` | `stable` | SMTP host. |
| `smtp_port` | `587` | `stable` | SMTP port. |
| `smtp_user` | `""` | `stable` | SMTP user. |
| `smtp_pass` | `""` | `stable` | SMTP password. |
| `mail_circuit_breaker.enabled` | `true` | `stable` | Wrap `mail.Sender.Send` with a `pkg/circuit` breaker. `noop` driver is never wrapped. `Healthy` (SMTP HELO probe) bypasses the breaker so `/healthz` observes recovery. |
| `mail_circuit_breaker.failure_threshold` | `5` | `stable` | Consecutive Send failures required to trip the breaker open. |
| `mail_circuit_breaker.cooldown` | `30s` | `stable` | Time the breaker stays open before admitting half-open probes. |
| `mail_circuit_breaker.half_open_max_concurrent` | `1` | `stable` | In-flight probe budget while half-open. |

## Module Jobs and Webhooks

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `jobs_provider` | `memory` | `stable` | `pkg/tasks` provider that executes module jobs: `memory` (in-process scheduler + workers; pending jobs are lost on restart) or `asynq` (Redis-backed, durable; requires `jobs_redis_url`). Added in v1.4.0. |
| `jobs_redis_url` | `""` | `stable` | Redis connection URL for the `asynq` jobs provider (e.g. `redis://localhost:6379/0`). Required when `jobs_provider: asynq` — validated at boot; ignored by `memory`. Added in v1.4.0. |
| `jobs_concurrency` | `4` | `stable` | Number of concurrent job workers. `0` uses the provider default. Added in v1.4.0. |
| `webhooks_prefix` | `/webhooks` | `stable` | URL prefix under which module webhook routes mount: `<prefix>/<module-name><path>`. With `csrf_enabled: true` the framework exempts this prefix from CSRF automatically — webhooks authenticate by HMAC signature (`X-Nucleus-Signature`), not by CSRF token. Added in v1.4.0. |

## Transactional Outbox (`outbox.*`)

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `outbox.enabled` | `false` | `transitional` | Enables the outbox: the table lives on the default database and the leasing dispatcher starts with the app. |
| `outbox.table_name` | `nucleus_outbox` | `transitional` | Name of the outbox table. |
| `outbox.lease_duration` | `30s` | `transitional` | How long a claimed message stays leased to one dispatcher instance before another may claim it. |
| `outbox.max_retries` | `5` | `transitional` | Delivery attempts before a message is marked `failed`. |
| `outbox.retry_backoff` | `1s` | `transitional` | Base delay for the exponential retry backoff. |
| `outbox.bridges.<n>.name` | — | `transitional` | Bridge instance name (required; also the routing target name). Bridge entries are configured in files only: the `NUCLEUS_*` double-underscore mapping has no list-index syntax, so per-entry env overrides do not apply. |
| `outbox.bridges.<n>.type` | — | `transitional` | Bridge type. `webhook` is the delivering implementation; `kafka` is disabled and fails boot. |
| `outbox.bridges.<n>.config.url` | — | `transitional` | Webhook bridge: delivery endpoint URL (required). |
| `outbox.bridges.<n>.config.pattern` | `*` | `transitional` | Webhook bridge: topic pattern routed to this bridge (e.g. `orders.*`). |
| `outbox.bridges.<n>.config.headers` | — | `transitional` | Webhook bridge: extra HTTP headers sent on every delivery (e.g. an `Authorization` value). The contract headers `X-Outbox-Payload-Encoding` and `X-Nucleus-Signature` cannot be overridden. |
| `outbox.bridges.<n>.config.secret` | `""` | `transitional` | Webhook bridge: HMAC-SHA256 signing secret. When set, every delivery carries `X-Nucleus-Signature: sha256=<hex>` over the exact body — the same scheme module webhooks verify, so consumers share one verifier. Empty: deliveries are unsigned and the boot log WARNs once per bridge. Added after v1.4.0. |
| `outbox.bridges.<n>.config.payload_encoding` | `base64` | `transitional` | Webhook bridge: wire shape of the body's `payload` field. `base64` (default) is the classic shape every release up to v1.4.0 emits (the payload as a base64 JSON string); `json` opts in to embedding the payload's JSON document verbatim. Every delivery declares its actual shape in `X-Outbox-Payload-Encoding`, whatever the mode. Added after v1.4.0. |

## Observability and Security

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `log_level` | `info` | `stable` | Logger level selector. |
| `log_format` | `json` | `stable` | `json`/`text` formatter contract. |
| `log_redact_extra_keys[]` | `[]` | `transitional` | Additional log attribute keys whose values the structured logger redacts, on top of the built-in denylist (`observe.DefaultRedactedKeys`). Case-insensitive. Use it for app-specific sensitive fields (`ssn`, `card_number`, …). There is intentionally **no** config key to *disable* redaction — redaction is on by default and turning it off requires an explicit code-level opt-out via `observe.NewLoggerWithRedaction` |
| `otlp_endpoint` | `""` | `stable` | Optional OTLP-HTTP push endpoint for traces + metrics. Coexists with `metrics_path` — when both are set, the MeterProvider feeds both readers. |
| `metrics_path` | `/metrics` | `stable` | Mount path for the Prometheus / OpenMetrics scrape endpoint. Empty string disables the endpoint. When non-empty, `App.New` attaches a Prometheus reader to the OTel MeterProvider and serves it at this path with `application/openmetrics-text` content type. The endpoint carries **no authentication of its own**: when enabled, restrict access at the network / reverse-proxy layer (allow-list your scraper) or mount your own guard middleware in front of it. |
| `metrics_public` | `true` | `stable` | Whether the metrics endpoint is seeded into the anonymous bootstrap allow-list. `true` (default, the historical behaviour) lets Prometheus scrape without credentials — pair it with network-layer restrictions. `false` keeps `/metrics` OUT of the allow-list, so the default-deny RBAC enforcer gates it like any user route: grant your scraper an explicit policy (e.g. `p, metrics-scraper, /metrics, *`) plus JWT auth, or use a reverse-proxy guard. Added in v1.3.0. |
| `sql_driver_instrumentation` | `false` | `stable` | Opt-in driver-level SQL instrumentation. `false` (default): the observability live SQL feed shows only `model.CRUD` traffic and the `database/sql` driver is not wrapped — zero hot-path cost. `true`: the driver is wrapped so direct `db.QueryContext`/`ExecContext` statements that bypass CRUD (outbox dispatch, SQL session stores, migrations, raw SQL) also reach the feed. CRUD statements are not double-recorded (de-duplicated by a context marker). Adds a small per-direct-statement cost when enabled; the expensive sanitize+emit still runs only when a subscriber is attached. Added in v1.3.0. |
| `rate_limit_requests` | `0` | `stable` | Sustained rate budget (0 disables). |
| `rate_limit_window` | `1m` | `stable` | Rate limit refill window. |
| `rate_limit_burst` | `0` | `stable` | Burst capacity over sustained budget. |
| `rate_limit_by_route` | `false` | `stable` | Per-route token bucket partitioning. |
| `rate_limit_by_role` | `false` | `stable` | Per-role token bucket partitioning. |
| `cors_origins[]` | `[]` (empty) | `stable` | CORS allow-list. Empty (the default) DENIES cross-origin requests — no CORS headers are emitted. A non-empty list restricts CORS to exactly these origins; the historical allow-all is the explicit opt-in `["*"]`. |
| `cors_allow_credentials` | `false` | `stable` | Emit `Access-Control-Allow-Credentials: true`. Only honored when `cors_origins` is non-empty — the Fetch standard forbids credentials with the `*` wildcard. |
| `csrf_enabled` | `false` | `stable` | Mounts the router's CSRF middleware (`router.WithCSRF`): Sec-Fetch-Site origin verification with a double-submit token fallback. Opt-in because CSRF protection only applies to cookie/session-authenticated browser routes — a pure Bearer-token API does not need it. The mvc scaffold ships with `true`. Added in v1.3.0. |
| `csrf_exempt_paths[]` | `[]` | `stable` | URL path prefixes excluded from CSRF validation (Bearer-only subtrees such as `/api/`, signature-authenticated webhook receivers). Only meaningful with `csrf_enabled: true`. |
| `trusted_proxies[]` | `[]` (empty) | `stable` | Upstream proxy addresses (IPs or CIDRs) whose `X-Forwarded-For` / `X-Real-IP` headers the RealIP middleware honors. Empty (the default) IGNORES forwarding headers and uses the immediate peer (`r.RemoteAddr`) as the client IP, preventing header-spoofed rate-limit evasion and audit-log poisoning. Set to your load balancer / reverse-proxy ranges (e.g. `["10.0.0.0/8"]`) when Nucleus runs behind one. |

## Localization, Static, Storage, Environment

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `default_locale` | `en` | `stable` | Default i18n locale. |
| `locales_path` | `locales/` | `stable` | Locale catalog path. |
| `static_prefix` | `/static/` | `stable` | Static route prefix. |
| `static_root` | `static/` | `stable` | Static collection target root. |
| `storage_driver` | — | `removed` | Removed in v0.12.0. Use `storage.provider` |
| `storage_path` | — | `removed` | Removed in v0.12.0. Use `storage.local.path` |
| `env` | `development` | `stable` | Environment mode (`development`/`production`). |
| `debug` | `false` | `stable` | Debug feature toggles. |

### Unified Storage (`storage.*`)

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `storage.provider` | `local` | `stable` | Backend: `s3`, `gcs`, `azure`, `local`. |
| `storage.default` | `private` | `stable` | Default object visibility (`private`/`public`). |
| `storage.public_url_base` | `""` | `stable` | Base URL for public objects (CDN or provider). |
| `storage.public_paths` | `{}` | `stable` | Maps URL paths to storage key prefixes. |
| `storage.s3.endpoint` | `""` | `stable` | Custom S3 endpoint (MinIO, R2). Empty = AWS. |
| `storage.s3.bucket` | `""` | `stable` | Primary S3 bucket name. |
| `storage.s3.region` | `""` | `stable` | AWS region. |
| `storage.s3.access_key_id` | `""` | `stable` | AWS access key (use env var at OS level). |
| `storage.s3.secret_access_key` | `""` | `stable` | AWS secret key (use env var at OS level). |
| `storage.s3.use_path_style` | `false` | `stable` | Path-style URLs (required for MinIO). |
| `storage.s3.public_bucket` | `""` | `stable` | Dedicated public bucket name. |
| `storage.gcs.bucket` | `""` | `stable` | Primary GCS bucket. |
| `storage.gcs.public_bucket` | `""` | `stable` | Dedicated public GCS bucket. |
| `storage.azure.account_name` | `""` | `stable` | Azure storage account name. |
| `storage.azure.account_key` | `""` | `stable` | Azure storage account key. |
| `storage.azure.container` | `""` | `stable` | Primary container name. |
| `storage.azure.public_container` | `""` | `stable` | Public container name. |
| `storage.local.path` | `storage/` | `stable` | Local filesystem root (dev only). |
| `storage.cleanup.enabled` | `false` | `stable` | Enable automatic temp file cleanup. |
| `storage.cleanup.interval` | `1h` | `stable` | Cleanup run frequency. |
| `storage.cleanup.prefix` | `_tmp/` | `stable` | Prefix for temporary objects. |
| `storage.cleanup.max_age` | `24h` | `stable` | Max age before temp files are purged. |
| `storage.circuit_breaker.enabled` | `true` | `stable` | Wrap remote provider ops (Put/Get/Delete/Exists/List/Copy/SignedURL) with a `pkg/circuit` breaker. Local provider is never wrapped. `PublicURL` is pass-through. `ErrNotFound` is not counted as a failure. |
| `storage.circuit_breaker.failure_threshold` | `5` | `stable` | Consecutive op failures required to trip the breaker open. |
| `storage.circuit_breaker.cooldown` | `30s` | `stable` | Time the breaker stays open before admitting half-open probes. |
| `storage.circuit_breaker.half_open_max_concurrent` | `1` | `stable` | In-flight probe budget while half-open. |

## Module configuration (`modules.*`)

The `modules.<name>.*` namespace is reserved for mounted modules.
Each module owns its own schema, declared as struct tags on its typed config —
the framework does not validate those keys against the tables above. Two
practical limits: the `NUCLEUS_MODULES__*` env-var pattern is not
applied (module config comes from files or code), and
`nucleus config print --effective` excludes `modules.*`
values (module schemas are open-ended and may carry secrets). See
[Concepts → Configuration → Module-specific configuration](../concepts/configuration.md#module-specific-configuration-modules)
for the full authoring guide.
