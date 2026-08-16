# pkg/nucleustest — contract

Lifecycle: `experimental`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

`Start(tb, *nucleus.AppBuilder)`, `StartApp(tb, nucleus.App)`, `Server`
(`BaseURL`, `Stop`, `Client`, `URL`, `MintToken`).

## Notes

In-process E2E harness (DX-22): boots the full `nucleus.RunContext`
startup sequence on a free loopback port inside the test process — no `go
build`, no child process, no hand-rolled `/healthz` polling — and shuts it
down gracefully via `t.Cleanup`. `MintToken` issues bearer tokens against
the application's configured `jwt_secret`; asymmetric keysets
(`jwt_keys`) should mint through `auth.NewJWTManagerFromKeys` directly.
Experimental: the surface may still grow (per-test databases, fixture
loading) before it freezes.
