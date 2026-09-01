---
sidebar_position: 6
title: Using Quark with Nucleus
covers: []
config_keys: []
---

# Using Quark with Nucleus

Nucleus's own data layer is deliberately SQL-first —
[Concepts → Models & database](../concepts/models-and-database.md) opens
with "there is no ORM". That is a statement about the framework core, not
about the suite: **Quark**, the suite's ORM, runs inside a Nucleus module
like any other library, and the integration bridges make the pairing more
than coexistence.

## When to reach for it

- You want an ORM — typed query builder, Active Record models, relations,
  model-driven migrations — instead of hand-written SQL and `database/sql`
  scanning.
- You want Orbit's Data Studio to browse and edit your domain models
  (`orbit/quarkdatasource`).
- You want the SQL your handlers run to appear in Orbit's live SQL feed,
  correlated to the request (`orbit/quarkbridge`).

Nucleus model tags (`db:"column:title;required"`) and Quark model tags
(`db:"title" quark:"not_null"`) are **two different dialects with two
different migration systems** — pick one per model. A Quark model is not
registered with Nucleus's model registry; it lives entirely on the Quark
client.

## The pattern

The shape comes from
[`examples/showcase_demo`](https://github.com/jcsvwinston/nucleus/tree/main/examples/showcase_demo),
which runs this end to end. Build the Quark client in `main`, migrate the
models it owns, and hand the client to your module:

```go
// main.go — the client is built once, outside the module.
client, err := quark.New("sqlite", "app.db")
if err != nil {
    log.Fatal(err)
}
defer client.Close()

if err := shop.Migrate(ctx, client); err != nil { // RegisterModel + MigrateRegistered
    log.Fatal(err)
}

app, err := nucleus.New().
    FromConfigFile("nucleus.yml").
    Mount(shop.Module(client)).
    Build()
```

Inside the module, handlers use Quark's typed API instead of raw SQL:

```go
func (m *module) listArticles(c *nucleus.Context) error {
    q := quark.For[Article](c.Request.Context(), m.client).OrderBy("id", "DESC")
    if author := c.Query("author_id"); author != "" {
        q = q.Where("author_id", "=", author)
    }
    articles, err := q.List()
    if err != nil {
        return err
    }
    return c.JSON(http.StatusOK, map[string]any{"articles": articles, "count": len(articles)})
}
```

## Optional bridges (Orbit)

Both are one-line opt-ins once Orbit is in the picture:

**Live SQL feed** — derive a bridged client in `OnStart`, where the
`Runtime` is available; every statement it runs shows up in Orbit's live
view, correlated to the request:

```go
OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
    bridged, err := m.base.WithOptions(
        quark.WithMiddleware(quarkbridge.New(rt.Observability())),
    )
    if err != nil {
        return err
    }
    m.client = bridged
    return nil
},
```

**Data Studio over Quark models** — back Orbit's Data Studio with the same
client, so `/admin` browses and edits your Quark models:

```go
ds := quarkdatasource.New(client)
_ = quarkdatasource.Register[Author](ds)
_ = quarkdatasource.Register[Article](ds)

// … Mount(orbit.Module(orbit.Config{Prefix: "/admin", DataSource: ds}))
```

## The suite, end to end

`examples/showcase_demo` is the runnable version of this whole page — a
Nucleus app whose domain runs on Quark, with Orbit mounted on `/admin` and
both bridges wired. It is its own Go module (so Quark and Orbit stay out
of the framework's dependency graph) and builds standalone from published
tags:

```bash
cd examples/showcase_demo
go run .

curl -s localhost:8091/api/articles | jq .
# then open http://localhost:8091/admin and watch the live SQL feed
```

Its [README](https://github.com/jcsvwinston/nucleus/tree/main/examples/showcase_demo)
walks through both bridges and the optional fleet leg.

## Where do I start? (the suite in one paragraph)

- **Only a data layer** — use
  [Quark](https://github.com/jcsvwinston/quark) on its own; it does not
  need Nucleus.
- **An application** — use Nucleus; keep its SQL-first layer, or put Quark
  inside your modules as above.
- **An admin over either** — mount
  [Orbit](https://github.com/jcsvwinston/orbit); it reads Nucleus's model
  registry natively and Quark models via `quarkdatasource`.
