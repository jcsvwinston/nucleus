---
sidebar_position: 1
title: Overview
covers:
  - pkg/nucleus.LoadEffective
  - pkg/nucleus.ConfigSource
  - pkg/nucleus.ConfigSource.Line
  - pkg/nucleus.EffectiveValue
  - pkg/nucleus.EffectiveConfig
config_keys:
  - databases.default
  - database_default
---

# CLI overview

The `nucleus` binary is the operations interface for any Nucleus project —
scaffolding, migrations, fixtures, user management and inspection. It is
designed to be safe in a deploy pipeline, so every command:

- reads `nucleus.yml` from the current working directory by default,
- emits structured, JSON-friendly output where that makes sense,
- exits non-zero on failure, with a meaningful message on stderr.

The tables below group the commands by purpose. The canonical, exhaustive
inventory is the
[CLI contract matrix](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/CLI_CONTRACT_MATRIX.md).

## Project lifecycle

| Command                       | What it does                                          |
| ----------------------------- | ----------------------------------------------------- |
| `nucleus new <name>`          | Scaffold a new project (`--template mvc\|api`).       |
| `nucleus startapp <name>`     | Create an app scaffold inside an existing project (same mountable-module artifacts as `generate resource`, plus a server-rendered page). |
| `nucleus serve`               | Start an HTTP server built from configuration only — full-stack, or core-only with `--without-defaults`. **Your modules are not mounted:** to serve your application, run your own binary with `go run .`. |
| `nucleus version`             | Print the CLI version. A binary installed with `go install …@vX.Y.Z` reports that exact version, read from its own build info. |
| `nucleus health`              | Check configured dependencies health.                 |
| `nucleus doctor`              | Run diagnostic checks for framework subsystems.       |
| `nucleus doctor --check security` | Flag high-risk security misconfiguration before you deploy. |
| `nucleus doctor --check auth` | Review the authentication chain: per-backend settings and whether a break-glass path exists. |
| `nucleus generate resource <name>` | Scaffold a feature spread across the layer packages: model, migration for the configured dialect, database-backed repository, service, `nucleus.Context` controller, tests, and a mountable module. Wiring it up is one line — `nucleus.New().Mount(modules.<Name>Module())`. The command prints the exact routes the module mounts; `--with-policy` also seeds the `rbac_policy.csv` rows and the CSRF exemption those routes need to answer their first request (anonymous development defaults — scope them down before production). |
| `nucleus generate module <name>`   | Scaffold the same feature as **one** self-contained package under `internal/<name>/`: model and storage, controller, and a module carrying its own policy rows, CSRF exemption, embedded migrations (applied on start) and page template. Mounting it needs no `rbac_policy.csv` or `nucleus.yml` edits, and no migrate step. |
| `nucleus test`                | Run Go tests with project-friendly defaults.          |
| `nucleus testserver`          | Load fixture data and start a local server (configuration-only, like `serve`: your modules are not mounted). |
| `nucleus openapi`             | Export the experimental OpenAPI project contract.     |

## Database & migrations

| Command                          | What it does                                                    |
| -------------------------------- | --------------------------------------------------------------- |
| `nucleus migrate`                | Apply pending migrations.                                       |
| `nucleus migrate status`         | Show plan vs. applied.                                          |
| `nucleus migrate drift`          | Detect applied migrations whose `.up.sql` file is missing on disk. Exits non-zero when drift is detected (CI-friendly). |
| `nucleus migrate down`           | Roll back the most recent batch.                                |
| `nucleus migrate steps <n>`      | Apply exactly N migrations (subcommand of `migrate`, not a top-level flag). |
| `nucleus sqlmigrate`             | Print SQL for a named migration file without applying it.       |
| `nucleus sqlflush`               | Print the SQL statements that `flush` would execute.            |
| `nucleus sqlsequencereset`       | Print SQL statements to reset table sequences/auto-increment counters. |
| `nucleus squashmigrations`       | Squash a migration range into a single migration file.          |
| `nucleus optimizemigration`      | Optimize SQL statements in one migration file.                  |
| `nucleus inspectdb`              | Inspect a live DB schema and generate Go model structs.         |
| `nucleus ogrinspect`             | Inspect geospatial tables and generate Go model structs.        |
| `nucleus seed`                   | Execute SQL seed files.                                         |
| `nucleus dumpdata`               | Export DB rows as JSON fixtures.                                |
| `nucleus loaddata`               | Import JSON fixtures into DB tables.                            |
| `nucleus flush`                  | Delete all data from database tables (keeps migration history). |
| `nucleus outbox requeue [id ...]` | Return failed outbox messages to `pending` with a fresh retry budget so the dispatcher retries them. With no ids, requeues every failed message; ids not currently failed are left untouched. Inspect counts first with `nucleus doctor --check outbox`. |

