# pkg/openapi — contract

Lifecycle: `experimental`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

Minimal OpenAPI 3.1 document model, JSON serialization helpers, runtime document handler helpers, and small schema/response/parameter/security helpers for scaffolded contracts

## Notes

Official experimental base layer for project-level API contracts via `internal/contracts`, `nucleus openapi`, and explicit runtime serving; the current helper subset covers repeated JSON schema shapes, shared `data`/`count` response envelopes, structured JSON error responses, empty responses, explicit path/query parameters including the scaffolded optional `q` search convention, and OpenAPI security declarations — `SecurityScheme`/`SecurityRequirement` types, `Components.SecuritySchemes`, document- and operation-level `Security` (operation-level is a pointer so an explicit empty `PublicSecurity()` override survives marshalling), plus the `BearerAuthScheme`/`APIKeyScheme`/`Require`/`RequireSecurity`/`PublicSecurity`/`AddSecurityScheme` helpers (added 2026-06-18, fleetdesk finding #33) — but the overall surface may still expand before `v1.0`. **Outside the v1.0 promise** (v1 gate A-1a, 2026-07-08): stays `experimental` through v1.0 and free to evolve; from v0.11 the stable surfaces no longer name its types (DEP-2026-008 re-signed the builder/mount to stdlib `http.Handler`).
