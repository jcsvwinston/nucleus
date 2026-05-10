---
sidebar_position: 2
title: Configuration
---

# Configuration

Nucleus has a single canonical configuration entry point: `nucleus.yml` at
the project root. There are no environment-only fallbacks for primary
config keys — environment variables are used to override values declared
in the file, never to introduce new ones.

## Anatomy of `nucleus.yml`

```yaml
app:
  name: myapp
  env: development          # development | staging | production
  debug: true

server:
  host: 0.0.0.0
  port: 8080
  read_timeout: 15s
  write_timeout: 15s
  shutdown_timeout: 30s

database_default: primary
databases:
  primary:
    driver: sqlite
    dsn: app.db

session:
  store: memory             # memory | sql | redis
  cookie_secure: false
  cookie_same_site: lax

auth:
  jwt_secret_env: NUCLEUS_JWT_SECRET
  password_hash: argon2id

mail:
  driver: noop              # noop | smtp | sendgrid

observability:
  log_level: info           # debug | info | warn | error
  log_format: text          # text | json
  otel_enabled: false

admin:
  enabled: true
  base_path: /admin
  rbac_policy_file: ""

multi_tenant:
  enabled: false
  resolver: subdomain       # subdomain | header
```

The above is illustrative — the canonical, exhaustive list is in
[`docs/reference/CONFIG_KEY_REGISTRY.md`](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/CONFIG_KEY_REGISTRY.md).

## Environment overrides

Any key in `nucleus.yml` can be overridden by an environment variable
named with the `NUCLEUS_` prefix and underscores:

```bash
NUCLEUS_SERVER_PORT=9090 nucleus serve
NUCLEUS_DATABASES_PRIMARY_DSN="postgres://..." nucleus migrate
```

Override semantics are flat-key: nested YAML keys are joined with
underscores. Booleans accept `true|false`; durations accept Go duration
strings (`15s`, `2m`).

## Config keys are part of the contract

Every registered config key is part of the stable surface. `internal/cli`
and `pkg/app/config.go` validate the schema at load time; unknown keys
are rejected to keep typos from silently doing nothing.

The freeze tests under `contracts/` ensure that:

- no registered config key disappears between releases without a
  deprecation entry,
- the YAML key shape (path, type) stays intact across versions inside
  the compatibility SLO window.

See [Architecture → Compatibility policy](../architecture/compatibility)
for the full rules.

## Diff against the registered schema

`nucleus diffsettings` prints the values your `nucleus.yml` resolves to,
including environment overrides. It is the fastest way to debug "why is
this app pointing at the wrong DB":

```bash
nucleus diffsettings
nucleus diffsettings --keys database_default,databases.primary.dsn
```

The output is deterministic and machine-friendly so you can pipe it to
`diff` between environments.
