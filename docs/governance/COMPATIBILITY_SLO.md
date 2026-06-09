# Compatibility SLO Policy

Reference date: 2026-06-09.
Status: Current.

This policy defines the measurable compatibility service levels that Nucleus must satisfy before and after `v1.0`.

## Objective

Ensure framework upgrades do not force application rewrites, and compatibility quality is enforced by release gates rather than best effort.

## Scope

This SLO applies to:

- stable public API in `pkg/*`
- stable CLI contracts
- stable config keys and defaults
- plugin SDK `v1` contract
- compatibility fixture applications and SQL matrix critical paths

## Indicators

1. Fixture App Pass Rate
- Definition: successful runs / total runs for compatibility fixture applications in release validation.

2. Stable Contract Regression Count
- Definition: number of detected regressions against stable API/CLI/config/plugin contracts.

3. Exploratory DB Stability Rate
- Definition: successful exploratory-lane runs / total stability drill runs.
  `mssql` and `oracle` were promoted from exploratory to required on 2026-05-12
  (see `CI_MATRIX.md`); they now count toward the required lanes. No engines are
  currently exploratory, so this indicator applies only to future candidates.

4. Dependency Compatibility Incidents
- Definition: number of release blockers caused by dependency upgrades leaking into user-facing behavior.

## SLO Targets By Stage

## Pre-`v1.0` (hardening stage)

- Fixture app pass rate: `>= 95%`
- Stable contract regressions: `0` unresolved at release
- Exploratory DB stability: `>= 80%` per engine on 10-run drill (mssql/oracle cleared this bar and were promoted to required on 2026-05-12; no engines are currently exploratory)
- Dependency compatibility incidents: `0` unresolved at release

## `v1.x` (steady state)

- Fixture app pass rate: `>= 99%`
- Stable contract regressions: `0` unresolved at release
- Exploratory/required DB stability:
  - required lanes: `>= 99%`
  - exploratory lanes: `>= 90%` before promotion request
- Dependency compatibility incidents: `0` unresolved at release

## Error Budget and Actions

If any SLO falls below target:

1. stop promotion of new capabilities to required gates
2. block release tag until compatibility remediation lands
3. publish a short RCA in changelog/release notes for transparency
4. add/adjust regression tests to prevent recurrence

## Release Artifacts (Mandatory)

Each release candidate must include:

- compatibility report (fixture app + stable contract summary)
- exploratory DB stability report (if applicable)
- dependency impact report for critical dependencies
- deprecation + migration-assistant artifacts (when active deprecations exist)
- explicit compatibility statement:
  - `no breaking changes`, or
  - `major-only breaking change with migration plan`

## Measurement Process

1. Run repeated exploratory stability drill:

```bash
gh auth login
bash scripts/ci/run_exploratory_stability.sh --runs 10 --output docs/reports/exploratory_stability.md
```

2. Run compatibility harness for fixture applications.
3. Run stable contract freeze validation (CLI primary commands, CLI JSON status envelope keys, config keys, and stable API exported symbols no-removal baselines).
4. Aggregate results into release artifacts.
5. Validate against SLO thresholds before tagging.

Recommended automation commands:

```bash
bash scripts/release/generate_compatibility_report.sh --output dist/reports/compatibility_report.md --enforce-threshold
bash scripts/release/generate_dependency_impact_report.sh --output dist/reports/dependency_impact_report.md
bash scripts/ci/check_contract_freeze.sh
```

## Ownership

- Maintainers: define/update SLO targets and approve exceptions.
- Release owner: attach required reports and verify gates.
- Contributors: treat compatibility regressions as release blockers.

## Exception Rule

Temporary exceptions require:

- explicit maintainer approval
- documented risk + mitigation + expiry date
- follow-up issue with milestone assignment

No silent exceptions.
