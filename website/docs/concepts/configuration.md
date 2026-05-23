---
sidebar_position: 2
title: Configuration
covers:
  - pkg/nucleus.New
  - pkg/nucleus.AppBuilder
  - pkg/nucleus.AppBuilder.FromConfigFile
  - pkg/nucleus.LoadEffective
  - pkg/nucleus.ConfigSource
  - pkg/nucleus.ConfigSource.Line
  - pkg/nucleus.EffectiveValue
  - pkg/nucleus.EffectiveConfig
  - pkg/nucleus.AppBuilder.WithConfigStrict
  - pkg/nucleus.AppBuilder.WithUnknownFields
  - pkg/nucleus.ErrUnsupportedConfigFormat
  - pkg/nucleus.ErrSecurityKeyNotNullable
  - pkg/nucleus.ErrConfigFileTooLarge
  - pkg/nucleus.ErrMixedConfigFormats
  - pkg/nucleus.ErrUnknownConfigKeys
  - pkg/nucleus.MaxConfigFileBytes
  - pkg/nucleus.UnknownFieldsWarn
  - pkg/nucleus.UnknownFieldsStrict
  - pkg/nucleus.EnvProduction
  - pkg/app.LoadConfig
  - pkg/app.Config
config_keys:
  - env
  - debug
  - host
  - port
  - read_timeout
  - write_timeout
  - database_default
  - databases.default
  - session_store
  - jwt_secret
  - mail_driver
  - log_level
  - log_format
  - admin_prefix
  - multitenant.enabled
  - multitenant.resolver
---

# Configuration

Nucleus resolves configuration through a layered precedence chain:

```
struct defaults  <  nucleus.yml file(s)  <  NUCLEUS_* env vars
```

`nucleus.yml` at the project root is the primary source. `NUCLEUS_`-prefixed
environment variables override any key set by a file (or left at its struct
default). Unknown `NUCLEUS_`-prefixed variables are silently ignored — env is
a shared namespace, so stray variables are not treated as mistakes.

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

## Multi-file config loader

`AppBuilder.FromConfigFile` accepts one or more file paths. Files are
merged left-to-right: the last file wins for scalar keys, maps
deep-merge, and lists replace by default.

```go
nucleus.New().
    WithConfigStrict(true).
    FromConfigFile(
        "config/nucleus.yml",
        "config/nucleus.production.yml",
    ).
    Mount(articles.Module).
    Start()
```

**Supported formats:** `.yaml` / `.yml`, `.toml`, `.json`. Any other
extension returns `ErrUnsupportedConfigFormat`.

**Merge precedence:** `struct defaults < file[0] < file[1] < … < file[N-1]`

### List operators: `_append` and `_remove`

Two suffix operators provide additive and subtractive list semantics
that survive every supported parser format:

```yaml
# Add allowed origins without replacing the base list
cors_origins_append:
  - https://staging.example.com

# Remove an origin that was set in a base file
cors_origins_remove:
  - https://old.example.com
```

The operator keys (`<key>_append`, `<key>_remove`) are stripped from
the merged output before schema validation runs.

### `null` reverts to default

Setting a key to `null` (or `~` in YAML) reverts it to the framework's
struct default:

```yaml
log_level: null   # reverts to "info"
```

**Exception — non-nullable security keys:** certain keys whose null
revert would be a silent security degradation are rejected at boot with
`ErrSecurityKeyNotNullable`. The current non-nullable key is
`jwt_secret`. Setting it to `null` is a hard error.

### Per-file size cap

Each file is read with a **1 MiB cap** (`MaxConfigFileBytes`). Files
larger than 1 MiB are rejected with `ErrConfigFileTooLarge` before any
parser is invoked. This eliminates parser-DoS classes (YAML anchor
expansion, deeply nested JSON) that format parsers alone cannot
prevent.

## Unknown-fields handling

By default, any key in a config file that is not part of the
`app.Config` schema is rejected with `ErrUnknownConfigKeys` and a
did-you-mean hint (`UnknownFieldsStrict` mode). This keeps typos from
silently doing nothing.

```go
// Development: downgrade unknown keys to a WARN slog event
nucleus.New().
    WithUnknownFields(nucleus.UnknownFieldsWarn).
    FromConfigFile("nucleus.yml").
    Start()
```

`WithUnknownFields` and `WithConfigStrict` must be called **before**
`FromConfigFile` on the same builder chain. Calling them after
`FromConfigFile` records a deferred error that surfaces at `Build` /
`Start`.

`NUCLEUS_ENV=production` is the operator escape hatch: when set, the
loader **forces** the mode back to `strict` regardless of the
code-level `WithUnknownFields("warn")` setting, and emits a `WARN`
slog event recording the override. A build accidentally left with warn
mode is therefore not silently exposed in production deployments.

