# Enterprise Long-Term Roadmap

Reference date: 2026-06-09.
Status: Current strategic source of truth.

This roadmap defines how Nucleus will become an enterprise-grade, developer-friendly framework designed for very long production lifecycles.

## Strategic Positioning

Nucleus has its own product identity and technical direction.

We are building:

- a Go-native platform with stable contracts and operational depth
- a framework where application code remains valid for years across upgrades
- a system that prioritizes reliability, observability, security, and maintainability by default

## Product Promise

From `v1.0` onward, teams should be able to upgrade within `v1.x` without rewriting application code.

Allowed in `v1.x`:

- additive APIs
- optional capabilities
- performance and security improvements
- better developer tooling

Not allowed in `v1.x`:

- breaking stable public APIs
- changing stable config semantics in a breaking way
- forcing app-level rewrites for framework upgrades

## Success Criteria (Enterprise Grade)

Nucleus reaches enterprise category when all are true:

1. Upgrade safety:
- stable contracts are preserved and continuously verified.

2. Operational excellence:
- production diagnostics, failure isolation, and recovery workflows are first-class.

3. Security posture:
- hardening defaults, policy-driven controls, and auditable change practices.

4. Multi-environment viability:
- deterministic behavior across dev/staging/prod and across supported SQL engines.

5. Developer experience:
- fast onboarding, predictable CLI, low-friction debugging, and maintainable extension model.

## Core Pillars

1. Compatibility and Contract Governance
- versioned contracts for API/CLI/config/plugin boundaries
- explicit lifecycle tags: `stable`, `transitional`, `experimental`
- deprecation policy with migration tooling before removals

2. Framework-Owned Critical Layers
- no third-party concrete types in stable public APIs
- adapters around router/DB/telemetry/plugin boundaries
- dependency swap capability without app code changes

3. Data Platform (Multi-Database, Multi-Engine)
- SQL-first deterministic migrations
- required lanes for primary engines, exploratory-to-required promotion path for enterprise engines
- parity gates for critical operational commands

4. Plugin and Extension Platform
- Plugin SDK `v1` stable through `v1.x`
- capability negotiation over hard rewrites
- strict timeout, payload, and security controls

5. Operations and Reliability
- health/deploy checks as release gates
- observability baseline for trace/metric/log correlation
- standardized runbooks and diagnostic commands

6. Developer Experience
- predictable scaffolding and project structure
- clear defaults, discoverable CLI, and non-interactive CI modes
- compatibility aliases maintained as convenience, not product identity

## Delivery Tracks

## Track A: Contract Freeze and Inventory (Now -> `v0.8.x`)

Deliverables:

- public API inventory with lifecycle status (`stable/transitional/experimental`)
- CLI command contract matrix
- config key registry with stability tags
- documented extension points and non-contract surfaces

Exit criteria:

- all public entrypoints classified
- release checklist references compatibility gates

## Track B: Compatibility Harness (`v0.8.x` -> `v0.9.x`) ✅ **COMPLETED**

Deliverables:

- fixture applications (minimal API, admin-heavy, plugin-heavy) ✅
- CI harness validating compile/run behavior across versions ✅
- golden tests for stable CLI/config/plugin contracts ✅

Exit criteria:

- no unresolved high-severity compatibility regressions ✅
- compatibility report artifact for every release candidate ✅

**Implementation:**
- Fixture applications: previously `examples/mvc_api` (minimal API, admin-heavy) and `examples/plugins` (plugin-heavy). The fixture tree was removed in the ADR-010 Phase 1 iteration on 2026-05-16; the harness runs a `core-build` placeholder profile in the interim, and the fixture profiles return in v0.9.X.
- CI harness: `scripts/ci/run_compatibility_harness.sh` with profile-based testing
- Golden tests: `contracts/freeze_test.go` with baseline files in `contracts/baseline/`
- Compatibility report: `scripts/release/generate_compatibility_report.sh`

## Track C: Critical Dependency Firewall (`v0.8.x` -> `v1.0`) ✅ **COMPLETED**

Deliverables:

- adapter boundaries complete for router, DB, plugin runtime, observability ✅
- tests preventing third-party type leaks in stable APIs ✅
- dependency impact report template as release artifact ✅

Exit criteria:

- at least one successful dependency swap drill in a critical subsystem ✅
- zero unresolved dependency-caused compatibility incidents at release ✅

**Implementation:**
- Adapter boundaries: All critical dependencies wrapped behind framework interfaces (see `docs/reference/DEPENDENCY_IMPACT_REPORT.md`)
- Type leak prevention: `contracts/firewall_test.go` with automated AST-based detection
- Dependency impact report: `scripts/release/generate_dependency_impact_report.sh` with critical dependency tracking
- Swap drills: SQL driver swap documented and validated (SQLite ↔ PostgreSQL ↔ MySQL)

