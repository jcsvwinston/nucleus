# Config Key Registry

Reference date: 2026-04-07.
Status: Current.

This file is the configuration key contract registry for GoFrame.

Source of truth:

- `pkg/app/config.go` (`Config` struct tags + defaults)
- environment override prefix: `GOFRAME_`

Example mapping:

- `port` -> `GOFRAME_PORT`
- `databases.analytics.url` -> `GOFRAME_DATABASES__ANALYTICS__URL`

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
| `databases.<alias>.url` | `databases.default.url=sqlite://goframe.db` | `stable` + `experimental` | Stable schemes: `sqlite://`, `postgres://`, `postgresql://`, `mysql://`; exploratory schemes: `sqlserver://`/`mssql://`, `oracle://`. |
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
| `session_table` | `goframe_sessions` | `stable` | SQL session table name. |
| `session_cookie_name` | `session` | `stable` | Session cookie name. |
| `session_cookie_domain` | `""` | `stable` | Session cookie domain. |
| `session_cookie_path` | `/` | `stable` | Session cookie path. |
| `session_cookie_secure` | `false` | `stable` | Secure cookie requirement (set true in TLS/prod). |
| `session_cookie_samesite` | `lax` | `stable` | SameSite policy string. |
| `session_idle_timeout` | `0` | `stable` | Optional idle timeout override. |
| `session_redis_prefix` | `goframe:sessions:` | `stable` | Session Redis key prefix. |

## Auth

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `jwt_secret` | `""` | `stable` | Required for signed JWT auth flows. |
| `jwt_expiry` | `24h` | `stable` | JWT lifetime default. |

## Admin

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `admin_prefix` | `/admin` | `stable` | Admin mount prefix. |
| `admin_title` | `GoFrame Admin` | `transitional` | UI labeling may evolve with admin UX maturation. |

## Mail

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `mail_driver` | `noop` | `stable` | Built-in and plugin-backed provider selection. |
| `mail_from` | `noreply@localhost` | `stable` | Default sender. |
| `smtp_host` | `""` | `stable` | SMTP host. |
| `smtp_port` | `587` | `stable` | SMTP port. |
| `smtp_user` | `""` | `stable` | SMTP user. |
| `smtp_pass` | `""` | `stable` | SMTP password. |
| `sendgrid_api_key` | `""` | `stable` | SendGrid API key. |
| `sendgrid_endpoint` | `https://api.sendgrid.com/v3/mail/send` | `stable` | SendGrid endpoint override. |

## Observability and Security

| Key | Default | Lifecycle | Notes |
| --- | --- | --- | --- |
| `log_level` | `info` | `stable` | Logger level selector. |
| `log_format` | `json` | `stable` | `json`/`text` formatter contract. |
| `otlp_endpoint` | `""` | `stable` | Optional OTLP export endpoint. |
| `metrics_path` | `/metrics` | `stable` | Metrics endpoint path. |
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
| `storage_driver` | `local` | `stable` | Storage backend selector. |
| `storage_path` | `uploads/` | `stable` | Local storage root path. |
| `env` | `development` | `stable` | Environment mode (`development`/`production`). |
| `debug` | `false` | `stable` | Debug feature toggles. |

## Contract Rules

- Key names are case-sensitive and snake_case.
- Renaming a `stable` key is considered a breaking change.
- Changing semantics of a `stable` key requires migration notes and compatibility review.
- New keys should default to additive/non-breaking behavior.
