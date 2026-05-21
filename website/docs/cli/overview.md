---
sidebar_position: 1
title: Overview
---

# CLI overview

The `nucleus` binary is the deterministic operations interface for any
Nucleus project. Every command:

- reads `nucleus.yml` from the current working directory by default,
- emits structured (JSON-friendly) output where structured output makes
  sense,
- exits with a non-zero status on failure and a meaningful message on
  stderr.

The full inventory is canonical at
[`docs/reference/CLI_CONTRACT_MATRIX.md`](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/CLI_CONTRACT_MATRIX.md).
The summary below groups the commands by purpose.

## Project lifecycle

| Command                       | What it does                                          |
| ----------------------------- | ----------------------------------------------------- |
| `nucleus new <name>`          | Scaffold a new project (`--template mvc\|api`).       |
| `nucleus startapp <name>`     | Create an app scaffold inside an existing project.    |
| `nucleus serve`               | Start the HTTP server.                                |
| `nucleus health`              | Check configured dependencies health.                 |
| `nucleus doctor`              | Run diagnostic checks for framework subsystems.       |
| `nucleus wizard`              | Interactive wizard for complex commands (e.g. `--type new`, `--type startapp`, `--type inspectdb`). |
| `nucleus generate`            | Generate model, handler, or migration scaffolds.      |
| `nucleus routes`              | List registered HTTP routes.                          |
| `nucleus test`                | Run Go tests with project-friendly defaults.          |
| `nucleus testserver`          | Load fixture data and start a local server.           |
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

## Users & sessions

| Command                       | What it does                                      |
| ----------------------------- | ------------------------------------------------- |
| `nucleus createuser`          | Create or update an admin user.                   |
| `nucleus changepassword`      | Update an admin user's password.                  |
| `nucleus clearsessions`       | Delete expired or all session rows.               |
| `nucleus createcachetable`    | Create the SQL table used by the database-backed cache. |

## Inspection & settings

| Command                       | What it does                                              |
| ----------------------------- | --------------------------------------------------------- |
| `nucleus routes`              | List registered routes.                                   |
| `nucleus diffsettings`        | Show configuration differences from defaults.             |
| `nucleus shell`               | Interactive SQL shell bound to the configured database (see below). |

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

In sandbox mode the shell rejects any statement that is not a `SELECT`,
`EXPLAIN`, `SHOW`, `DESCRIBE`, `DESC`, or `VALUES` prefix. This makes it
safe to hand to junior operators or automation that should never mutate
production data.

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

The JSON output keys are part of the contract and pinned by
`contracts/cli_json_freeze_test.go`.

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
