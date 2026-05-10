---
sidebar_position: 3
title: Routing & middleware
---

# Routing & middleware

`pkg/router` is Nucleus's HTTP layer. It is built on `net/http`, exposes
its own `Router` and `Context` types, and ships an opinionated middleware
chain.

## Defining routes

```go
r := a.Router

r.Get("/api/articles",     listArticles)
r.Post("/api/articles",    createArticle)
r.Get("/api/articles/{id}", showArticle)
r.Put("/api/articles/{id}", updateArticle)
r.Delete("/api/articles/{id}", deleteArticle)

r.Group("/admin/api", func(g *router.Group) {
    g.Use(adminAuthMiddleware)
    g.Get("/stats", adminStats)
})
```

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
    return router.HandlerFunc(func(c *router.Context) error {
        start := time.Now()
        err := next.ServeHTTP(c)
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

a.MountOpenAPI("/api/openapi.json", openapi.From(myDoc))
```

There is no auto-generation of the document from handler reflection —
that path was deliberately not taken. The contract you ship is the one
you wrote.
