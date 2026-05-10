---
sidebar_position: 1
title: Application container
---

# Application container

`pkg/app` is the composition root. One call wires every subsystem and
returns a fully validated application:

```go
import "github.com/jcsvwinston/nucleus/pkg/app"

cfg, err := app.LoadConfig("nucleus.yml")
if err != nil {
    return err
}

a, err := app.New(cfg)
if err != nil {
    return err
}
defer a.Shutdown(ctx)

return a.Run(ctx)
```

## What `app.New` wires

`app.New(cfg)` (no options) initialises:

- the canonical configuration view (`pkg/app/config.go`)
- the `slog` logger (`pkg/observe`)
- the SQL database map by alias — `database_default` plus
  `databases.<alias>`
- the mail sender (`pkg/mail`)
- the session manager (`pkg/auth`) backed by the configured store
  (`memory | sql | redis`)
- the HTTP router and middleware chain (`pkg/router`)
- the request scope resolver for multi-site / multi-tenant setups
  (`pkg/app/requestscope.go`)
- the model registry (`pkg/model`)
- the embedded admin panel (`pkg/admin`)

This is the default, full-stack mode and matches what the `mvc`
scaffold template produces.

## Core-only mode

Pass `app.WithoutDefaults()` to opt out of the default subsystems and wire
only what you need:

```go
a, err := app.New(cfg, app.WithoutDefaults())
```

This is the path the `api` template uses. From here you attach the
subsystems you actually want using extensions.

## Extensions

Extensions are first-class pluggable subsystems:

```go
type Extension interface {
    Name() string
    Attach(a *App) error
    Shutdown(ctx context.Context) error
}
```

```go
a, err := app.New(cfg,
    app.WithoutDefaults(),
    app.WithExtensions(myExtension),
)
```

`Attach` runs at startup and may register routes, middleware, models, and
shutdown hooks on the application. `Shutdown` runs in reverse order during
graceful shutdown.

## What `App` exposes

| Member                            | Purpose                                       |
| --------------------------------- | --------------------------------------------- |
| `App.DB`                          | The default database handle.                  |
| `App.DBs`                         | All opened databases keyed by alias.          |
| `App.Database(alias)`             | Look up a specific database.                  |
| `App.DatabaseForRequest(r)`       | Resolve the database for the current request scope (multi-site / multi-tenant). |
| `App.Router`                      | The mounted router.                           |
| `App.Models`                      | The model registry.                           |
| `App.Run(ctx)` / `App.Shutdown(ctx)` | Lifecycle entry points.                    |

## Lifecycle

`App.Run` is blocking. It listens, serves traffic, and on context
cancellation runs each registered shutdown hook in reverse attach order
before returning. The graceful timeout is bounded by `server.shutdown_timeout`
in `nucleus.yml`.

There are no hidden globals. `App` does not register a singleton; each
`app.New` call produces an independent application. This makes
end-to-end testing straightforward — run a real `App` on a random port
and tear it down on test completion.
