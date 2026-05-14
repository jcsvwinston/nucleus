# Migration Assistant: <old surface> → <new surface>

<!--
Canonical scaffold for a migration assistant spec. Copy this file to
docs/migration_assistants/MA-YYYY-NNN-<slug>.md and fill every field.

Conventions: docs/governance/MIGRATION_ASSISTANT_CONVENTIONS.md
Every active deprecation in docs/deprecations/ must reference exactly
one migration assistant spec, and the spec must point back at it.
-->

- ID: `MA-YYYY-NNN`
- Pairs with: `docs/deprecations/DEP-YYYY-NNN-<slug>.md`
- Severity: `low|medium|high`
  <!-- low: mechanical rename, low behavior risk.
       medium: behavior differences requiring validation.
       high: data/runtime semantics change requiring staged rollout. -->
- Status: `current|superseded`

## Scope

<which API / CLI / config / plugin surfaces are affected. Be precise —
name the symbols, keys, and commands a consumer would have in their
tree.>

## Detection

<explicit patterns to detect impacted usage. Include runnable command
examples — `rg`, `nucleus diffsettings --json`, `nucleus plugin
doctor`, etc. State the exact condition that makes a consumer
"impacted".>

```bash
# detection commands
```

## Rewrite Plan

<old → new mapping. A table is the canonical form.>

| Surface (before) | Surface (after) | Note |
|------------------|-----------------|------|
| `<old>`          | `<new>`         | `<behaviour delta>` |

Automatic rewrite candidates:

- <changes that can be applied mechanically / scripted>

Manual steps:

- <changes that need human judgement or out-of-tree action>

## Verification

<exact commands and tests a consumer runs after migrating. These must
be reproducible in CI / non-interactive contexts where possible and
should exit non-zero on regression.>

```bash
# verification commands
```

## Rollback

<safe rollback path if the migration introduces risk. Migration
guidance must be additive-first; destructive steps need explicit
guardrails (`--dry-run`, confirmation flags, backup steps).>

## Compatibility Notes

<optional — additive-first guarantees, CI reproducibility, pre-v1.0
exceptions invoked.>
