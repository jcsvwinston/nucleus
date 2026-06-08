# Architecture Decision Records

Reference date: 2026-05-09.
Status: Current.

This directory contains Architecture Decision Records (ADRs) documenting key technical choices in the framework.

## Index

- [ADR-001: stdlib-First Runtime Design](ADR-001-stdlib-first.md) — Build on Go's standard library; pull in third-party libraries only when stdlib has no equivalent.
- [ADR-002: Django-Inspired CLI Design](ADR-002-django-cli.md) — Adopt Django's `manage.py` command vocabulary (`new`, `migrate`, `createsuperuser`, …) for project lifecycle ergonomics.
- [ADR-003: Project Identity — Nucleus](ADR-003-project-identity-nucleus.md) — Rename the framework from `GoFrame` to `Nucleus`; new module path, CLI binary, public package, and config filename.
- [ADR-004: Casbin Enforcer Mounted with Default-Deny by `App.New`](ADR-004-casbin-default-deny-mount.md) — Mount the RBAC enforcer in the default app path with deny-everything-except-bootstrap-routes semantics; `WithOpenAuthz()` as the explicit opt-out.
- [ADR-013: Real-App Readiness Decisions](ADR-013-real-app-readiness.md) — Retain `Module.Migrations`/`Jobs`/`Webhooks` as reserved shape with a boot WARN; `nucleus serve --without-defaults` for core-only parity; configurable CORS origins (empty = allow-all); document the two coexisting project layouts. (§R4 CORS-credentials posture superseded in part by ADR-014.)
- [ADR-014: CORS Credentials Secure Default (SEC-1)](ADR-014-cors-credentials-secure-default.md) — Flip the `corsAllowCredentials` default to `false` ahead of the ADR-013 §R4 major-version schedule; credentials require an explicit origin allow-list + opt-in; boot WARN on the misconfig. Closes audit finding SEC-1 (SPEC §2.4).
