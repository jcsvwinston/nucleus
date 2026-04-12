# Documentation Map

Reference date: 2026-04-12.
Status: Current.

This file is the canonical entrypoint for GoFrame documentation.

## Start Here

| Document | Purpose |
|----------|---------|
| [QUICKSTART.md](QUICKSTART.md) | Zero to running in 5 minutes |
| [DEVELOPER_MANUAL.md](DEVELOPER_MANUAL.md) | Primary developer reference (all features) |
| [DETAILED_TUTORIAL.md](DETAILED_TUTORIAL.md) | Step-by-step walkthrough building a complete app |
| [PROJECT_LAYOUT.md](PROJECT_LAYOUT.md) | Standard directory structure and folder responsibilities |
| [../SPEC.md](../SPEC.md) | Technical implementation baseline (v0.7.x) |
| [../README.md](../README.md) | Project landing page with feature overview |

## Feature Guides

### Core

| Document | Purpose |
|----------|---------|
| [AUTH_GUIDE.md](AUTH_GUIDE.md) | Authentication & Authorization (JWT, sessions, Casbin RBAC) |
| [MULTISITE_GUIDE.md](MULTISITE_GUIDE.md) | MultiSite & MultiTenant routing with DB isolation |
| [MODELING_MULTI_DATABASE.md](MODELING_MULTI_DATABASE.md) | Multi-database model definition and routing |
| [ERROR_HANDLING.md](ERROR_HANDLING.md) | Domain error types and HTTP status mapping |
| [VALIDATION_GUIDE.md](VALIDATION_GUIDE.md) | Input validation with go-playground/validator |
| [SIGNALS_GUIDE.md](SIGNALS_GUIDE.md) | In-process event bus and model lifecycle hooks |
| [RATE_LIMITING_GUIDE.md](RATE_LIMITING_GUIDE.md) | Per-route and per-role rate limiting |
| [MAIL_PROVIDERS.md](MAIL_PROVIDERS.md) | Mail drivers (noop, smtp, sendgrid) and plugin extensibility |
| [PLUGIN_SDK.md](PLUGIN_SDK.md) | Plugin SDK v1 capability-based contract |
| [PLUGIN_EXAMPLES.md](PLUGIN_EXAMPLES.md) | Official plugin SDK example implementations |

### Storage Layer

| Document | Purpose |
|----------|---------|
| [STORAGE_GUIDE.md](STORAGE_GUIDE.md) | Storage abstraction: S3, GCS, Azure, Local drivers |

### Admin Panel

| Document | Purpose |
|----------|---------|
| [ADMIN_PANEL.md](ADMIN_PANEL.md) | Admin panel configuration, features, and API reference |
| [ADMIN_CLUSTER_LAB.md](ADMIN_CLUSTER_LAB.md) | Local multi-node cluster lab (2 nodes + Redis + LB) |

### Operations

| Document | Purpose |
|----------|---------|
| [DEPLOYMENT_GUIDE.md](DEPLOYMENT_GUIDE.md) | Docker, Kubernetes, reverse proxy, TLS, scaling |
| [TESTING_GUIDE.md](TESTING_GUIDE.md) | Testing strategies (unit, integration, multi-engine) |
| [OBSERVABILITY_BASELINE.md](OBSERVABILITY_BASELINE.md) | OTel dashboards and alerts minimum baseline |
| [CLI_BEST_PRACTICES.md](CLI_BEST_PRACTICES.md) | CLI design principles and quality standards |

## Engineering References

### Contract Inventories

| Document | Purpose |
|----------|---------|
| [API_CONTRACT_INVENTORY.md](API_CONTRACT_INVENTORY.md) | Public API package stability lifecycle tags |
| [CLI_CONTRACT_MATRIX.md](CLI_CONTRACT_MATRIX.md) | CLI command lifecycle tags (stable/transitional/experimental) |
| [CONFIG_KEY_REGISTRY.md](CONFIG_KEY_REGISTRY.md) | All configuration keys with defaults and environment overrides |

### Architecture Decisions

| Document | Purpose |
|----------|---------|
| [adrs/README.md](adrs/README.md) | ADR-001: stdlib-first runtime, ADR-002: Django-inspired CLI |

## Governance

### Policies

| Document | Purpose |
|----------|---------|
| [COMPATIBILITY_SLO.md](COMPATIBILITY_SLO.md) | Quantitative compatibility thresholds for release gating |
| [VERSIONING.md](VERSIONING.md) | Semantic versioning strategy (pre-1.0 and v1.x commitment) |
| [GO_VERSION_POLICY.md](GO_VERSION_POLICY.md) | Minimum and recommended Go version |
| [DEPRECATION_TEMPLATE.md](DEPRECATION_TEMPLATE.md) | Deprecation lifecycle policy and notice template |
| [MIGRATION_ASSISTANT_CONVENTIONS.md](MIGRATION_ASSISTANT_CONVENTIONS.md) | Migration assistant naming and content contract |
| [CI_MATRIX.md](CI_MATRIX.md) | CI database lanes (required vs exploratory) |
| [RELEASE_CHECKLIST.md](RELEASE_CHECKLIST.md) | Pre-release validation and tagging checklist |

### Roadmaps

| Document | Purpose |
|----------|---------|
| [ENTERPRISE_LONG_TERM_ROADMAP.md](ENTERPRISE_LONG_TERM_ROADMAP.md) | Strategic long-term roadmap (v1.x upgrade safety, engineering principles) |
| [../CHANGELOG.md](../CHANGELOG.md) | Notable changes tracking (Keep-a-Changelog format) |

### Directories

| Directory | Purpose |
|-----------|---------|
| [deprecations/](deprecations/) | Active deprecation notices (empty if none) |
| [migration_assistants/](migration_assistants/) | Migration assistant specs (empty if none) |
| [reports/](reports/) | Current validation and stability reports |
| [templates/](templates/) | Templates for deprecation notices and migration assistants |
| [../dist/reports/](../dist/reports/) | Auto-generated release rehearsal reports |

## Precedence Rule

When documents conflict or contradict, use this precedence order:

1. `README.md` (root)
2. `SPEC.md` (technical baseline)
3. Strategy/governance docs (`ENTERPRISE_LONG_TERM_ROADMAP.md`, `COMPATIBILITY_SLO.md`, `VERSIONING.md`)
4. Feature guides (individual `.md` files in `docs/`)
5. `DEVELOPER_MANUAL.md` (monolith reference — overlaps with standalone guides)
6. Historical behavior from git history only

## Terminology

- External provider binaries: `goframe-plugin-<provider>`
- Legacy mail fallback naming: `goframe-mail-<driver>`
- Storage providers: `s3` (AWS S3, MinIO, R2), `gcs` (Google Cloud Storage), `azure` (Azure Blob), `local` (development only)
- Credential sources: `value` (literal), `env_var` (environment variable), `file` (mounted file), `secret_manager` (cloud secret via `env:` prefix)
