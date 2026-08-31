# Nucleus Documentation

Welcome to Nucleus documentation. This is your starting point for learning, building, and operating Nucleus applications.

> **Prefer the public site for learning:**
> [jcsvwinston.github.io/quantum/nucleus](https://jcsvwinston.github.io/quantum/nucleus/)
> (source: `website/docs/`) is the living user narrative. The documents
> under `docs/` are the in-repo layer — contracts, governance, ADRs, and
> the internal guides the site links into; where a guide duplicates the
> site, the site wins.

## Quick Links

| Document | Purpose |
|----------|---------|
| [QUICKSTART.md](QUICKSTART.md) | Get running in 5 minutes |
| [guides/DETAILED_TUTORIAL.md](guides/DETAILED_TUTORIAL.md) | Step-by-step complete app tutorial |
| [reference/PROJECT_LAYOUT.md](reference/PROJECT_LAYOUT.md) | Standard directory structure |

## Core Concepts

### For Developers

- **Models**: Define domain entities with `pkg/model`
- **Migrations**: SQL-first schema evolution via CLI
- **Controllers**: HTTP handlers for API and web routes
- **Services**: Business logic layer
- **Admin Panel**: Provided by the separate [orbit](https://github.com/jcsvwinston/orbit) module — auto-generated CRUD over registered models

### For Operators

- **CLI**: All lifecycle tasks (`nucleus serve`, `migrate`, `seed`, etc.)
- **Config**: `nucleus.yml` for all settings
- **Observability**: Built-in OTel, logging, health checks

## Feature Guides

| Topic | Guide |
|-------|-------|
| Authentication | [guides/AUTH_GUIDE.md](guides/AUTH_GUIDE.md) |
| Multi-tenancy | [guides/MULTISITE_GUIDE.md](guides/MULTISITE_GUIDE.md) |
| Background Jobs | [reference/DEVELOPER_MANUAL.md](reference/DEVELOPER_MANUAL.md) (sección de tareas) |
| Storage (S3/GCS) | [guides/STORAGE_GUIDE.md](guides/STORAGE_GUIDE.md) |
| Deployment | [guides/DEPLOYMENT_GUIDE.md](guides/DEPLOYMENT_GUIDE.md) |
| Testing | [guides/TESTING_GUIDE.md](guides/TESTING_GUIDE.md) |

## Reference

- [DEVELOPER_MANUAL.md](reference/DEVELOPER_MANUAL.md) - Core concepts reference
- [CONFIG_KEY_REGISTRY.md](reference/CONFIG_KEY_REGISTRY.md) - All config options
- [CLI_CONTRACT_MATRIX.md](reference/CLI_CONTRACT_MATRIX.md) - CLI commands

## Architecture

- [adrs/README.md](adrs/README.md) - Architecture Decision Records
- [governance/COMPATIBILITY_SLO.md](governance/COMPATIBILITY_SLO.md) - Stability guarantees

## Project Structure

`nucleus new` generates a **minimal skeleton**: a project-root `main.go`
(run with `go run .` — there is no `cmd/server/`), `nucleus.yml`,
`.gitignore`, an empty `migrations/` directory, and — on the mvc
template — `rbac_policy.csv`. Features are added as modules under
`internal/<feature>/`.

The authoritative tree, both templates, and the layered alternative live
in [reference/PROJECT_LAYOUT.md](reference/PROJECT_LAYOUT.md) — kept in
one place so this page cannot drift from it.

## Support

- GitHub Issues: https://github.com/jcsvwinston/nucleus/issues
- Go Package: https://pkg.go.dev/github.com/jcsvwinston/nucleus