## Track D: Enterprise Data Coverage (`v0.9.x` -> `v1.1`) ✅ **DONE**

Deliverables:

- required SQL lanes: SQLite, PostgreSQL, MySQL ✅
- enterprise lanes (MSSQL, Oracle) promoted to required on 2026-05-12 ✅
- critical command coverage for migrations, fixtures, inspect, sessions/cache operations ✅

Promotion rule (exploratory -> required):

- reproducible local setup ✅ (Docker images: MSSQL 2022, Oracle Free 23-slim)
- sustained stability drills above threshold ✅ (10-run drills passed; promotion landed 2026-05-12)
- no unresolved critical regressions for target engine ✅ (all critical commands tested)

**Progress (2026-05-08):**
- Critical command coverage completed for MSSQL/Oracle:
  - ✅ migrate (up, down, status)
  - ✅ fixtures (loaddata, dumpdata)
  - ✅ inspectdb
  - ✅ sessions/cache (clearsessions)
  - ✅ health, createcachetable, sqlflush, flush, sqlsequencereset, shell
- Stability drill script operational: `scripts/ci/run_exploratory_stability.sh`
- Stability report created: `docs/reports/mssql_oracle_stability_report.md`
- Completed (2026-05-12): 10-run stability drills passed; MSSQL and Oracle promoted to required CI lanes (see `docs/governance/CI_MATRIX.md`)

## Track E: Security and Compliance Baseline (`v1.0` -> `v1.2`)

Deliverables:

- hardened default policies for session/cookie/headers/rate-limits
- deploy checks for high-risk misconfiguration
- audit-friendly release and changelog discipline

Exit criteria:

- default hardening profile documented and test-backed
- security-sensitive config changes always compatibility-reviewed

## Track F: Cloud Services Integration (`v1.0` -> `v1.2`)

Deliverables:

- AWS Secrets Manager integration for credential resolution
- AWS KMS integration for encryption key management
- AWS Lambda integration for serverless function deployment
- Google Cloud Pub/Sub integration for messaging
- Azure Service Bus integration for messaging

Promotion rule (exploratory -> required):

- reproducible local setup with mocks/stubs
- sustained stability drills above threshold
- adapter boundaries prevent third-party type leaks

Exit criteria:

- at least one successful cloud service swap drill
- zero unresolved cloud-service-caused compatibility incidents at release

## Track G: Developer Productivity and Tooling (`v1.0` -> `v1.3`)

Deliverables:

- CLI UX improvements (history, diagnostics, explain mode where appropriate)
- CLI assistant/doctor commands (tasks, outbox, storage, observability, tenancy, rbac, audit)
- Interactive CLI wizard mode (inspectdb, new, startapp with guided multi-step prompts)
- migration and deprecation assistants
- stronger plugin developer tooling and contract test kits

Exit criteria:

- reduced onboarding time and fewer manual migration steps
- stable plugin authoring experience across `v1.x`

## SLO and Governance

Compatibility policy is enforced through:

- `docs/governance/COMPATIBILITY_SLO.md`
- `docs/governance/RELEASE_CHECKLIST.md`
- CI required gates + reproducible stability drills

Baseline SLO interpretation:

- pre-`v1.0`: hardening stage thresholds
- `v1.x`: strict no-break stability bar for stable contracts

## What We Will Not Do

- optimize for framework mimicry or parity branding
- introduce breaking behavior in stable surfaces to ship short-term features faster
- expose internal implementation details as implicit contracts

## 12-Month Milestones

1. Milestone M1 (`v0.8.x`)
- contract inventory complete
- roadmap governance wired into release process

2. Milestone M2 (`v0.9.x`)
- compatibility harness operational
- dependency firewall tests active

3. Milestone M3 (`v1.0`)
- stable contract freeze published
- `v1.x` compatibility commitment active

4. Milestone M4 (`v1.1`)
- enterprise engine maturity advanced with sustained stability evidence

5. Milestone M5 (`v1.2+`)
- stronger security/compliance and developer productivity toolchain

## Immediate Backlog (Next 6 Weeks)

1. Publish API/CLI/config contract inventory docs.
2. Create fixture-app compatibility harness in CI.
3. Automate dependency impact report generation for RCs.
4. Expand SQL critical-command integration coverage for enterprise engines.
5. Add deprecation template and migration assistant conventions.
6. Add Oracle custom sequence-mapping support for `sqlsequencereset` (table->sequence strategy beyond common conventions).
7. Design and stage Admin Live Runtime Inspector vNext under `/admin`:
  - in-memory live traffic/session/runtime introspection,
  - reflection-driven auto-admin expansion,
  - secure-by-default masking/redaction model,
  - non-blocking event pipeline and WebSocket streaming.
  - specification source: this roadmap item; a dedicated spec document has not been published yet.

