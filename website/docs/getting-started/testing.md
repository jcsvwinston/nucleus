# Testing your application

End-to-end tests do not need to build a binary, launch a child process or
poll `/healthz` by hand. The `pkg/nucleustest` kit (experimental) boots the
full application **inside the test process** and stops it on cleanup:

```go
import (
    "net/http"
    "testing"

    "github.com/jcsvwinston/nucleus/pkg/nucleus"
    "github.com/jcsvwinston/nucleus/pkg/nucleustest"

    "example.com/myapp/internal/modules"
)

func TestWidgetsAPI(t *testing.T) {
    srv := nucleustest.Start(t, nucleus.New().
        FromConfigFile("testdata/nucleus.yml").
        Mount(modules.WidgetModule()))

    resp, err := srv.Client().Get(srv.URL("/widgets"))
    if err != nil {
        t.Fatal(err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("want 200, got %d", resp.StatusCode)
    }
}
```

`Start` builds the application from the builder, replaces the configured
port with a free loopback port (parallel tests never collide), runs the full
startup sequence — modules, jobs, webhooks, middleware — and waits for
`/healthz` before returning. A `t.Cleanup` shuts the application down
gracefully; an unexpected run error fails the test. `StartApp` is the
direct-struct counterpart for a hand-built `nucleus.App`.

## Exercising protected routes

`MintToken` issues a bearer token signed with the application's own
`jwt_secret` — the same material the framework's JWT middleware validates:

```go
token := srv.MintToken("user-1", "tester", "admin")
req, _ := http.NewRequest(http.MethodGet, srv.URL("/api/admin/stats"), nil)
req.Header.Set("Authorization", "Bearer "+token)
resp, err := srv.Client().Do(req)
```

Applications configured with asymmetric keysets (`jwt_keys`) should mint
through `auth.NewJWTManagerFromKeys` directly.

## A per-test database, with your real schema

`nucleustest.TempSQLite(t)` gives every test its own database file (removed
with the test's temp dir), and `srv.MigrateDir` applies your project's SQL
migrations through the real migrator — ledger and checksums included, so a
second call is a no-op, exactly like `nucleus migrate up`:

```go
cfg := app.DefaultConfig()
cfg.Databases = nucleustest.TempSQLite(t)

srv := nucleustest.StartApp(t, nucleus.App{Config: cfg, Modules: myModules})
srv.MigrateDir("../../migrations")
```

## Asserting against the database

`srv.DB()` is the application's managed `*sql.DB` — the same pool your
modules use — so a test can close the loop an HTTP assertion alone cannot:

```go
resp, _ := srv.Client().Post(srv.URL("/widgets"), "application/json", body)
// status assertions…

var n int
_ = srv.DB().QueryRow("SELECT COUNT(*) FROM widgets WHERE name = 'x'").Scan(&n)
// …and the row is REALLY there.
```

`srv.Runtime()` exposes the full module-facing handle (logger, authorizer,
dialect-aware database handles, storage, mailer) when a test needs more
than the pool. Under the hood the kit captures it by mounting one extra
module — the name `nucleustest_probe` is reserved for it.

## Proving persistence

Because starting and stopping is cheap, the restart pattern — the only test
that distinguishes a real repository from an in-memory one — is three lines:

```go
first := nucleustest.StartApp(t, app())
// ... create a record over HTTP ...
first.Stop()

second := nucleustest.StartApp(t, app())
// ... the record must still be served ...
```

With `TempSQLite`, point both boots at the same map (call it once, reuse
the value) so the second boot sees the first boot's file.

## Under the hood

The kit is a thin wrapper over `nucleus.RunContext(ctx, app)`: `Run` with a
caller-owned lifetime, where cancelling the context triggers the same
graceful shutdown a SIGTERM does. Embedders with their own harness can use
it directly.

For fast unit tests of a generated resource, the scaffold already ships a
self-contained test file with an in-memory fake of the repository interface
— no database, no HTTP server. The kit is for the layer above: booting the
real thing.
