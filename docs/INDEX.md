# Documentation Map

Reference date: 2026-04-23.
Status: Current.

This file is the canonical entrypoint for GoFrame documentation.

## Start Here

| Document | Purpose |
|----------|---------|
| [QUICKSTART.md](QUICKSTART.md) | Zero to running in 5 minutes |
| [guides/DETAILED_TUTORIAL.md](guides/DETAILED_TUTORIAL.md) | Step-by-step walkthrough building a complete app |
| [reference/DEVELOPER_MANUAL.md](reference/DEVELOPER_MANUAL.md) | Primary developer reference (core concepts + quick reference) |
| [reference/PROJECT_LAYOUT.md](reference/PROJECT_LAYOUT.md) | Standard directory structure and folder responsibilities |
| [MODULARIZATION.md](MODULARIZATION.md) | Standalone scaffold initiative (build tags, extensions, multi-module) |
| [BREADCRUMB.md](BREADCRUMB.md) | Current work-in-progress state (update after each session) |
| [AGENT_HANDOFF.md](AGENT_HANDOFF.md) | Handoff guide for any agent to resume work |
| [../SPEC.md](../SPEC.md) | Technical implementation baseline (pre-v1) |
| [../README.md](../README.md) | Project landing page with feature overview |

## Feature Guides

### Core

| Document | Purpose |
|----------|---------|
| [guides/AUTH_GUIDE.md](guides/AUTH_GUIDE.md) | Authentication & Authorization (JWT, sessions, Casbin RBAC) |
| [guides/MULTISITE_GUIDE.md](guides/MULTISITE_GUIDE.md) | MultiSite & MultiTenant routing with DB isolation |
| [guides/MODELING_MULTI_DATABASE.md](guides/MODELING_MULTI_DATABASE.md) | Multi-database model definition and routing |
| [guides/ERROR_HANDLING.md](guides/ERROR_HANDLING.md) | Domain error types and HTTP status mapping |
| [guides/VALIDATION_GUIDE.md](guides/VALIDATION_GUIDE.md) | Input validation with go-playground/validator |
| [guides/SIGNALS_GUIDE.md](guides/SIGNALS_GUIDE.md) | In-process event bus and model lifecycle hooks |
| [guides/RATE_LIMITING_GUIDE.md](guides/RATE_LIMITING_GUIDE.md) | Per-route and per-role rate limiting |
| [guides/STORAGE_GUIDE.md](guides/STORAGE_GUIDE.md) | Storage abstraction: S3, GCS, Azure, Local drivers |

### Admin Panel

| Document | Purpose |
|----------|---------|
| [ADMIN_UI.md](ADMIN_UI.md) | Complete Admin UI documentation (React-based, pre-v1) |
| [ADMIN_CLUSTER_LAB.md](ADMIN_CLUSTER_LAB.md) | Local multi-node cluster lab (2 nodes + Redis + LB) |

### Operations

| Document | Purpose |
|----------|---------|
| [guides/DEPLOYMENT_GUIDE.md](guides/DEPLOYMENT_GUIDE.md) | Docker, Kubernetes, reverse proxy, TLS, scaling |
| [guides/TESTING_GUIDE.md](guides/TESTING_GUIDE.md) | Testing strategies (unit, integration, multi-engine) |
| [guides/OBSERVABILITY_BASELINE.md](guides/OBSERVABILITY_BASELINE.md) | OTel dashboards and alerts minimum baseline |

## Engineering References

### Reference Materials

| Document | Purpose |
|----------|---------|
| [reference/DEVELOPER_MANUAL.md](reference/DEVELOPER_MANUAL.md) | Core app container, models, routing, CLI overview |
| [reference/CONFIG_KEY_REGISTRY.md](reference/CONFIG_KEY_REGISTRY.md) | All configuration keys with defaults and environment overrides |
| [reference/API_CONTRACT_INVENTORY.md](reference/API_CONTRACT_INVENTORY.md) | Public API package stability lifecycle tags |
| [reference/CLI_CONTRACT_MATRIX.md](reference/CLI_CONTRACT_MATRIX.md) | CLI command lifecycle tags (stable/transitional/experimental) |
| [reference/CLI_BEST_PRACTICES.md](reference/CLI_BEST_PRACTICES.md) | CLI design principles and quality standards |
| [reference/PLUGIN_SDK.md](reference/PLUGIN_SDK.md) | Plugin SDK v1 capability-based contract (includes mail providers) |
| [reference/PLUGIN_EXAMPLES.md](reference/PLUGIN_EXAMPLES.md) | Official plugin SDK example implementations |
| [reference/DEPENDENCY_IMPACT_REPORT.md](reference/DEPENDENCY_IMPACT_REPORT.md) | Dependency tracking and impact analysis |
| [reference/PROJECT_LAYOUT.md](reference/PROJECT_LAYOUT.md) | Standard project directory structure |

### Architecture Decisions

| Document | Purpose |
|----------|---------|
| [adrs/README.md](adrs/README.md) | ADR-001: stdlib-first runtime, ADR-002: Django-inspired CLI |

## Governance

### Policies

| Document | Purpose |
|----------|---------|
| [governance/COMPATIBILITY_SLO.md](governance/COMPATIBILITY_SLO.md) | Quantitative compatibility thresholds for release gating |
| [governance/ENTERPRISE_LONG_TERM_ROADMAP.md](governance/ENTERPRISE_LONG_TERM_ROADMAP.md) | Strategic long-term roadmap (includes versioning & Go version policy) |
| [governance/DEPRECATION_TEMPLATE.md](governance/DEPRECATION_TEMPLATE.md) | Deprecation lifecycle policy and notice template |
| [governance/MIGRATION_ASSISTANT_CONVENTIONS.md](governance/MIGRATION_ASSISTANT_CONVENTIONS.md) | Migration assistant naming and content contract |
| [governance/CI_MATRIX.md](governance/CI_MATRIX.md) | CI database lanes (required vs exploratory) |
| [governance/RELEASE_CHECKLIST.md](governance/RELEASE_CHECKLIST.md) | Pre-release validation and tagging checklist |

### Directories

| Directory | Purpose |
|-----------|---------|
| [deprecations/](deprecations/) | Active deprecation notices (empty if none) |
| [migration_assistants/](migration_assistants/) | Migration assistant specs (empty if none) |
| [reports/](reports/) | Current validation and stability reports |
| [templates/](templates/) | Templates for deprecation notices and migration assistants |

## Precedence Rule

When documents conflict or contradict, use this precedence order:

1. `README.md` (root)
2. Contract/governance docs in `governance/`:
   - `governance/COMPATIBILITY_SLO.md`
   - `governance/ENTERPRISE_LONG_TERM_ROADMAP.md`
3. `SPEC.md` (technical baseline — defers to governance on conflicts)
4. Feature guides (individual `.md` files in `guides/`)
5. Reference materials (`reference/`)
6. Historical behavior from git history only

## Terminology

- External provider binaries: `goframe-plugin-<provider>`
- Legacy mail fallback naming: `goframe-mail-<driver>`
- Storage providers: `s3` (AWS S3, MinIO, R2), `gcs` (Google Cloud Storage), `azure` (Azure Blob), `local` (development only)
- Credential sources (pkg/storage layer): `value`, `env_var`, `file`, `secret_manager`; app YAML config currently exposes direct strings only
