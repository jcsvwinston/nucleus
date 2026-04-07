# Deprecation Template and Policy

Reference date: 2026-04-07.
Status: Current.

This document defines the required deprecation workflow and the canonical deprecation notice template.

## Objective

Ensure deprecations are explicit, test-backed, and migration-ready before any removal decision.

## Lifecycle

1. `proposed`
- deprecation is identified but not yet announced.

2. `active`
- deprecation is announced in docs/changelog and migration guidance is available.

3. `completed`
- migration path is validated and removal window is reached.

4. `removed`
- deprecated surface is removed in a version allowed by policy.

## Removal Rules

- Pre-`v1.0`: removals are exception-only and must include migration notes and explicit maintainer approval.
- `v1.x`: stable surfaces are not removed in `v1.x`; removals are major-only (`v2+`).

## Required Artifacts For Any Active Deprecation

1. a deprecation notice file under `docs/deprecations/` based on `docs/templates/deprecation_notice.md`
2. a migration assistant specification based on `docs/templates/migration_assistant.md`
3. changelog entry in `CHANGELOG.md` (`Unreleased`)
4. compatibility review note in release validation

## Deprecation Notice Template

Copy from `docs/templates/deprecation_notice.md`:

```markdown
# Deprecation Notice: <title>

- ID: `DEP-YYYY-NNN`
- Status: `proposed|active|completed|removed`
- Announced in: `<version>`
- Earliest removal: `<version or major-only note>`
- Scope: `api|cli|config|plugin|mixed`
- Affected lifecycle tag: `stable|transitional|experimental`
- Owner: `<team or maintainer>`

## Summary

<what is being deprecated and why>

## Affected Surfaces

- `<surface 1>`
- `<surface 2>`

## Migration Path

- Replacement: `<new API/command/key>`
- Behavior differences: `<short list>`
- Required app changes: `<code/config/ops changes>`

## Migration Assistant

- Assistant spec: `<path to migration assistant document>`
- Detection rule: `<how affected usage is detected>`
- Suggested rewrite: `<automatic/manual mapping>`

## Validation

- Compatibility tests updated: `yes|no`
- Release note updated: `yes|no`
- Rollback plan documented: `yes|no`

## Timeline

- Announcement date: `<YYYY-MM-DD>`
- Review checkpoint: `<YYYY-MM-DD>`
- Removal decision date: `<YYYY-MM-DD>`
```

## Review Rule

Any pull request that introduces or updates an active deprecation must include both:

- a deprecation notice document (`docs/deprecations/*.md`)
- a migration assistant spec (`docs/migration_assistants/*.md`)
