# Migration Assistant Conventions

Reference date: 2026-06-09.
Status: Current.

This document defines conventions for migration assistant artifacts used during contract changes and deprecations.

## Objective

Provide deterministic, low-friction migration guidance for users when framework behavior evolves.

## Artifact Location and Naming

- assistant specs live in `docs/migration_assistants/`
- filename format: `MA-YYYY-NNN-<slug>.md`

Deprecation links:

- each active deprecation (`docs/deprecations/*.md`) must reference one migration assistant spec.

## Assistant Content Contract

Every migration assistant spec must include:

1. Scope
- which API/CLI/config/plugin surfaces are affected.

2. Detection
- explicit patterns to detect impacted usage.
- include command examples (`rg`, `nucleus diffsettings --json`, etc.) when applicable.

3. Rewrite Plan
- old -> new mapping table.
- automatic rewrite candidates vs manual steps.

4. Verification
- exact commands/tests users should run after migration.

5. Rollback
- safe rollback path if migration introduces risk.

## Assistant Severity

- `low`: mostly mechanical rename, low behavior risk.
- `medium`: behavior differences requiring validation.
- `high`: data/runtime semantics change requiring staged rollout.

## Compatibility Expectations

- assistant guidance must be additive-first and non-destructive by default.
- destructive actions require explicit guardrails (`--dry-run`, confirmation flags, backup steps).
- migration steps must be reproducible in CI/non-interactive contexts where possible.

## Template

Use `docs/templates/migration_assistant.md` as canonical scaffold.
