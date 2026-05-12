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

| Command                       | What it does                                      |
| ----------------------------- | ------------------------------------------------- |
| `nucleus new <name>`          | Scaffold a new project (`--template mvc|api`).    |
| `nucleus serve`               | Start the HTTP server.                            |
| `nucleus health`              | Probe `/healthz` of the running app.              |
| `nucleus doctor`              | Self-check the local environment.                 |

## Database & migrations

| Command                       | What it does                                      |
| ----------------------------- | ------------------------------------------------- |
| `nucleus migrate`             | Apply pending migrations.                         |
| `nucleus migrate status`      | Show plan vs. applied.                            |
| `nucleus migrate down`        | Roll back the most recent batch.                  |
| `nucleus generate migration`  | Scaffold a new migration (with `--from-model`).   |
| `nucleus inspectdb`           | Inspect a live database schema.                   |
| `nucleus seed`                | Load seed data (fixtures).                        |
| `nucleus dumpdata`            | Export model rows to JSON / CSV.                  |
| `nucleus loaddata`            | Import model rows from JSON / CSV.                |

## Users & sessions

| Command                       | What it does                                      |
| ----------------------------- | ------------------------------------------------- |
| `nucleus createuser`          | Create an admin user interactively.               |
| `nucleus changepassword`      | Change a user's password.                         |
| `nucleus clearsessions`       | Purge expired sessions from the configured store. |

## Inspection

| Command                       | What it does                                      |
| ----------------------------- | ------------------------------------------------- |
| `nucleus routes`              | List registered routes.                           |
| `nucleus diffsettings`        | Print the resolved configuration.                 |
| `nucleus shell`               | Drop into a Go-evaluated REPL bound to your app.  |

## Mail & plugins

| Command                       | What it does                                      |
| ----------------------------- | ------------------------------------------------- |
| `nucleus mailproviders`       | List registered mail drivers and plugin probes.   |
| `nucleus sendtestemail`       | Send a one-off email through the configured driver. |
| `nucleus plugin list`         | Discover `nucleus-plugin-*` binaries on `PATH`.   |

## Static assets, i18n, and content types

| Command                              | What it does                                      |
| ------------------------------------ | ------------------------------------------------- |
| `nucleus collectstatic`              | Stage static assets for production.               |
| `nucleus findstatic`                 | Find static assets across discovered source dirs. |
| `nucleus makemessages`               | Extract translatable strings into `.po` catalogs. |
| `nucleus compilemessages`            | Compile `.po` catalogues into JSON bundles.       |
| `nucleus remove_stale_contenttypes`  | Delete stale rows from the content types table.   |

## Output style

Every command accepts a top-level output style flag:

```bash
nucleus migrate status --output json
nucleus routes          --output table
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