## Progress Snapshot

- 2026-04-07: Track A contract inventory deliverables published:
  - `docs/reference/API_CONTRACT_INVENTORY.md`
  - `docs/reference/CLI_CONTRACT_MATRIX.md`
  - `docs/reference/CONFIG_KEY_REGISTRY.md`
- 2026-04-07: Immediate backlog item 5 delivered:
  - `docs/governance/DEPRECATION_TEMPLATE.md`
  - `docs/governance/MIGRATION_ASSISTANT_CONVENTIONS.md`
  - reusable templates under `docs/templates/`
- 2026-04-07: Immediate backlog item 4 advanced:
  - expanded exploratory CLI critical-command coverage for `mssql`/`oracle`
  - added assertions for `sqlflush`, `flush --dry-run`, `sqlsequencereset`, and DDL idempotency (`createcachetable`)
  - Oracle `sqlsequencereset` now emits concrete reset SQL for common sequence naming conventions
- 2026-04-08: Immediate backlog item 7 added:
  - Admin Live Runtime Inspector vNext backlog/design item recorded in this roadmap
  - scoped for additive rollout and compatibility-safe execution
- 2026-04-11: P0/P1/P2 admin features delivered (tenant-aware CRUD, RBAC/Casbin, audit logging,
  migrations UI, health dashboard, jobs, sites, deployment detection, cache, storage browser, email stats).
  Admin panel updated in `SPEC.md`. Current documentation lives in `docs/ADMIN_UI.md`.
- 2026-04-11: P3 (Data Import/Export Wizard) design documented below. **Implementation blocked**
  until storage abstraction is resolved (see storage dependency analysis).
- 2026-05-07: Track B (Compatibility Harness) completed:
  - Fixture applications operational at this milestone: `examples/mvc_api` (minimal API, admin-heavy), `examples/plugins` (plugin-heavy). *(Subsequently removed in the ADR-010 Phase 1 iteration on 2026-05-16; the fixture profiles return in v0.9.X with the new reference applications.)*
  - CI harness validated: `scripts/ci/run_compatibility_harness.sh` with profile-based cross-version testing
  - Golden tests enforced: `contracts/freeze_test.go` with baseline files in `contracts/baseline/`
  - Compatibility report generation: `scripts/release/generate_compatibility_report.sh` integrated into release process
- 2026-05-07: Track C (Critical Dependency Firewall) completed:
  - Adapter boundaries validated: All critical dependencies wrapped behind framework interfaces
  - Type leak prevention implemented: `contracts/firewall_test.go` with automated AST-based detection
  - Dependency impact report operational: `scripts/release/generate_dependency_impact_report.sh` with critical dependency tracking
  - Swap drills validated: SQL driver swap (SQLite ↔ PostgreSQL ↔ MySQL) documented in `docs/reference/DEPENDENCY_IMPACT_REPORT.md`
- 2026-05-08: Track D (Enterprise Data Coverage) in progress:
  - Critical command coverage completed for MSSQL/Oracle (migrate, fixtures, inspect, sessions/cache)
  - Stability drill script operational: `scripts/ci/run_exploratory_stability.sh`
  - Stability report created: `docs/reports/mssql_oracle_stability_report.md`
  - Next step: Execute stability drills to validate promotion thresholds (MSSQL >= 80%, Oracle >= 80%)
- 2026-05-08: Track F (Cloud Services Integration) added to roadmap:
  - AWS Secrets Manager, KMS, Lambda integration planned
  - Google Cloud Pub/Sub integration planned
  - Azure Service Bus integration planned
  - Adapter boundary pattern to prevent third-party type leaks
- 2026-05-08: Track G (Developer Productivity and Tooling) expanded:
  - CLI assistant/doctor commands added (tasks, outbox, storage, observability, tenancy, rbac, audit)
  - Interactive CLI wizard mode added (inspectdb, new, startapp with guided multi-step prompts)

## P3 Backlog: Data Import/Export Wizard (COMPLETED — 2026-04-11)

**Status:** Fully implemented with multi-node support via S3 shared storage.

### What's implemented

**Backend (Go):**
- Export formats: CSV, JSON, SQL dump (CREATE TABLE + INSERT)
- Import formats: CSV, JSON with full validation pipeline
- On-conflict strategies: skip, update, error
- Tenant-aware: auto-filter exports, auto-inject tenant_id on imports
- Storage via `pkg/storage`: files stream to S3, never held in memory
- Multi-node safe: any node can upload, validate, export, import (S3 is shared)
- Signed URLs for secure downloads (24h TTL)
- Temporary file cleanup via `_tmp/` prefix + TTL cleaner

**Frontend (Data Studio integration):**
- "Export" button → modal with format selector (CSV/JSON/SQL)
- "Import" button → 2-step wizard: upload+validate → review+execute
- Validation preview shows errors before importing
- On-conflict selector: skip/update/error
- Toolbar shows: Delete selected | Export selected | Export all | Import | CSV

