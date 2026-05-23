# Config Key Registry

Reference date: 2026-05-23.
Status: Current.

This file is the configuration key contract registry for Nucleus.

Source of truth:

- `pkg/app/config.go` (`Config` struct tags + defaults)
- environment override prefix: `NUCLEUS_`

Example mapping:

- `port` -> `NUCLEUS_PORT`
- `databases.analytics.url` -> `NUCLEUS_DATABASES__ANALYTICS__URL`

## Configuration Precedence

The loader applies layers in this order (lowest to highest priority):

```
struct defaults < config files < NUCLEUS_* env vars < CLI flags < programmatic
```

The **struct defaults** (`app.DefaultConfig()`) seed every key before any
file or environment variable is consulted. Config files are merged
left-to-right when multiple paths are given (`FromConfigFile(a, b)`: `b`
wins). Environment variables then override whatever the files set.
CLI-flag and programmatic layers (not yet implemented in the fluent path)
would sit above env.

**Both the `app.LoadConfig` path and the `pkg/nucleus` fluent builder
(`FromConfigFile`) now apply the `NUCLEUS_` env layer.** Prior to Phase 3.1
the fluent builder ignored env vars; as of Phase 3.1 both paths honour the
full precedence chain.

### NUCLEUS_* key mapping convention

- Flat config keys use a single underscore: `port` → `NUCLEUS_PORT`.
- Nested keys use **double underscores** as the segment separator:
  `databases.<alias>.url` → `NUCLEUS_DATABASES__<ALIAS>__URL`.
- Key names are lowercased after prefix stripping.

### Provenance in the effective config

`nucleus config print --effective` and `GET /_/config` report the source of
each key:

| Source | CLI rendering | Notes |
|--------|---------------|-------|
| struct default | `[default]` | No file or env override was set. |
| config file (YAML) | `[yaml:path:line]` | 1-based source line reported for YAML only. |
| config file (TOML) | `[toml:path]` | TOML has no stable line API; line is omitted. |
| config file (JSON) | `[json:path]` | No standard JSON line API; line is omitted. |
| `NUCLEUS_*` env var | `[env:NUCLEUS_VAR]` | The originating variable name is recorded. |

### Non-nullable security keys

Certain keys must never be silently reverted to their struct default:

- `jwt_secret` — setting it to `null` in a file, or setting
  `NUCLEUS_JWT_SECRET=` (empty value) via env, is a **boot error**
  (`ErrSecurityKeyNotNullable`) rather than a silent no-op.

Unknown `NUCLEUS_`-prefixed env vars are silently ignored (env is an ambient
shared namespace). Unknown keys in config *files* are rejected with
`ErrUnknownConfigKeys` (or demoted to a `WARN` with
`WithUnknownFields("warn")`). `NUCLEUS_ENV=production` forces strict mode
regardless of the code-level setting.

## Lifecycle Tags

- `stable`: key name and semantic meaning are contract surfaces.
- `transitional`: key is supported but semantics may still refine.
- `experimental`: key/semantic has no compatibility guarantee yet.

## Server

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `host` | `0.0.0.0` | `stable` | Bind host. |
| `port` | `8080` | `stable` | Bind port. |
| `read_timeout` | `30s` | `stable` | HTTP read timeout. |
| `write_timeout` | `60s` | `stable` | HTTP write timeout. |
| `idle_timeout` | `120s` | `stable` | HTTP idle timeout. |

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
| `session_cookie_secure` | `false` | `stable` | Secure cookie requirement (set true in TLS/prod). |
| `session_cookie_samesite` | `lax` | `stable` | SameSite policy string. |
| `session_idle_timeout` | `0` | `stable` | Optional idle timeout override. |
| `session_redis_prefix` | `nucleus:sessions:` | `stable` | Session Redis key prefix. |

## Auth

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `jwt_secret` | `""` | `stable` | Single-secret HS256 used as legacy fallback when `jwt_keys` is empty. Tokens carry no `kid` header. When `jwt_keys[]` is non-empty this key is ignored. See `jwt_keys[]` for the production multi-key path. |
| `jwt_expiry` | `24h` | `stable` | JWT lifetime default. |
| `jwt_issuer` | `""` | `stable` | Issuer claim (`iss`) stamped into every token minted by `App.JWT`. Used by both single-secret and multi-key managers. |
| `jwt_keys[]` | `[]` | `stable` | Ordered keyset consumed by `App.New` to build a `*auth.JWTManager` via `auth.NewJWTManagerFromKeys`. Each entry is a `JWTKeySpec` sub-object — see table below. When non-empty, `jwt_secret` is ignored. |
| `jwt_current_kid` | `""` | `stable` | `kid` value that identifies the active signing key within `jwt_keys[]`. Must match one entry's `kid`. New tokens are signed with this key; all keyset keys remain valid for validation. |

