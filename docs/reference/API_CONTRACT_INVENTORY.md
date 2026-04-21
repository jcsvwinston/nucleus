# API Contract Inventory

Reference date: 2026-04-07.
Status: Current.

This file defines lifecycle tags for GoFrame public API surfaces and documents extension points and non-contract zones.

## Lifecycle Tags

- `stable`: treated as compatibility contract; avoid breaking changes (pre-`v1.0` breaks require explicit migration notes).
- `transitional`: public and supported, but still maturing; breaking adjustments may occur with explicit release notes.
- `experimental`: no compatibility guarantee yet; intended for early validation and feedback.

Policy references:

- `docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md`
- `docs/governance/COMPATIBILITY_SLO.md`

## Public Package Inventory

| Surface | Lifecycle | Contract Scope | Notes |
| --- | --- | --- | --- |
| `pkg/app` | `stable` | `Config`, `DefaultConfig`, `LoadConfig`, `New`, `App` lifecycle methods (`Run`, `Shutdown`, `RegisterModel`, `MountAdmin`, `OnShutdown`) | Core application bootstrap contract. |
| `pkg/db` | `stable` + `experimental` | `db.New`, `db.DB`, migrator APIs (`NewMigrator`, migration lifecycle methods), SQL URL support | URL schemes `sqlite://`, `postgres://`/`postgresql://`, `mysql://` are `stable`; `sqlserver://`/`mssql://` and `oracle://` are `experimental` until lane promotion. |
| `pkg/model` | `stable` | `BaseModel`, metadata extraction, registry, CRUD interfaces and hooks | Foundation for model/admin integration. |
| `pkg/router` | `stable` | Router construction, middleware hooks, unified request context helpers (`Context`, `ContextHandler`), rendering/binding/pagination helpers | Request/response helper behavior is contract surface. |
| `pkg/auth` | `stable` | JWT manager, claims context helpers, session manager/store APIs | Multi-store session surface is contracted (`memory`, `sql`, `redis`). |
| `pkg/authz` | `stable` | Enforcer creation and authorization middleware helpers | Casbin-backed authz boundary for apps. |
| `pkg/mail` | `stable` | Mail sender abstraction, provider registry (`RegisterProvider`), sender construction (`NewSender`) | Built-ins + external plugin/legacy bridge are supported. |
| `pkg/plugins` | `stable` | Plugin SDK v1 envelopes/capability constants, inventory/probe/runtime execution APIs | SDK `v1` contract is intended stable through `v1.x`. |
| `pkg/openapi` | `experimental` | Minimal OpenAPI 3.1 document model and JSON serialization helpers | Intended as the first contract-generation lane for scaffolded APIs; schema shape may expand before `v1.0`. |
| `pkg/tasks` | `stable` | Task manager config/runtime + JSON task helpers | Queue runtime boundary for app code. |
| `pkg/observe` | `stable` | Logger/context correlation helpers and OTel setup entrypoint | Telemetry backend internals are not part of app contract. |
| `pkg/errors` | `stable` | Domain error constructors + HTTP writer | Error payload shape and status mapping are public behavior. |
| `pkg/validate` | `stable` | Validation entrypoint + custom rule registration | Shared validation boundary for handlers/models. |
| `pkg/signals` | `stable` | In-process event bus types and APIs | Used by model hooks and domain events. |
| `pkg/admin` | `transitional` | Admin panel mount/handler integration surface | Embedded UI details can evolve faster than core runtime APIs. |

## Extension Points

| Extension Point | Lifecycle | Contract |
| --- | --- | --- |
| In-process mail provider registration (`mail.RegisterProvider`) | `stable` | Register custom provider factories without forking framework internals. |
| External capability plugins (`goframe-plugin-<provider>`) using SDK `v1` | `stable` | Capability envelopes, exit-code mapping, and probe flow are contracted. |
| Legacy mail plugin bridge (`goframe-mail-<driver>`) | `transitional` | Supported for compatibility; capability plugin path is preferred. |
| External CLI command bridge (`goframe-<name>`) | `transitional` | Command delegation interface is supported but kept intentionally minimal. |

## Explicit Non-Contract Surfaces

These surfaces are intentionally outside compatibility guarantees:

- `internal/*` packages and all non-exported implementation details.
- Frontend implementation details under `pkg/admin/ui/*` (except mounted admin route behavior).
- Test helper APIs and environment variables used only by tests/CI harnesses.
- Generated scaffold internals beyond documented structure expectations (`docs/reference/PROJECT_LAYOUT.md`).

## Contract Review Rule

When changing any `stable` surface:

1. update this inventory if contract shape changes,
2. add/adjust compatibility tests,
3. include migration notes in `CHANGELOG.md` if behavior is user-visible.

## Freeze Enforcement

Stable no-removal freeze is enforced in `contracts/freeze_test.go` with baselines under `contracts/baseline/`:

- `cli_primary_commands.txt`
- `cli_json_status_keys.txt` (stable command-status JSON envelope/data keys for automation-critical commands)
- `config_key_patterns.txt`
- `api_exported_symbols.txt` (stable packages only)

Intentional baseline refresh workflow:

```bash
GOFRAME_UPDATE_CONTRACT_BASELINE=1 go test ./contracts -run '^TestContractFreeze_APIExportedSymbols_NoRemovals$' -count=1
bash scripts/ci/check_contract_freeze.sh
```
