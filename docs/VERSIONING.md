# Versioning Strategy

Reference date: 2026-04-05.
Status: Current.

GoFrame follows Semantic Versioning while in pre-1.0 mode.

## Current Policy

Version format:

- `v0.x.y`

Interpretation in pre-1.0:

- `x` (minor): may include significant feature additions and limited breaking changes
- `y` (patch): bug fixes, hardening, and non-breaking improvements

## Release Types

1. Release candidates
- Format: `v0.x.y-rcN`
- Used to validate release packaging and workflows before stable promotion

2. Stable pre-1.0
- Format: `v0.x.y`
- Promoted after CI, rehearsal, and artifact checks pass

## Source of Truth

- Git tags are the version source of truth.
- Binary version output is injected at build time.

## Required Checks Before Tagging

```bash
go test ./...
bash scripts/release/rehearse_rc.sh
```

## Changelog Discipline

Every user-facing change should be reflected in `CHANGELOG.md` under `Unreleased` before release.