## Users & sessions

| Command                       | What it does                                      |
| ----------------------------- | ------------------------------------------------- |
| `nucleus createuser`          | Create or update an admin user.                   |
| `nucleus changepassword`      | Update an admin user's password.                  |
| `nucleus clearsessions`       | Delete expired or all session rows.               |
| `nucleus createcachetable`    | Create the SQL table used by the database-backed cache. |

## Inspection & settings

| Command                              | What it does                                              |
| ------------------------------------ | --------------------------------------------------------- |
| `nucleus routes`                     | List framework-owned routes. Routes registered by the modules of **your** binary mount at application startup and are not visible here; with `env: development`, booting your app logs one `module route mounted` line per route — that log is the full table. |
| `nucleus diffsettings`               | Show configuration differences from defaults.             |
| `nucleus config print --effective`   | Print the effective merged configuration with per-key provenance (source kind + path). |
| `nucleus shell`                      | Interactive SQL shell bound to the configured database (see below). |

## Effective config (`nucleus config print --effective`)

`nucleus config print --effective` prints the fully merged configuration and
names the source of every key. It is the primary tool for answering "why is
this value wrong in this environment".

```bash
# Single config file
nucleus config print --effective --config nucleus.yml

# Multiple files — merged left-to-right (defaults < file[0] < … < file[N-1])
nucleus config print --effective \
  --config config/nucleus.yml \
  --config config/nucleus.production.yml

# Structured JSON output
nucleus config print --effective --config nucleus.yml --json
```

**If the merged configuration would not boot**, `print` still renders it —
that is what you reach for when something is wrong — and writes the loader's
own rejection to **stderr**:

```
warning: this configuration will not boot: nucleus: invalid configuration
reference: session_cookie_samesite="none" requires session_cookie_secure=true
```

The warning never touches stdout, so `--json` stays pipeable.

**Text output format:** one line per key in `key = value [source]` notation.
Source labels:

| Label | Meaning |
| ----- | ------- |
| `[default]` | Value comes from the framework struct default; no file or env var set it. |
| `[yaml:path:line]` | Set in a YAML file at the given 1-based line number. |
| `[yaml:path]` | Set in a YAML file; line could not be determined (anchor/alias or operator key). |
| `[toml:path]` | Set in a TOML file (line numbers not available for TOML). |
| `[json:path]` | Set in a JSON file (line numbers not available for JSON). |
| `[env:NUCLEUS_VAR]` | Overridden by a `NUCLEUS_`-prefixed environment variable. |

```
port = 9090 [env:NUCLEUS_PORT]
host = 0.0.0.0 [default]
databases.primary.dsn = [REDACTED] [yaml:config/nucleus.production.yml:22]
log_level = info [yaml:config/nucleus.yml:8]
```

- Keys resolved entirely from framework defaults are labelled `[default]`.
- Secret values (DB connection strings, `jwt_secret`, passwords, tokens, etc.)
  are automatically redacted and shown as `[REDACTED]`.
- Environment-variable overrides appear as `[env:NUCLEUS_<KEY>]` and win over
  all file layers (the full precedence is `defaults < files < env`).
- YAML file keys carry a 1-based source line when available. TOML and JSON do
  not expose a standard line API and always show `[kind:path]` with no line.

**`--json` output** is a structured document with the same fields:

```json
{
  "values": [
    { "key": "port", "value": "9090", "redacted": false, "source": { "kind": "env", "path": "NUCLEUS_PORT" } },
    { "key": "databases.primary.dsn", "value": "", "redacted": true, "source": { "kind": "yaml", "path": "config/nucleus.production.yml", "line": 22 } },
    { "key": "host", "value": "0.0.0.0", "redacted": false, "source": { "kind": "default" } }
  ]
}
```

**Flags:**

