# Security Policy

## Supported Versions

The current release is **v1.23.0** <!-- x-release-please-version --> on the
stable `v1.x` line (release-please rewrites that version on every release,
and `scripts/ci/check_version_claims.sh` fails CI if it drifts).

Security fixes land on:

- `main` (always), and
- the **latest two tagged minor lines**, via patch releases when a fix
  warrants one.

Older tags are not patched — upgrade to a current tag to receive security
updates. Within `v1.x` that upgrade is bounded by the
[Compatibility SLO](docs/governance/COMPATIBILITY_SLO.md): stable surfaces
do not break in a minor.

## Reporting a Vulnerability

Please do not open public issues for potential vulnerabilities.

Instead:

1. Open a private GitHub Security Advisory
   ([Security → Report a vulnerability](https://github.com/jcsvwinston/nucleus/security/advisories/new))
   if available for this repository.
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

## What the framework itself guarantees

The default security posture (session cookie flags, CSRF, CORS denial,
default-deny RBAC, security headers) is **measured, not transcribed**: it
is frozen in `contracts/baseline/security_posture.txt` from a real HTTP
response of a booted application, and a change in either direction fails
CI until it is stated explicitly. If you find a way to make a deployment
weaker than that baseline claims without an explicit opt-out, that is a
vulnerability — report it.

## Hardening Guidance

For production deployments, review:

- `nucleus health --deploy` and `nucleus doctor --check security`
- [`docs/reference/DEVELOPER_MANUAL.md`](docs/reference/DEVELOPER_MANUAL.md)
- [`docs/governance/RELEASE_CHECKLIST.md`](docs/governance/RELEASE_CHECKLIST.md)
- the public site's [Operations → Security](https://jcsvwinston.github.io/quantum/nucleus/operations/security) page
