# pkg/router — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

Router construction, middleware hooks, unified request context helpers (`Context`, `ContextHandler`), rendering/binding/pagination helpers; rate-limit middleware keys per-tenant when a tenant is resolved in context

## Notes

Request/response helper behavior is contract surface.

`CSRFMiddleware`, which panics on a misconfiguration, is deprecated toward `NewCSRFMiddleware`, which returns it as an error (DEP-2026-012, removal in v2.0.0); the default stack (`csrf_enabled`) builds its middleware without the panicking wrapper.