| Flag                    | Default    | Description                                                  |
| ----------------------- | ---------- | ------------------------------------------------------------ |
| `--config <path>`       | _(none)_   | Config file to load. Repeatable; files merge left-to-right.  |
| `--json`                | `false`    | Emit structured JSON instead of plain-text key = value lines. |

The underlying loader is exposed programmatically via `pkg/nucleus.LoadEffective`,
which returns an `EffectiveConfig` whose `Values []EffectiveValue` carry the same
`Key`, `Value`, `Redacted`, and `Source` (`ConfigSource`) fields.

## SQL shell (`nucleus shell`)

`nucleus shell` opens an interactive **SQL shell** against the configured
database. It does not evaluate Go expressions.

```bash
# Interactive REPL (exit with 'exit', 'quit', or '\q')
nucleus shell --config nucleus.yml

# Execute a single SQL statement and exit
nucleus shell --config nucleus.yml -c "SELECT COUNT(*) FROM users"
nucleus shell --config nucleus.yml --command "SELECT id FROM sessions LIMIT 5"

# Target a non-default database alias
nucleus shell --config nucleus.yml --database analytics

# Read-only sandbox mode — only SELECT/EXPLAIN/SHOW/DESCRIBE/VALUES allowed
nucleus shell --config nucleus.yml --sandbox

# Set a per-statement timeout (default 10s)
nucleus shell --config nucleus.yml --timeout 30s

# Pipe a SQL script via stdin
cat schema_audit.sql | nucleus shell --config nucleus.yml
```

**Flags:**

| Flag                       | Default    | Description                                                  |
| -------------------------- | ---------- | ------------------------------------------------------------ |
| `--config <path>`          | _(empty)_  | Path to the `nucleus.yml` config file.                       |
| `--database <alias>`       | _(empty)_  | Database alias to use; defaults to `database_default`.       |
| `-c` / `--command <sql>`   | _(empty)_  | Execute one SQL statement and exit (non-interactive mode).   |
| `--sandbox`                | `false`    | Allow only read-only statements (`SELECT`, `EXPLAIN`, `SHOW`, `DESCRIBE`, `VALUES`). |
| `--timeout <duration>`     | `10s`      | Per-statement execution timeout.                             |

In sandbox mode the shell rejects any statement that does not start with
`SELECT`, `EXPLAIN`, `SHOW`, `DESCRIBE`, `DESC` or `VALUES`. Use it whenever
the session — human or automated — should never be able to mutate production
data.

## Mail & plugins

| Command                       | What it does                                              |
| ----------------------------- | --------------------------------------------------------- |
| `nucleus mailproviders`       | List registered and external mail providers.              |
| `nucleus sendtestemail`       | Send a test email through the configured mail provider.   |
| `nucleus plugin list`         | Discover and list plugin providers/capabilities.          |
| `nucleus plugin doctor`       | Run health checks on configured plugins.                  |
| `nucleus plugin test`         | Test a specific plugin provider and capability.           |

## Static assets, i18n, and content types

| Command                              | What it does                                      |
| ------------------------------------ | ------------------------------------------------- |
| `nucleus collectstatic`              | Collect static assets into configured `static_root`. |
| `nucleus findstatic`                 | Find static assets across discovered source directories. |
| `nucleus makemessages`               | Extract translatable strings into `.po` catalogs. |
| `nucleus compilemessages`            | Compile `.po` catalogues into JSON bundles.       |
| `nucleus remove_stale_contenttypes`  | Delete stale rows from the content types table.   |

## Output style

Every command accepts top-level output style flags:

```bash
nucleus --output json   migrate status
nucleus --output plain  routes
nucleus --json          diffsettings   # shorthand for --output json
nucleus --color never   doctor
nucleus --no-symbols    health
```

The JSON output keys are part of the compatibility contract, and a freeze
test pins them: a command cannot silently change the shape of what it emits.

## Help

```bash
nucleus help
nucleus help migrate
nucleus migrate --help
```

`nucleus help <command>` is the canonical inline reference. The website
cannot stay perfectly synchronized with every flag — when in doubt, ask
the binary.

## Extensions

External binaries on `PATH` named `nucleus-<name>` are automatically
available as `nucleus <name>`. This is the plugin extension point for
project-local or organization-wide commands.
