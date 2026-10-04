# pkg/nucleustest — contract

Lifecycle: `experimental`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

`Start(tb, *nucleus.AppBuilder)`, `StartApp(tb, nucleus.App)`, `Server`
(`BaseURL`, `Stop`, `Client`, `URL`, `MintToken`; the client — `Request`,
`Get`, `Post`, `Put`, `Patch`, `Delete`, `Cookies`, `SetCookie`,
`CSRFToken`, `WithCSRF`, `SignIn`, `SignInAccount`, `SignOut`; the runtime —
`Runtime`, `DB`, `MigrateDir`, `Stream`; the doubles — `SentMail`,
`ResetMail`, `Stored`, `StoredKeys`, `EnqueuedTasks`,
`ResetEnqueuedTasks`), `Response` (`JSON`, `String`), `RequestOption`
(`WithHeader`, `WithQuery`, `WithBearer`), `TempSQLite`, `Transactional`,
`Make[T]`, `MakeN[T]`, `NewHTTPRecorder` (`HTTPRecorder`,
`RecordedRequest`), `Stream`/`StreamEvent`, and the module conformance
kit — `CheckModule(tb, nucleus.ModuleSpec) []nucleus.ModuleCheck` and
`CheckModuleIn(tb, *nucleus.AppBuilder, nucleus.ModuleSpec)
[]nucleus.ModuleCheck`, which fail the test once per check
`nucleus.CheckModule` reports failed.

## Notes

In-process E2E harness (DX-22): boots the full `nucleus.RunContext`
startup sequence on a free loopback port inside the test process — no `go
build`, no child process, no hand-rolled `/healthz` polling — and shuts it
down gracefully via `t.Cleanup`. `MintToken` issues bearer tokens against
the application's configured `jwt_secret`; asymmetric keysets
(`jwt_keys`) should mint through `auth.NewJWTManagerFromKeys` directly.
Experimental: the surface still grows with the A10 arc (the client, test
data, the doubles and the module conformance kit landed in it) before it
freezes.
