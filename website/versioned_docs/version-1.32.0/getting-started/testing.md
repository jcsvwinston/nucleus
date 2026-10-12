# Testing your application

End-to-end tests do not need to build a binary, launch a child process, or
poll `/healthz` by hand.

The `pkg/nucleustest` kit boots your full application **inside the test
process** and stops it on cleanup, and gives you a client that speaks your
API's language. This page covers booting a test server, making requests and
reading their answers, cookies, CSRF and sessions, protected routes, giving
each test its own database, asserting against the data afterwards, and
checking a module against what the framework expects of it.

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

    resp := srv.Get("/widgets")
    if resp.Status != http.StatusOK {
        t.Fatalf("want 200, got %d: %s", resp.Status, resp)
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

The tests the CLI writes are built this way. The suite starter's
`shop/module_test.go` (`nucleus new --template suite`) and the test
`nucleus generate module` writes next to a slice boot the module through the
kit and speak to it through its client, decoding the answers into the
module's own types — a working example of everything below, in your own
project.

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

## Test data with one call

`Make` persists one record of a registered model with sensible defaults and
returns it, primary key filled — strings become `<field>-<n>`, numbers `n`,
times now, from a per-process sequence so two records never collide on a
value the test did not choose. Booleans, pointers and foreign keys stay zero
for the test to set. `MakeN` makes several; an override function sets what
the test cares about:

```go
w := nucleustest.Make[Widget](srv)                                   // name "name-1", size 1
gear := nucleustest.Make[Widget](srv, func(w *Widget) { w.Name = "gear" })
many := nucleustest.MakeN[Widget](srv, 5)
```

The record is written through `model.CRUD` against the model's database with
the application's dialect, so what the test reads back is what a handler
would have written. The model has to be registered — mounted in a module's
`Models` — and its table has to exist (`srv.MigrateDir`, or
`srv.Runtime().AutoMigrate` in a test).

## A transaction per test

`Transactional` makes every database run the whole test inside one
transaction that is rolled back when the test ends — for the application's
routes, not only the test's own handle. Two tests can share one database
without seeing each other, and a suite on the application's PostgreSQL needs
no cleanup between them:

```go
dbs := nucleustest.Transactional(t, map[string]app.DatabaseConfig{
    "default": {URL: os.Getenv("TEST_DATABASE_URL")},
})
srv := nucleustest.Start(t, nucleus.New().WithDatabases(dbs).Mount(modules.WidgetModule()))
```

It works one level below the pool: each database is opened through a driver
registered for this test alone, which hands the pool a single connection
with a transaction already begun. Transactions the application begins become
savepoints, so its own commits and rollbacks keep their meaning. On engines
whose DDL is transactional (PostgreSQL, SQLite, SQL Server) the schema the
test created is rolled back too; on MySQL a `CREATE TABLE` commits, so
create tables outside the scope there. Two consequences of "one
connection": statements are serialised, and a handler that reads a result
set while issuing another statement on the same connection behaves as the
engine allows (SQLite interleaves, PostgreSQL does not). `TempSQLite` stays
the right tool for a test that needs the pool's real concurrency.

## What the application sends out

A flow sends a mail, stores a file, enqueues a job, calls another service.
The kit keeps each of those in the process and reads it back:

```go
srv.Post("/signup", map[string]string{"email": "ana@example.test"})

mails := srv.SentMail()                 // what the application sent (mail_driver: memory, the kit's default for tests)
data := srv.Stored("avatars/ana.png")   // what it stored (storage.provider: memory, or whatever it runs)
keys := srv.StoredKeys("avatars/")
jobs := srv.EnqueuedTasks()             // what it enqueued, whether or not a worker ran it
```

Mail: when the application would discard mail (`mail_driver` empty or
`noop`) the kit selects the `memory` driver, so `SentMail` works without
configuration; a driver you chose — `smtp`, a plugin — is kept. Storage:
`Stored` and `StoredKeys` read the application's own store; `storage.provider:
memory` keeps files in the process. Jobs: `EnqueuedTasks` is the record the
in-process provider keeps of every enqueue; the jobs runtime exists once a
module registers a job.

In a multi-tenant application (`multitenant.enabled`) the store keeps each
tenant's files under the tenant's prefix: a request for `acme` that stores
`avatars/ana.png` writes `acme/avatars/ana.png`. `StoredFor` and
`StoredKeysFor` read one tenant's files by the keys its requests used:

```go
srv.Post("/avatars", img, nucleustest.WithHeader("X-Tenant-ID", "acme"))

data := srv.StoredFor("acme", "avatars/ana.png")
keys := srv.StoredKeysFor("acme", "avatars/") // ["avatars/ana.png"]: no tenant prefix, no other tenant's keys
```

`Stored` and `StoredKeys` read below the tenant scoping: the whole store as
the backend holds it, each tenant's keys under its prefix
(`srv.Stored("acme/avatars/ana.png")`). All four are the test's reads, not
the application's, so they never meet the rule for storage operations that
name no tenant: they work under `multitenant.require_tenant_storage: true`,
and in the default mode they leave the one-time warning about the shared key
space to the application's own code. `StoredFor` in an application that is
not multi-tenant fails the test — its keys carry no tenant.

For the HTTP the application makes to other services, `NewHTTPRecorder`
starts a server that records every request and answers what you tell it:

```go
rec := nucleustest.NewHTTPRecorder(t)
rec.Respond(http.StatusAccepted, `{"queued":true}`)
// point the application at rec.URL through its configuration …
srv.Post("/orders", order)
reqs := rec.Requests()                  // method, path, query, headers, body
```

Code that takes an `*http.Client` can be given `rec.Client()`, which sends
every request to the recorder whatever host it names.

## Holding a response to the API document

An application that serves its OpenAPI document (`WithOpenAPIDocument`) can
be held to it from any test. `AssertConforms` looks up the operation the
document declares for the request that produced a response and checks the
response against it: a declared status, a declared content type, a body that
matches the schema.

```go
resp := srv.Get("/api/articles/1")
srv.AssertConforms(t, resp) // fails naming each field that departs: "/title: is required"
```

A handler that stops writing a field the document promises, or answers a
status the operation does not declare, fails the test that calls it rather
than the client that reads it. `srv.Document()` returns the document itself.

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
resp := srv.Post("/widgets", map[string]any{"name": "x"})
// status assertions…

var n int
_ = srv.DB().QueryRow("SELECT COUNT(*) FROM widgets WHERE name = 'x'").Scan(&n)
// …and the row is REALLY there.
```

A test of a "that email is taken" branch works on the kit's SQLite as it
does in your application: the kit links SQLite together with the code that
recognises its unique-constraint errors, so `db.IsUniqueViolation` answers
`true` for a duplicate key without importing `drivers/sqlite` in the test.

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

## Checking a module against the framework

A module's mistakes used to surface as a boot failure in some application's
test, one boot at a time: a malformed policy row stopped the boot before
anyone learnt that two routes conflicted. `CheckModule` checks a module on
its own and names every defect at once:

```go
func TestNotesModule(t *testing.T) {
    nucleustest.CheckModule(t, notes.Module())
}
```

It runs, one at a time, the checks the framework applies to a module when it
boots, and fails the test once for every check the module does not pass,
with the framework's own words for the defect:

| check | what it asks |
|---|---|
| `name` | non-empty, not another module's, and lowercase letters, digits and underscores — the name is a config key, an environment variable, a template namespace and a webhook path segment |
| `prefix` | empty, or a clean absolute path: policy rows and CSRF exemptions resolve against it as written |
| `config` | the typed configuration binds, takes its `default:` tags and passes its `validate:` tags |
| `depends-on` | every module `DependsOn` names is mounted, and no declaration closes a cycle |
| `requires` | every database `Requires` names, and `DefaultDB`, is configured |
| `policies` | every row is well formed, loads, and grants something the module serves |
| `csrf-exempt` | every exemption is well formed, stays under the module, and covers a route it serves |
| `templates` | the embedded templates parse |
| `models` | every model registers |
| `start` | `OnStart` returns nil |
| `migrations` | the embedded migrations apply on a fresh database |
| `jobs`, `webhooks` | the registrations are valid |
| `routes` | the routes register — no duplicate, no conflict with the framework's own, no `Resource` verb the controller lacks |
| `shutdown` | `OnShutdown` returns nil within the shutdown budget, before the framework closes the database |

Three of them catch what boot lets through without a word: a policy row or a
CSRF exemption about a path the module does not serve loads and never
applies; a prefix without its leading slash serves the routes at `/api`
while its rows name `api/…`; and a `DefaultDB` nobody configured hands
`OnStart` a nil database.

`CheckModule` uses a default application with a temporary SQLite database.
A module that requires a database by name, reads `modules.<name>` from a
configuration file, or ships migrations for another engine is checked
against the application it belongs to, with the other modules mounted so
their names are taken:

```go
nucleustest.CheckModuleIn(t, nucleus.New().
    FromConfigFile("testdata/nucleus.yml").
    WithDatabases(dbs).
    Mount(accounts.Module()),
    billing.Module())
```

Both return every verdict — a `[]nucleus.ModuleCheck`, a name and an error
each. `nucleus.CheckModule` runs the same checks without a test, for a tool
that wants them.

## Under the hood

The kit is a thin wrapper over `nucleus.RunContext(ctx, app)`: `Run` with a
caller-owned lifetime, where cancelling the context triggers the same
graceful shutdown a SIGTERM does. Embedders with their own harness can use
it directly.

For fast unit tests of a generated resource, the scaffold already ships a
self-contained test file with an in-memory fake of the repository interface
— no database, no HTTP server. The kit is for the layer above: booting the
real thing.
