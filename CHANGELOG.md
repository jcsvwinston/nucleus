# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
while in pre-1.0 mode (`v0.x.y`).

## [Unreleased]

## [0.5.0] - 2026-03-31

### Added

- Cross-OS release asset smoke workflow (`.github/workflows/release_asset_smoke.yml`).

### Changed

- Promoted `v0.5.0-rc1` to stable after successful artifact execution checks on Linux, macOS, and Windows.

## [0.5.0-rc1] - 2026-03-31

### Added

- Phase 5 release-candidate baseline:
  - CI workflow (`.github/workflows/ci.yml`)
  - tag-based release workflow (`.github/workflows/release.yml`)
  - release rehearsal workflow (`.github/workflows/rehearsal.yml`)
  - GoReleaser config for multi-platform artifacts (`.goreleaser.yaml`)
  - rehearsal script (`scripts/release/rehearse_rc.sh`)
  - versioning strategy docs (`docs/VERSIONING.md`)
  - release checklist (`docs/RELEASE_CHECKLIST.md`)
  - Go version support policy (`docs/GO_VERSION_POLICY.md`)

### Changed

- Project status docs aligned with current roadmap and phase closures.
- `goframe version` now prints build-injected release versions instead of a fixed value.

## [0.4.0] - 2026-03-31

### Added

- Bun-first SQL layer and consolidated migration/seed CLI flow.
- Rich admin SPA with:
  - command palette
  - filters and sorting
  - bulk selected export
  - tabs/detail panels
  - accessibility and recoverable-error hardening
- Runnable example app (`examples/mvc_api`) combining MVC + API + Admin.
- CLI project bootstrap via `goframe new`.
- Smoke E2E test for the official example.

### Fixed

- Admin SPA serving reliability when mounted under `/admin` prefix.

---

[Unreleased]: https://github.com/jcsvwinston/GoFrame/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/jcsvwinston/GoFrame/compare/v0.5.0-rc1...v0.5.0
[0.5.0-rc1]: https://github.com/jcsvwinston/GoFrame/compare/v0.4.0...v0.5.0-rc1
[0.4.0]: https://github.com/jcsvwinston/GoFrame/releases/tag/v0.4.0
