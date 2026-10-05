# pkg/plugins — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

Plugin SDK v1 envelopes/capability constants, inventory/probe/runtime execution APIs (the host side), `Serve`/`ServeIO`/`Plugin`/`Fail` (the plugin side), and `Policy` — the allowlist the `plugins.*` configuration keys build, which `DiscoverAllowed` and the mail runtime enforce

## Notes

SDK `v1` contract is intended stable through `v1.x`.
