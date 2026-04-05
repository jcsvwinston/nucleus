# Go Version Policy

Reference date: 2026-04-05.
Status: Current.

This document defines how GoFrame handles Go runtime compatibility.

## Goals

- Keep the framework usable for teams on stable enterprise environments.
- Allow contributors to use modern Go toolchains for development and release automation.
- Avoid accidental breakages caused by implicit toolchain upgrades.

## Supported Versions

- Minimum supported Go version: `1.23`
- Recommended Go version for active development and releases: latest `1.26.x`

## Rules

1. Public compatibility target
- New framework features must compile and run on Go `1.23+` unless explicitly documented otherwise.

2. Development baseline
- CI and release workflows may run on newer Go versions to stay aligned with ecosystem support.

3. Upgrading minimum version
- Any minimum-version bump must be explicit and documented in:
  - `go.mod`
  - `CHANGELOG.md`
  - `README.md`
  - this policy file

4. Third-party dependencies
- Dependency upgrades should be evaluated for Go version constraints before merge.

## Contributor Guidance

Before opening a PR:

```bash
go test ./...
```

For release-level confidence:

```bash
bash scripts/release/rehearse_rc.sh
```
