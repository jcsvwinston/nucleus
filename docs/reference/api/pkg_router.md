# pkg/router — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

Router construction, middleware hooks, unified request context helpers (`Context`, `ContextHandler`), rendering/binding/pagination helpers; rate-limit middleware keys per-tenant when a tenant is resolved in context

## Notes

Request/response helper behavior is contract surface.