## Mixed-format file lists

Passing a mix of YAML, TOML, and JSON paths to `FromConfigFile` emits a
startup `WARN` by default and proceeds with the merge. Call
`WithConfigStrict(true)` before `FromConfigFile` to reject mixed-format
lists outright with `ErrMixedConfigFormats`:

```go
nucleus.New().
    WithConfigStrict(true).          // mixed formats → hard error
    FromConfigFile("a.yml", "b.toml"). // returns ErrMixedConfigFormats
    Start()
```

## Environment overrides

Any key in `nucleus.yml` can be overridden by an environment variable
named with the `NUCLEUS_` prefix. Nested YAML keys are joined with a
**double underscore** (`__`); a single underscore is just part of the
segment name:

```bash
NUCLEUS_PORT=9090 nucleus serve
NUCLEUS_DATABASES__PRIMARY__DSN="postgres://..." nucleus migrate
NUCLEUS_LOG_LEVEL=debug nucleus serve
```

This applies in both the lower-level `app.LoadConfig` path and — since
ADR-010 Phase 3.1 — in the fluent `nucleus.New().FromConfigFile(...)` builder
path. The full precedence chain honoured by `FromConfigFile` is:

```
struct defaults  <  file[0]  <  …  <  file[N-1]  <  NUCLEUS_* env vars
```

Unknown `NUCLEUS_`-prefixed variables (ones that do not map to a registered
config key) are silently ignored. Env is a shared ambient namespace; an
unrecognised variable is not treated as an authored mistake the way an
unknown key in a config file is.

Booleans accept `true|false`; durations accept Go duration strings (`15s`,
`2m`). Non-nullable security keys (e.g. `NUCLEUS_JWT_SECRET`) reject an empty
string the same way the file layer rejects `null`.

## Config keys are part of the contract

Every registered config key is part of the stable surface. Unknown keys
reject the load with a did-you-mean hint by default (strict mode). The
`pkg/nucleus` builder exposes `AppBuilder.WithUnknownFields(nucleus.UnknownFieldsWarn)` to
downgrade unknown-key failures to `WARN`-level slog events during
development; `NUCLEUS_ENV=production` forces strict mode regardless of
the code-level setting.

The freeze tests under `contracts/` ensure that:

- no registered config key disappears between releases without a
  deprecation entry,
- the YAML key shape (path, type) stays intact across versions inside
  the compatibility SLO window.

See [Architecture → Compatibility policy](../architecture/compatibility.md)
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

## Inspect the effective merged config

`nucleus config print --effective` shows the fully merged view across
one or more config files — including environment-variable overrides — with
a per-key source label so you can see exactly which file or env var each
value came from:

```bash
nucleus config print --effective \
  --config config/nucleus.yml \
  --config config/nucleus.production.yml
```

Example output (when `NUCLEUS_PORT=9090` is set in the environment):

```
port = 9090 [env:NUCLEUS_PORT]
host = 0.0.0.0 [default]
databases.primary.dsn = [REDACTED] [yaml:config/nucleus.production.yml:14]
log_level = info [yaml:config/nucleus.yml:8]
```

Source labels follow these rules:

| Label | Meaning |
| ----- | ------- |
| `[default]` | Value comes from the framework struct default; no file set it. |
| `[yaml:path:line]` | Set in a YAML file; `line` is the 1-based line where the key appears. |
| `[yaml:path]` | YAML file, but the line could not be determined (e.g. anchor/alias, `_append`/`_remove` operator). |
| `[toml:path]` | Set in a TOML file (line numbers not available for TOML). |
| `[json:path]` | Set in a JSON file (line numbers not available for JSON). |
| `[env:NUCLEUS_VAR]` | Overridden by a `NUCLEUS_`-prefixed environment variable. |

Secret values are automatically redacted. Pass `--json` for structured
output. See [CLI overview → Effective config](../cli/overview.md#effective-config-nucleus-config-print---effective)
for the full flag reference.

## Runtime HTTP inspection (`GET /_/config`)

When the admin subsystem is active, Nucleus also exposes the same
effective-config view over HTTP at `GET /_/config`. It is the HTTP
counterpart to `nucleus config print --effective` — same merged output,
same secret redaction — for tooling and dashboards that hold a valid
admin session.

The endpoint is protected by the admin session gate (unauthenticated
requests receive `403 Forbidden`) and always sets
`Cache-Control: no-store`. It is not mounted on apps built with
`WithoutDefaults()`.

See [Observability → `/_/config`](../features/observability.md#_config)
for the full request/response shape and mounting conditions.
