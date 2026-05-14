# Deprecation Notice: <title>

<!--
Canonical scaffold for a deprecation notice. Copy this file to
docs/deprecations/DEP-YYYY-NNN-<slug>.md and fill every field.

Policy and lifecycle: docs/governance/DEPRECATION_TEMPLATE.md
Required artifacts for any active deprecation:
  1. this notice under docs/deprecations/
  2. a paired migration assistant under docs/migration_assistants/
  3. a CHANGELOG.md entry under [Unreleased]
  4. a compatibility review note in release validation
-->

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

## Notes

<optional — exceptions invoked, prior-art precedent, context the
reviewer needs>
