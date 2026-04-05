# Documentation Map

Reference date: 2026-04-05.
Status: Current.

This file is the canonical entrypoint for GoFrame documentation.

## Start Here (Current)

- [QUICKSTART.md](QUICKSTART.md)
- [DETAILED_TUTORIAL.md](DETAILED_TUTORIAL.md)
- [DEVELOPER_MANUAL.md](DEVELOPER_MANUAL.md)

## Core References (Current)

- [PROJECT_LAYOUT.md](PROJECT_LAYOUT.md)
- [CLI_BEST_PRACTICES.md](CLI_BEST_PRACTICES.md)
- [CLI_DJANGO_PARITY.md](CLI_DJANGO_PARITY.md)
- [MAIL_PROVIDERS.md](MAIL_PROVIDERS.md)
- [PLUGIN_SDK.md](PLUGIN_SDK.md)
- [OBSERVABILITY_BASELINE.md](OBSERVABILITY_BASELINE.md)

## Roadmaps and Status (Current)

- [ENTERPRISE_ROADMAP.md](ENTERPRISE_ROADMAP.md)
- [V0.6.0_ROADMAP.md](V0.6.0_ROADMAP.md)

## Release and Governance (Current)

- [RELEASE_CHECKLIST.md](RELEASE_CHECKLIST.md)
- [VERSIONING.md](VERSIONING.md)
- [GO_VERSION_POLICY.md](GO_VERSION_POLICY.md)
- [CI_MATRIX.md](CI_MATRIX.md)
- [../CHANGELOG.md](../CHANGELOG.md)

## Historical Archive (Reference Only)

These files are retained for project history and should not be treated as active roadmap source:

- [PHASE0.md](PHASE0.md)
- [PHASE1_BACKLOG.md](PHASE1_BACKLOG.md)
- [PHASE2.md](PHASE2.md)
- [PHASE3.md](PHASE3.md)
- [PHASE4.md](PHASE4.md)
- [PHASE5.md](PHASE5.md)

## Consolidation Rule

When docs conflict, use this precedence:

1. `README.md` + current docs listed above
2. `CHANGELOG.md` for release-scoped behavior
3. historical phase files only as context

## Terminology Conventions

- External provider binaries: prefer `goframe-plugin-<provider>`.
- Legacy mail binary naming remains supported as fallback: `goframe-mail-<driver>`.
- Use "current baseline" for shipped behavior and "historical" for archived phase notes.