**API endpoints:**
- `POST /api/export` — Create export (returns storage key + signed URL)
- `GET /api/export/list` — List completed exports
- `GET /api/export/status?id=` — Check export status
- `GET /api/export/download?key=` — Download exported file
- `POST /api/import/upload` — Upload file for import (max 50MB)
- `POST /api/import/validate?key=` — Validate without writing
- `POST /api/import/execute?key=` — Execute validated import

### Multi-node architecture

```
User → [LB] → Node A (upload file → S3)
                      ↓
                Any Node (validate → S3 read)
                      ↓
                Any Node (import → DB write)
                      ↓
                Any Node (export → DB read → S3 write)
                      ↓
                User downloads via SignedURL (direct S3 → browser)
```

**Zero node affinity. Zero coordination. Any node can handle any step.**

### Storage requirements that were NOT YET RESOLVED (now resolved)

1. ~~Large export files (100MB–5GB) need persistent storage~~ → **RESOLVED: S3 streaming**
2. ~~Import uploads need temporary storage with cleanup/TTL~~ → **RESOLVED: `_tmp/` prefix + cleaner**
3. ~~Cluster environments: shared storage across nodes~~ → **RESOLVED: S3 is inherently shared**
4. ~~K8s pods are ephemeral~~ → **RESOLVED: S3 is external to pods**
5. ~~Multi-tenant isolation at storage level~~ → **RESOLVED: automatic tenant prefixing**
6. ~~Streaming/chunked processing for files >50MB~~ → **RESOLVED: io.Reader streaming**
7. ~~Lifecycle management~~ → **RESOLVED: TTL cleanup + S3 lifecycle policies**

### Storage backend options — Decision

**Selected:** S3-compatible first (implemented). Local FS for development only.

| Option | Status | Notes |
|--------|--------|-------|
| S3/MinIO | ✅ Implemented | AWS S3, MinIO, R2, DigitalOcean Spaces via MinIO SDK |
| Local FS | ✅ Implemented | Development only, not for production |
| GCS native | ⏳ Pending | Config defined, S3 interoperability works as workaround |
| Azure Blob native | ⏳ Pending | Config defined, S3 interoperability works as workaround |
| NFS mount | ❌ Not needed | S3 solves the shared storage requirement better |
| DB LOB | ❌ Rejected | Terrible for large files, bloats backups |

**Decision required before P3 proceeds.** → **DECIDED: S3-compatible first. P3 unblocked.**

This file is the canonical roadmap for enterprise and long-term strategy.

---

## Versioning & Compatibility

### Versioning Strategy

Nucleus follows Semantic Versioning while in pre-1.0 mode.

**Current Policy:**
- Format: `v0.x.y`
- `x` (minor): may include significant feature additions and limited breaking changes
- `y` (patch): bug fixes, hardening, and non-breaking improvements

**Pre-1.0 note:**
While breaking changes are still technically possible before `v1.0`, they should be treated as exceptions and require explicit migration notes.

**v1.x Compatibility Commitment (Target):**
From `v1.0` onward:
- No breaking changes in `v1.x` for stable public contracts
- Deprecations must provide migration path and tooling before any major-version removal

**Release Types:**
1. **Release candidates**: `v0.x.y-rcN` - Used to validate release packaging and workflows
2. **Stable pre-1.0**: `v0.x.y` - Promoted after CI, rehearsal, and artifact checks pass

**Source of Truth:**
- Git tags are the version source of truth
- Binary version output is injected at build time

**Required Checks Before Tagging:**
```bash
go test ./...
bash scripts/release/rehearse_rc.sh
```

**Changelog Discipline:**
Every user-facing change should be reflected in `CHANGELOG.md` under `Unreleased` before release.

### Go Version Compatibility

**Goals:**
- Keep the framework usable for teams on stable enterprise environments
- Allow contributors to use modern Go toolchains for development and release automation
- Avoid accidental breakages caused by implicit toolchain upgrades

**Supported Versions:**
- Minimum supported Go: `1.26` (matches the `go 1.26.4` directive in `go.mod`)
- Recommended for development/release: latest `1.26.x`

**Rules:**
1. **Public compatibility target**: New features must compile and run on Go `1.26+` unless explicitly documented otherwise
2. **Development baseline**: CI and release workflows may run on newer Go versions
3. **Upgrading minimum version**: Any version bump must be explicit and documented in `go.mod`, `CHANGELOG.md`, `README.md`, and this policy
4. **Third-party dependencies**: Dependency upgrades should be evaluated for Go version constraints before merge

**Contributor Guidance:**
```bash
# Before opening a PR
go test ./...

# For release-level confidence
bash scripts/release/rehearse_rc.sh
```
