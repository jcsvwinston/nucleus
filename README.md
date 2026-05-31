# Nucleus

[![CI](https://github.com/jcsvwinston/nucleus/actions/workflows/ci.yml/badge.svg)](https://github.com/jcsvwinston/nucleus/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/jcsvwinston/nucleus.svg)](https://pkg.go.dev/github.com/jcsvwinston/nucleus)
[![Go Report Card](https://goreportcard.com/badge/github.com/jcsvwinston/nucleus)](https://goreportcard.com/report/github.com/jcsvwinston/nucleus)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

> **Status: pre-1.0 (`v0.6.x`).** Public APIs are classified `stable`,
> `transitional`, or `experimental` (see
> [`docs/reference/API_CONTRACT_INVENTORY.md`](docs/reference/API_CONTRACT_INVENTORY.md))
> and frozen by an automated contract test. Once `v1.0` ships, application
> code on stable surfaces will not need rewrites within `v1.x`.

**Nucleus is an enterprise-grade web framework for Go.** It pairs the
ergonomics of a Django-style CLI with a stdlib-first runtime: `net/http`,
`database/sql`, and `log/slog` are the substrate; everything else is added
intentionally and stays behind framework-owned adapter boundaries so it can
be swapped without breaking application code.

The framework ships as a single Go module with a single CLI binary
(`nucleus`) and an embedded React admin panel. It is designed for systems
that have to live in production for years, not for one-shot prototypes.

---

## Why Nucleus

- **stdlib-first runtime.** `net/http`, `database/sql`, `log/slog`, `context`
  are used directly — no Gin/Chi/Echo, no GORM/Bun/Ent, no zap/zerolog,
  no per-framework debugger plumbing in stack traces. ([ADR-001](docs/adrs/ADR-001-stdlib-first.md))
- **Django-inspired CLI, Go-native semantics.** 37 lifecycle commands —
  `nucleus serve`, `migrate`, `createuser`, `inspectdb`, `dumpdata`,
  `loaddata`, `mailproviders`, `plugin doctor`, `makemessages`,
  `compilemessages`, `collectstatic`, etc. — with both Go-style names and
  Django-compatible aliases (`runserver`, `makemigrations`,
  `createsuperuser`, `dbshell`). ([ADR-002](docs/adrs/ADR-002-django-cli.md))
- **Stable-by-default extension model.** Plugin SDK `v1` uses capability
  envelopes (`mail.send`, `queue.publish`, `webhook.deliver`) discovered
  via the `nucleus-plugin-<provider>` PATH convention. Single envelope,
  single discovery prefix, no legacy bridges.
- **Embedded React admin.** Auto-generated CRUD against registered models,
  cluster-aware live-events runtime (single binary or multi-node via Redis),
  RBAC via Casbin policy file, audit log, and operational views for jobs,
  outbox state, sessions, content types, and request tracing.
- **Multi-database, multi-engine.** SQLite, PostgreSQL, MySQL are required
  lanes. MSSQL and Oracle are exploratory lanes behind build tags
  (`-tags mssql`, `-tags oracle`) with parity tests for migrations,
  fixtures, sessions, cache, and inspect commands.
- **Operational depth.** Transactional outbox with leasing dispatcher, task
  scheduler with periodic and queue-runtime helpers (Asynq + Redis), signals
  bus with optional Redis relay, OpenTelemetry tracing/metrics, structured
  logging with request correlation, deploy-readiness `health` command,
  and `doctor` checks for plugins/tasks/storage/observability.
- **Multi-tenant and multi-site.** Subdomain or header-based tenant
  resolution, per-tenant DB isolation, automatic storage prefixing,
  per-tenant rate limiting, and explicit override APIs when you need
  to step around the convention.

---

## 5-minute start

```bash
go install github.com/jcsvwinston/nucleus/cmd/nucleus@latest

nucleus new myapp --module github.com/acme/myapp
cd myapp
go mod tidy
go run .
```

`nucleus new` generates a **minimal skeleton** — a composition-root `main.go`,
`nucleus.yml`, `.gitignore`, and an empty `migrations/` directory. There is no
pre-built demo content. The mvc template also includes `rbac_policy.csv` and
mounts the admin panel; the api template uses `WithoutDefaults()` and serves
only `/healthz`.

Open after `go run .`:

| URL | Surface |
|---|---|
| `http://localhost:8080/healthz` | Liveness/readiness probe (always available) |
| `http://localhost:8080/admin` | Embedded admin panel (mvc template only) |

To build your first feature module, see the working reference application in
`examples/mvc_api` — it adds a `notes` REST resource on the fluent
`nucleus.Module` surface and is the canonical starting point.

Generated projects are **self-contained**: `go.mod` already requires the
right Nucleus version, no `replace` directive, no Nucleus source tree
needed.

### Minimal API in code

```go
package main

import "github.com/jcsvwinston/nucleus/pkg/nucleus"

func main() {
    nucleus.New().
        FromConfigFile("nucleus.yml").
        WithoutDefaults().
        Start()
}
```

Add features as modules; see `examples/mvc_api` for a complete worked example
using the `nucleus.Module` surface. The fluent package is a façade over the
same `pkg/app` runtime.

---

## What's in the box

### Runtime packages

| Package | Lifecycle | Purpose |
|---|---|---|
| [`pkg/app`](pkg/app) | `stable` | Application container, configuration, lifecycle, multi-tenant context |
| [`pkg/router`](pkg/router) | `stable` | `net/http`-backed router, middleware, request `Context`, binding/rendering helpers |
| [`pkg/model`](pkg/model) | `stable` | `BaseModel`, struct tags, validation, hook lifecycle, admin metadata |
| [`pkg/db`](pkg/db) | `stable` | `database/sql` adapter, multi-DB resolution, migration runner |
| [`pkg/auth`](pkg/auth) | `stable` | JWT manager, claims context, SCS-backed sessions (memory/SQL/Redis) |
| [`pkg/authz`](pkg/authz) | `stable` | Casbin policy engine + middleware |
| [`pkg/mail`](pkg/mail) | `stable` | Sender abstraction, built-in `noop`/`smtp`/`sendgrid`, capability plugins |
| [`pkg/plugins`](pkg/plugins) | `stable` | Plugin SDK `v1` envelopes, discovery, capability probe, runtime execution |
| [`pkg/tasks`](pkg/tasks) | `stable` | Asynq-backed task manager, scheduler, queue runtime ops, instrumentation |
| [`pkg/storage`](pkg/storage) | `stable` | S3/GCS/Azure/local providers, credential resolution, public-path mapping, signed URLs |
| [`pkg/signals`](pkg/signals) | `stable` | In-process bus + optional Redis pub/sub relay |
| [`pkg/observe`](pkg/observe) | `stable` | `slog` setup + OTel pipeline |
| [`pkg/errors`](pkg/errors) | `stable` | Domain error types and HTTP writer |
| [`pkg/validate`](pkg/validate) | `stable` | Validator integration + custom rule registry |
| [`pkg/admin`](pkg/admin) | `transitional` | Admin mount, runtime snapshot, embedded React UI |
| [`pkg/outbox`](pkg/outbox) | `transitional` | SQL transactional outbox, leasing dispatcher (Kafka/Webhook bridges = preview) |
| [`pkg/openapi`](pkg/openapi) | `experimental` | OpenAPI 3.1 document model for `internal/contracts` projects |
| [`pkg/nucleus`](pkg/nucleus) | `stable` | Fluent builder façade — the `nucleus.New()` entry point |

See [`docs/reference/API_CONTRACT_INVENTORY.md`](docs/reference/API_CONTRACT_INVENTORY.md)
for the contract per package.

### CLI command groups

```
Project lifecycle    new, startapp, generate, serve, health, doctor
Database             migrate, sqlmigrate, sqlflush, sqlsequencereset,
                     squashmigrations, optimizemigration, inspectdb,
                     ogrinspect, shell, flush
Data                 loaddata, dumpdata, seed
Cache & sessions     createcachetable, clearsessions,
                     remove_stale_contenttypes
Identity             createuser, changepassword
Mail                 mailproviders, sendtestemail
Plugins              plugin list, plugin doctor, plugin test
Static & i18n        collectstatic, findstatic, makemessages,
                     compilemessages
Diagnostics          diffsettings, routes, testserver, test
```

Aliases mirror Django where it is unambiguous: `runserver`,
`startproject`, `makemigrations`, `showmigrations`, `createsuperuser`,
`dbshell`. Every command supports the global output flags `--json`,
`--output`, `--color`, `--symbols` so it is usable both interactively and
in CI/scripts.

### Configuration

A single `nucleus.yml` per project (extension is `.yml`, not `.yaml`).
All keys are versioned in
[`docs/reference/CONFIG_KEY_REGISTRY.md`](docs/reference/CONFIG_KEY_REGISTRY.md)
and frozen by `contracts/baseline/config_key_patterns.txt`.

Credentials never live in plaintext in source. Every sensitive value
accepts the `CredentialSource` shape (`value` / `env_var` / `file` /
`secret_manager`) for K8s secrets, mounted volumes, or cloud secret
managers.

---

## Reference applications

The previous `examples/*` tree was removed in the ADR-010 Phase 1 iteration (2026-05-16) so it would not constrain the new `pkg/nucleus` fluent surface during its rewrite. New reference applications — designed against the post–Phase 1 fluent API, the v0.7.0 CSRF / `slog` redaction defaults, and the SchemaDrift introspection landed in v0.7.x — will be authored in v0.9.X as part of the ADR-010 Phase 4 / docs-sync iteration. See [`docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md`](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md).

---

## Documentation map

### Start here

- [`docs/QUICKSTART.md`](docs/QUICKSTART.md) — 5-minute walkthrough, more detail than this README
- [`docs/guides/DETAILED_TUTORIAL.md`](docs/guides/DETAILED_TUTORIAL.md) — End-to-end tutorial app
- [`docs/reference/PROJECT_LAYOUT.md`](docs/reference/PROJECT_LAYOUT.md) — Generated project conventions

### Build

- [`docs/guides/MODELING_MULTI_DATABASE.md`](docs/guides/MODELING_MULTI_DATABASE.md)
- [`docs/guides/AUTH_GUIDE.md`](docs/guides/AUTH_GUIDE.md) · [`CSRF_GUIDE`](docs/guides/CSRF_GUIDE.md) · [`VALIDATION_GUIDE`](docs/guides/VALIDATION_GUIDE.md) · [`ERROR_HANDLING`](docs/guides/ERROR_HANDLING.md)
- [`docs/guides/STORAGE_GUIDE.md`](docs/guides/STORAGE_GUIDE.md) · [`SIGNALS_GUIDE`](docs/guides/SIGNALS_GUIDE.md)
- [`docs/guides/MULTISITE_GUIDE.md`](docs/guides/MULTISITE_GUIDE.md) · [`RATE_LIMITING_GUIDE`](docs/guides/RATE_LIMITING_GUIDE.md)
- [`docs/ADMIN_UI.md`](docs/ADMIN_UI.md) · [`ADMIN_CLUSTER_LAB`](docs/ADMIN_CLUSTER_LAB.md)
- [`docs/reference/PLUGIN_SDK.md`](docs/reference/PLUGIN_SDK.md) · [`PLUGIN_EXAMPLES`](docs/reference/PLUGIN_EXAMPLES.md)

### Operate

- [`docs/guides/DEPLOYMENT_GUIDE.md`](docs/guides/DEPLOYMENT_GUIDE.md)
- [`docs/guides/OBSERVABILITY_BASELINE.md`](docs/guides/OBSERVABILITY_BASELINE.md)
- [`docs/guides/TESTING_GUIDE.md`](docs/guides/TESTING_GUIDE.md)
- [`docs/reference/CLI_BEST_PRACTICES.md`](docs/reference/CLI_BEST_PRACTICES.md) · [`CLI_CONTRACT_MATRIX`](docs/reference/CLI_CONTRACT_MATRIX.md)

### Govern

- [`SPEC.md`](SPEC.md) — Single canonical spec (runtime, deps, config, CLI, governance)
- [`docs/governance/COMPATIBILITY_SLO.md`](docs/governance/COMPATIBILITY_SLO.md) · [`RELEASE_CHECKLIST`](docs/governance/RELEASE_CHECKLIST.md) · [`CI_MATRIX`](docs/governance/CI_MATRIX.md)
- [`docs/governance/DEPRECATION_TEMPLATE.md`](docs/governance/DEPRECATION_TEMPLATE.md) · [`MIGRATION_ASSISTANT_CONVENTIONS`](docs/governance/MIGRATION_ASSISTANT_CONVENTIONS.md)
- [`docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md`](docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md) — Tracks A → G
- [`docs/adrs/`](docs/adrs/) — Architecture Decision Records (001 stdlib-first, 002 Django CLI, 003 Project Identity)

---

## Compatibility and contracts

Three text files in [`contracts/baseline/`](contracts/baseline) freeze the
public surface and are checked on every CI run:

| File | Asserts |
|---|---|
| `api_exported_symbols.txt` | No exported symbol disappears from the curated stable packages without an explicit ADR |
| `cli_primary_commands.txt` | No CLI command name disappears |
| `cli_json_status_keys.txt` | No JSON status key disappears from `--json` output |
| `config_key_patterns.txt` | No `nucleus.yml` key shape disappears |

The harness only blocks **removals**. New surface area is allowed but is
captured in the next baseline commit. See `contracts/freeze_test.go` and
`contracts/firewall_test.go`.

---

## Requirements

- Go `1.26+` (matches the `go 1.26.3` directive in `go.mod`)
- One of: SQLite, PostgreSQL, MySQL — required lanes
- Optional: Redis (sessions, tasks, signals relay, admin live cluster)
- Optional, behind build tags: MSSQL (`-tags mssql`), Oracle (`-tags oracle`)

For local dev, `docker-compose.yml` brings up Postgres, MySQL, MariaDB,
and Redis instances aligned with the test matrix.

---

## License

Apache-2.0 — see [LICENSE](LICENSE).