#### `jwt_keys[]` entry fields (`JWTKeySpec`)

| Field | Type | Notes |
| --- | --- | --- |
| `kid` | string | Unique key identifier stamped in token `kid` header. Required. |
| `algorithm` | string | `HS256`, `RS256`, or `ES256`. Required. |
| `secret_env` | string | Resolver reference to the HMAC secret (HS256 only). See reference forms below. |
| `pem_path` | string | Filesystem path to a PEM-encoded private key (RS256: RSA, PKCS#1/PKCS#8; ES256: ECDSA P-256, SEC1/PKCS#8). Rejects PEM with trailing content. |
| `pem_env` | string | Resolver reference to PEM bytes (RS256 / ES256). See reference forms below. |

Exactly one of `secret_env` / `pem_path` / `pem_env` must be set per entry. Key material is never read from tracked YAML files.

**Resolver reference forms** — `secret_env` and `pem_env` are dispatched by scheme prefix (`transitional` — the `aws-sm:` scheme was introduced in `v0.7.x` per ADR-005):

| Reference | Resolved from |
| --- | --- |
| `MY_VAR` | environment variable `MY_VAR` (historical behaviour) |
| `env:MY_VAR` | environment variable `MY_VAR` (explicit form) |
| `aws-sm:<secret-id>` | AWS Secrets Manager secret `<secret-id>` |
| `aws-sm:<secret-id>#<json-key>` | one string field of a JSON-object AWS secret |

The AWS Secrets Manager resolver is constructed lazily — only when at least one `jwt_keys[]` entry uses an `aws-sm:` reference — and uses the standard AWS credential chain. `pem_path` is always a filesystem path and is not a resolver reference.

## Admin

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `admin_prefix` | `/admin` | `stable` | Admin mount prefix. |
| `admin_title` | `Nucleus Admin` | `transitional` | UI labeling may evolve with admin UX maturation. |
| `admin_auth_database` | `""` | `stable` | Optional dedicated DB alias for admin auth user store. |
| `admin_bootstrap_username` | `""` | `stable` | Initial admin user created on first boot (one-time). |
| `admin_bootstrap_email` | `""` | `stable` | Email for the bootstrap admin user. |
| `admin_bootstrap_password` | `""` | `stable` | Password for the bootstrap admin user (must be changed). |
| `admin_live_exclude_patterns[]` | `[/admin]` | `stable` | Path patterns excluded from live HTTP capture. |
| `admin_cluster_enabled` | `false` | `stable` | Enables cluster-aware admin live telemetry relay. |
| `admin_cluster_redis_url` | `""` | `stable` | Redis URL override for admin cluster relay (falls back to `redis_url`). |
| `admin_cluster_channel` | `nucleus:admin:live:v1` | `stable` | Pub/Sub channel used by the admin live cluster relay. |
| `admin_cluster_node_id` | `""` | `stable` | Optional explicit runtime node id used in cluster telemetry events. |
| `admin_cluster_token` | `""` | `stable` | Optional shared token to reject untrusted cluster relay events. |
| `admin_trace_url_template` | `""` | `stable` | Optional external trace URL template (`{trace_id}` placeholder) used by admin trace links. |
| `admin_rbac_policy_file` | `""` | `stable` | Path to Casbin RBAC CSV policy file. **CSV rows now require a 4th column** (`allow` / `deny`) — the model uses deny-override semantics. Programmatic callers use `Enforcer.AddPolicy` (auto-stamps `allow`) and `Enforcer.Deny`. |

## Mail

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `mail_driver` | `noop` | `stable` | Built-in and plugin-backed provider selection. |
| `mail_from` | `noreply@localhost` | `stable` | Default sender. |
| `smtp_host` | `""` | `stable` | SMTP host. |
| `smtp_port` | `587` | `stable` | SMTP port. |
| `smtp_user` | `""` | `stable` | SMTP user. |
| `smtp_pass` | `""` | `stable` | SMTP password. |
| `mail_circuit_breaker.enabled` | `true` | `transitional` | Wrap `mail.Sender.Send` with a `pkg/circuit` breaker. `noop` driver is never wrapped. `Healthy` (SMTP HELO probe) bypasses the breaker so `/healthz` observes recovery. |
| `mail_circuit_breaker.failure_threshold` | `5` | `transitional` | Consecutive Send failures required to trip the breaker open. |
| `mail_circuit_breaker.cooldown` | `30s` | `transitional` | Time the breaker stays open before admitting half-open probes. |
| `mail_circuit_breaker.half_open_max_concurrent` | `1` | `transitional` | In-flight probe budget while half-open. |

For vendor-specific drivers (SendGrid, Mailgun, AWS SES, Postmark, Resend, …) install `nucleus-plugin-<driver>` on `PATH`. The framework does not register their config keys — each plugin reads its own credentials per its documented contract (typically env vars). See [MA-2026-002](../migration_assistants/MA-2026-002-sendgrid-builtin-to-plugin.md) for the migration path away from the previously built-in `sendgrid` driver.

## Observability and Security

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `log_level` | `info` | `stable` | Logger level selector. |
| `log_format` | `json` | `stable` | `json`/`text` formatter contract. |
| `log_redact_extra_keys[]` | `[]` | `transitional` | Additional log attribute keys whose values the structured logger redacts, on top of the built-in denylist (`observe.DefaultRedactedKeys`). Case-insensitive. Use it for app-specific sensitive fields (`ssn`, `card_number`, …). There is intentionally **no** config key to *disable* redaction — redaction is on by default and turning it off requires an explicit code-level opt-out via `observe.NewLoggerWithRedaction`. See ADR-007. |
| `otlp_endpoint` | `""` | `stable` | Optional OTLP-HTTP push endpoint for traces + metrics. Coexists with `metrics_path` — when both are set, the MeterProvider feeds both readers. |
| `metrics_path` | `/metrics` | `stable` | Mount path for the Prometheus / OpenMetrics scrape endpoint. Empty string disables the endpoint. When non-empty, `App.New` attaches a Prometheus reader to the OTel MeterProvider and serves it at this path with `application/openmetrics-text` content type. |
| `rate_limit_requests` | `0` | `stable` | Sustained rate budget (0 disables). |
| `rate_limit_window` | `1m` | `stable` | Rate limit refill window. |
| `rate_limit_burst` | `0` | `stable` | Burst capacity over sustained budget. |
| `rate_limit_by_route` | `false` | `stable` | Per-route token bucket partitioning. |
| `rate_limit_by_role` | `false` | `stable` | Per-role token bucket partitioning. |

## Localization, Static, Storage, Environment

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `default_locale` | `en` | `stable` | Default i18n locale. |
| `locales_path` | `locales/` | `stable` | Locale catalog path. |
| `static_prefix` | `/static/` | `stable` | Static route prefix. |
| `static_root` | `static/` | `stable` | Static collection target root. |
| `storage_driver` | `local` | `stable` (deprecated) | Legacy storage backend. Use `storage.provider` instead. |
| `storage_path` | `uploads/` | `stable` (deprecated) | Legacy local storage root. Use `storage.local.path` instead. |
| `env` | `development` | `stable` | Environment mode (`development`/`production`). |
| `debug` | `false` | `stable` | Debug feature toggles. |

### Unified Storage (`storage.*`)

The new storage config replaces the legacy `storage_driver`/`storage_path` keys.
See `docs/STORAGE_GUIDE.md` for full examples.

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
| `storage.circuit_breaker.enabled` | `true` | `transitional` | Wrap remote provider ops (Put/Get/Delete/Exists/List/Copy/SignedURL) with a `pkg/circuit` breaker. Local provider is never wrapped. `PublicURL` is pass-through. `ErrNotFound` is not counted as a failure. |
| `storage.circuit_breaker.failure_threshold` | `5` | `transitional` | Consecutive op failures required to trip the breaker open. |
| `storage.circuit_breaker.cooldown` | `30s` | `transitional` | Time the breaker stays open before admitting half-open probes. |
| `storage.circuit_breaker.half_open_max_concurrent` | `1` | `transitional` | In-flight probe budget while half-open. |

## Contract Rules

- Key names are case-sensitive and snake_case.
- Renaming a `stable` key is considered a breaking change.
- Changing semantics of a `stable` key requires migration notes and compatibility review.
- New keys should default to additive/non-breaking behavior.
