# Architecture Decision Records

Reference date: 2026-08-29.
Status: Current.

This directory contains Architecture Decision Records (ADRs) documenting key technical choices in the framework.

## Index

- [ADR-001: stdlib-First Runtime Design](ADR-001-stdlib-first.md) — Build on Go's standard library; pull in third-party libraries only when stdlib has no equivalent.
- [ADR-002: Django-Inspired CLI Design](ADR-002-django-cli.md) — Adopt Django's `manage.py` command vocabulary (`new`, `migrate`, `createsuperuser`, …) for project lifecycle ergonomics.
- [ADR-003: Project Identity — Nucleus](ADR-003-project-identity-nucleus.md) — Rename the framework from `GoFrame` to `Nucleus`; new module path, CLI binary, public package, and config filename.
- [ADR-004: Casbin Enforcer Mounted with Default-Deny by `App.New`](ADR-004-casbin-default-deny-mount.md) — Mount the RBAC enforcer in the default app path with deny-everything-except-bootstrap-routes semantics; `WithOpenAuthz()` as the explicit opt-out.
- [ADR-005: ES256 JWT Signing and AWS Secrets Manager Key Resolution](ADR-005-es256-and-aws-secrets-manager.md)
- [ADR-006: CSRF Hardening — Constant-Time Comparison and Mandatory Encryption Key](ADR-006-csrf-hardening.md)
- [ADR-007: Secret Redaction in the Structured Logger](ADR-007-slog-secret-redaction.md)
- [ADR-008: CSRF Middleware Follow-ups — Logger, Key Type, Secure-By-Default Cookie](ADR-008-csrf-followups.md)
- [ADR-009: Schema-Level Drift Detection](ADR-009-schema-drift-detection.md)
- [ADR-010: Fluent API v2 for `pkg/nucleus` over `pkg/app`](ADR-010-fluent-api-v2-pkg-nucleus.md)
- [ADR-011: Oracle Identifier Casing — Unquoted (Upper-Folded)](ADR-011-oracle-identifier-casing.md)
- [ADR-012: Prometheus Metrics Exposition via the OTel SDK (`pkg/observe`)](ADR-012-prometheus-metrics-exporter.md)
- [ADR-013: Real-App Readiness Decisions](ADR-013-real-app-readiness.md) — Retain `Module.Migrations`/`Jobs`/`Webhooks` as reserved shape with a boot WARN; `nucleus serve --without-defaults` for core-only parity; configurable CORS origins (empty = allow-all); document the two coexisting project layouts. (§R4 CORS-credentials posture superseded in part by ADR-014.)
- [ADR-014: CORS Credentials Secure Default (SEC-1)](ADR-014-cors-credentials-secure-default.md) — Flip the `corsAllowCredentials` default to `false` ahead of the ADR-013 §R4 major-version schedule; credentials require an explicit origin allow-list + opt-in; boot WARN on the misconfig. Closes audit finding SEC-1 (SPEC §2.4).
- [ADR-015: Dependency-Firewall `/vN` Resolution + Per-Leak Dispositions (F-4)](ADR-015-firewall-vn-resolution-and-leak-dispositions.md) — Resolve the firewall test's versioned-module-path matching and record per-leak dispositions (blessed vs. fix) surfaced by the F-4 audit.
- [ADR-016: Admin API Authentication Enforced at the Router Edge](ADR-016-admin-api-authn-at-router-edge.md) — Authenticate admin `/api/*` at the router edge (authn before any handler), authz per action; WARN when no auth provider is configured.
- [ADR-017: Admin Login Timing Equalization (Username-Enumeration Oracle)](ADR-017-admin-login-timing-equalization.md) — Equalize admin login timing with a constant-cost bcrypt compare on the unknown-user branch to close a username-enumeration timing oracle.
- [ADR-018: Admin Live View Consumes the Observability Bus](ADR-018-admin-observability-bus-migration.md) — The admin live view consumes the process-wide observability bus, so the live SQL feed shows every application query, not just the admin's own browsing.
- [ADR-019: Extract the Admin Panel into "orbit", a Separate Pluggable Module](ADR-019-extract-admin-to-orbit-module.md) — Extract the admin into `orbit`, a separate in-process Go module that embeds its own SPA and mounts via the extension API; clean break in the core. Closes fleetdesk #9.
- [ADR-020: Public SQL Ingest on the EventBus Runtime Surface](ADR-020-eventbus-public-sql-ingest.md)
- [ADR-021: Driver-level SQL instrumentation for the live feed](ADR-021-driver-level-sql-instrumentation.md)
- [ADR-022: Vertical-Slice Modules — Carried Policies, Migrations and Templates](ADR-022-vertical-slice-modules.md) — Modules declare their own RBAC rows and CSRF exemptions (operator `deny` overrides), apply their embedded migrations through one explicit `Runtime` call (boot still never mutates the schema on its own), carry templates as `fs.FS` under a per-module namespace, and gain a package-per-feature generator. Executes the ADR-013 §R1 follow-up.
- [ADR-023: Registros de proveedores — los subsistemas dejan de elegir backend con un switch cerrado](ADR-023-provider-registries.md) — Los subsistemas dejan de elegir backend con un `switch` cerrado: almacenamiento, store de sesión y autenticación se registran por nombre, y la cadena de autenticación distingue rechazo de fuente no disponible.
- [ADR-024: The LDAP backend is a separate module inside this repository](ADR-024-ldap-provider-module.md) — El backend LDAP es un módulo hermano en este repositorio, no un plugin externo ni una dependencia del framework: separado para el compilador y para `go.mod`, un solo producto para todo lo demás.
- [ADR-025: The plugin contract lives in a leaf package](ADR-025-plugin-contract-leaf-package.md) — El contrato que implementa un backend de autenticación se muda a un paquete hoja, para que su autor no herede 115 paquetes de terceros por implementar dos métodos. Los nombres antiguos quedan como alias.
- [ADR-026: The storage contract lives in a leaf package; the other registries, measured](ADR-026-storage-contract-leaf-package.md) — La misma medición y la misma mudanza para almacenamiento (301 → 2) y store de sesión (115 → 0); el registro de correo ya estaba limpio.
- [ADR-027: The backend contract ships a conformance suite](ADR-027-backend-conformance-suite.md) — El contrato de autenticación trae una suite de conformidad que un tercero apunta contra su propio backend: comprueba propiedades del contrato, no calidad.
- [ADR-028: Federated sign-in is a second contract, not a wider first one](ADR-028-federated-authentication-seam.md) — La autenticación federada (OIDC, SAML) es un contrato SEGUNDO, no un `Backend` más ancho, y el framework se queda la custodia del state anti-CSRF para que un proveedor no pueda olvidarlo.
- [ADR-029: A third party can intercept the request lifecycle](ADR-029-request-interceptors.md) — Un tercero puede interceptar el ciclo de la petición registrando por nombre, con el orden declarado desde la configuración; y el observador de SQL deja de ser un hueco único que se pisaba en silencio.
- [ADR-030: The cloud backends ship as their own modules](ADR-030-cloud-backends-as-modules.md) — Los cuatro SDK cloud (S3, GCS, Azure y Secrets Manager) pesaban 42,6 MB de un hola-mundo de 75,6 MB, así que salen a módulos hermanos: el framework se queda con almacenamiento local y secretos de entorno, y quien use un backend de nube lo importa — el error dice la línea exacta.
- [ADR-031: Database drivers and telemetry exporters ship as their own modules](ADR-031-drivers-and-exporters-as-modules.md) — Los cinco drivers de BD y los dos exportadores de telemetría salen a módulos propios: el hola-mundo baja de 37 MB a 19 y de 176 módulos a 87, los build tags `mssql`/`oracle` desaparecen, y la configuración no cambia ni una línea — sólo hay que añadir el import, que `nucleus add` escribe por ti.
- [ADR-032: A packaging move ships in a minor with a guided error; a `!` is a major](ADR-032-packaging-moves-and-the-major-line.md) — Mover código a un módulo hermano sin cambiar firmas ni comportamiento, con el error de arranque diciendo el import exacto, es una minor con declaración de compatibilidad; un `!` o un `BREAKING CHANGE:` es una major sin excepción y no se corrige después con `Release-As` (QADR-0002 arrastra a la suite). 1.23.0 queda clasificada así retroactivamente.
