# Testing your application

End-to-end tests do not need to build a binary, launch a child process, or
poll `/healthz` by hand.

The `pkg/nucleustest` kit boots your full application **inside the test
process** and stops it on cleanup, and gives you a client that speaks your
API's language. This page covers booting a test server, making requests and
reading their answers, cookies, CSRF and sessions, protected routes, giving
each test its own database, and asserting against the data afterwards.

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

`Start` does four things. It builds the application from the builder;
replaces the configured port with a free loopback port, so parallel tests
never collide; runs the full startup sequence (modules, jobs, webhooks,
middleware); and waits for `/healthz` before returning. A registered
`t.Cleanup` shuts the application down gracefully, and an unexpected run
error fails the test.

`StartApp` is the direct-struct counterpart, for a hand-built `nucleus.App`.

## Making requests

`Request` sends one request and reads the whole answer; `Get`, `Post`,
`Put`, `Patch` and `Delete` fill in the method. A body that is not `nil`, a
`[]byte`, a `string` or an `io.Reader` is encoded as JSON, and `Accept`
defaults to `application/json`:

```go
resp := srv.Post("/widgets", map[string]any{"name": "gear", "size": 3})
if resp.Status != http.StatusCreated {
    t.Fatalf("want 201, got %d: %s", resp.Status, resp)
}

var widget struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}
resp.JSON(t, &widget) // fails the test when the body is not the JSON expected
```

Options adjust one request: `nucleustest.WithHeader`, `WithQuery`,
`WithBearer` and `srv.WithCSRF()` (below). A transport failure is fatal — the
application is in this process, so an unreachable server is a bug in the
test, never a condition to assert on. `srv.Client()` still returns the
underlying `*http.Client` for anything the helpers do not cover.

## Cookies, CSRF and sessions

The client keeps the cookies the application sets — session, CSRF — across
requests, **`Secure` ones included**: the application issues them with the
flag, the test server speaks plain HTTP on loopback, and the kit makes the
same exception browsers make for `localhost`. `srv.Cookies()` shows what it
holds; `srv.SetCookie` stores one as if the application had.

When the application runs the CSRF middleware (`csrf_enabled: true`), a
state-changing request needs the token the middleware issued. `srv.WithCSRF()`
fetches it when the client holds none and sends it in the header the
middleware reads; `srv.CSRFToken()` returns it for a form field:

```go
resp := srv.Post("/widgets", widget, srv.WithCSRF())
```

To act as a signed-in user on routes that read the session, open a session in
the application's own store — no password, no login form, no flows around
them:

```go
srv.SignInAccount("acc-7", "seven@example.test") // the keys pkg/accounts reads
resp := srv.Get("/me")                           // sees account acc-7
srv.SignOut()
```

`SignIn(values)` opens a session with any keys your own modules read.

## Exercising protected routes

`MintToken` issues a bearer token signed with the application's own
`jwt_secret` — the same material the framework's JWT middleware validates —
and `WithBearer` sends it:

```go
token := srv.MintToken("user-1", "tester", "admin")
resp := srv.Get("/api/admin/stats", nucleustest.WithBearer(token))
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

From the builder, pin it with `WithDatabases`:

```go
srv := nucleustest.Start(t, nucleus.New().
    FromConfigFile("testdata/nucleus.yml").
    WithDatabases(nucleustest.TempSQLite(t)).
    Mount(modules.WidgetModule()))
```

`WithDatabases` beats both the file and the `NUCLEUS_*` environment layer.
That last part matters more than it looks: the environment layer is applied
after the file, so in a shell carrying your project's variables — the
ordinary development loop — a test that thought it had its own SQLite file
would open your development database instead, and `MigrateDir` would write
to it. The kit now logs a warning when it sees `NUCLEUS_DATABASES__*` set,
but pinning is the way to be sure.

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
