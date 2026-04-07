# Enterprise Long-Term Roadmap

Reference date: 2026-04-07.
Status: Current strategic source of truth.

This roadmap defines how GoFrame will become an enterprise-grade, developer-friendly framework designed for very long production lifecycles.

## Strategic Positioning

GoFrame has its own product identity and technical direction.

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

GoFrame reaches enterprise category when all are true:

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

## Track B: Compatibility Harness (`v0.8.x` -> `v0.9.x`)

Deliverables:

- fixture applications (minimal API, admin-heavy, plugin-heavy)
- CI harness validating compile/run behavior across versions
- golden tests for stable CLI/config/plugin contracts

Exit criteria:

- no unresolved high-severity compatibility regressions
- compatibility report artifact for every release candidate

## Track C: Critical Dependency Firewall (`v0.8.x` -> `v1.0`)

Deliverables:

- adapter boundaries complete for router, DB, plugin runtime, observability
- tests preventing third-party type leaks in stable APIs
- dependency impact report template as release artifact

Exit criteria:

- at least one successful dependency swap drill in a critical subsystem
- zero unresolved dependency-caused compatibility incidents at release

## Track D: Enterprise Data Coverage (`v0.9.x` -> `v1.1`)

Deliverables:

- required SQL lanes: SQLite, PostgreSQL, MySQL
- enterprise lanes (MSSQL, Oracle) promoted by measurable stability
- critical command coverage for migrations, fixtures, inspect, sessions/cache operations

Promotion rule (exploratory -> required):

- reproducible local setup
- sustained stability drills above threshold
- no unresolved critical regressions for target engine

## Track E: Security and Compliance Baseline (`v1.0` -> `v1.2`)

Deliverables:

- hardened default policies for session/cookie/headers/rate-limits
- deploy checks for high-risk misconfiguration
- audit-friendly release and changelog discipline

Exit criteria:

- default hardening profile documented and test-backed
- security-sensitive config changes always compatibility-reviewed

## Track F: Developer Productivity and Tooling (`v1.0` -> `v1.3`)

Deliverables:

- CLI UX improvements (history, diagnostics, explain mode where appropriate)
- migration and deprecation assistants
- stronger plugin developer tooling and contract test kits

Exit criteria:

- reduced onboarding time and fewer manual migration steps
- stable plugin authoring experience across `v1.x`

## SLO and Governance

Compatibility policy is enforced through:

- `docs/COMPATIBILITY_SLO.md`
- `docs/RELEASE_CHECKLIST.md`
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

This file is the canonical roadmap for enterprise and long-term strategy.
