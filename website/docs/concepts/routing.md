---
sidebar_position: 3
title: Routing & middleware
covers:
  - pkg/nucleus.New
  - pkg/nucleus.Router
  - pkg/nucleus.Module
  - pkg/nucleus.Methods
  - pkg/nucleus.Handler
  - pkg/nucleus.Middleware
  - pkg/router.New
  - pkg/router.Context
  - pkg/router.Context.Param
  - pkg/router.Context.Query
  - pkg/router.Context.JSON
  - pkg/router.FromHTTP
  - pkg/router.CORSMiddleware
  - pkg/router.CSRFMiddleware
  - pkg/router.RateLimitMiddleware
  - pkg/router.TelemetryMiddleware
  - pkg/router.Recoverer
  - pkg/router.RequestID
  - pkg/app.App.MountOpenAPI
config_keys:
  - rate_limit_requests
  - rate_limit_window
  - rate_limit_burst
  - rate_limit_by_route
---

# Routing & middleware

Nucleus has two routing surfaces. `pkg/nucleus` is the **module-facing
layer** and is the recommended entry point for application code.
`pkg/router` is the **lower-level implementation** and is only needed
when integrating third-party HTTP handlers or constructing an application
directly with `pkg/app`.

## Defining routes (module layer — `pkg/nucleus`)

Inside a `Module[C].Routes` callback, the `nucleus.Router` interface is
the only surface you should use. It does not expose any `pkg/router`
types, so modules do not take a hard dependency on the router
implementation.

```go
var articlesModule = nucleus.Module[struct{}]{
    Name:   "articles",
    Prefix: "/api/articles",
    Routes: func(r nucleus.Router, _ struct{}) {
        r.Get("/",     listArticles)
        r.Post("/",    createArticle)
        r.Get("/{id}", showArticle)
        r.Put("/{id}", updateArticle)
        r.Delete("/{id}", deleteArticle)
    },
}
```

`nucleus.Router` supports three coexisting styles:

- **Flat declarative** — `r.Get("/path", handler)` for simple or
  audit-sensitive modules.
- **REST resource** — `r.Resource("/path", controller, nucleus.Methods(...))` for
  CRUD modules. Only the requested verbs are registered; reflection is
  not used.
- **Nested groups** — `r.Group("/prefix", func(g nucleus.Router) { ... })` for
  areas with nested URL hierarchy. Middleware added inside the callback
  is scoped to the group.

```go
Routes: func(r nucleus.Router, _ struct{}) {
    r.Group("/admin", func(g nucleus.Router) {
        g.Get("/stats", adminStats)
        g.Get("/users", listUsers)
    })
},
```

`Middleware` type: `func(http.Handler) http.Handler` — standard
`net/http` middleware. No framework-specific wrapper type is needed.

## Lower-level routing (`pkg/router`)

`pkg/router` is used directly only in two cases:

1. You are assembling an app with `pkg/app` (not `pkg/nucleus`).
2. You need to mount an arbitrary `http.Handler` via
   `a.Router.Mount(prefix, handler)`.

In the `pkg/app` context, `a.Router` is a `*router.Router` and handlers
receive `*router.Context`:

```go
// pkg/app-level wiring (not module code)
a.Router.Get("/api/articles", listArticles)
a.Router.Post("/api/articles", createArticle)

a.Router.Mux.Route("/admin/api", func(sub *router.Mux) {
    sub.Use(adminAuthMiddleware)
    sub.Get("/stats", adminStats)
})
```

`router.Handler` is `func(*router.Context) error`; errors bubble up to
the recovery / logging middleware.

`Router.Mount(prefix, handler)` mounts an arbitrary `http.Handler` —
useful for embedding third-party handlers or a second app.

## The `Context` type

Handlers receive a `*router.Context` (or, in fluent mode, a
`*nucleus.Context` that wraps it). The context exposes:

- `Request` / `ResponseWriter`
- path parameters via `c.Param("id")`
- query string helpers (`c.Query`, `c.QueryInt`, …)
- body binding (`c.BindJSON`, `c.BindXML`, `c.BindForm`)
- response helpers (`c.JSON`, `c.XML`, `c.String`, `c.Status`)
- the request-scoped `context.Context`
- the resolved request scope (site, tenant) when multi-site is on

## Built-in middleware

The default middleware chain (full-stack mode) installs:

| Middleware            | Purpose                                            |
| --------------------- | -------------------------------------------------- |
| Recovery              | Recovers from panics, logs with stack trace.       |
| Request ID            | Generates / propagates an X-Request-ID.            |
| Structured logging    | Emits one `slog` line per request with timing.    |
| OpenTelemetry         | Wraps the handler in an OTel span (when enabled). |
| CORS                  | Configured from `cors.*` keys.                     |
| CSRF                  | Configured from `csrf.*` keys (form-based apps).  |
| Rate limiting         | Configured from `rate_limit_*` keys.               |
| Request scope         | Resolves multi-site / multi-tenant context.        |

Each middleware is opt-out at the config level; none of them rely on
hidden state. The order is fixed and documented — handlers can rely on
the request having a logger, a request ID and a span by the time they
run.

## Custom middleware

```go
func auditMiddleware(next router.Handler) router.Handler {
    return router.Handler(func(c *router.Context) error {
        start := time.Now()
        err := next(c)
        slog.InfoContext(c.Request.Context(),
            "audit",
            "method", c.Request.Method,
            "path",   c.Request.URL.Path,
            "took",   time.Since(start),
        )
        return err
    })
}

r.Use(auditMiddleware)
```

`router.Handler` is a thin wrapper over `http.Handler` that returns an
`error`. Errors bubble up to the recovery / logging middleware where they
are translated into a JSON or HTML response according to the request
`Accept` header.

## Mounting an OpenAPI document

The runtime ships an explicit OpenAPI mount:

```go
import "github.com/jcsvwinston/nucleus/pkg/openapi"

// MountOpenAPI takes an openapi.DocumentProvider — a func() *openapi.Document.
a.MountOpenAPI("/api/openapi.json", func() *openapi.Document { return myDoc })
```

There is no auto-generation of the document from handler reflection —
that path was deliberately not taken. The contract you ship is the one
you wrote.
