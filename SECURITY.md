# Security Policy

## Supported Versions

GoFrame is currently in pre-1.0 development (`v0.x`).

Security fixes are prioritized for:

- latest `main`
- latest tagged release line

Older tags may not receive patches.

## Reporting a Vulnerability

Please do not open public issues for potential vulnerabilities.

Instead:

1. Open a private GitHub Security Advisory if available for this repository.
2. If unavailable, contact project maintainers privately and include:
   - vulnerability type and impact
   - affected version/commit
   - reproduction details or proof of concept
   - suggested mitigation (if known)

We aim to acknowledge reports quickly and provide status updates as triage progresses.

## Coordinated Disclosure

We follow coordinated disclosure whenever possible:

- report received and validated
- fix prepared and reviewed
- release published
- advisory disclosed with remediation details

## Hardening Guidance

For production deployments, review:

- `goframe check --deploy`
- `docs/DEVELOPER_MANUAL.md`
- `docs/RELEASE_CHECKLIST.md`
