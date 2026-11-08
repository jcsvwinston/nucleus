---
sidebar_position: 6
title: Using Quark with Nucleus
covers: []
config_keys: []
---

# Using Quark with Nucleus

**Quark** is the suite's ORM and the data layer a Nucleus feature is
written on: `nucleus generate module`, `generate resource` and `startapp`
produce a Quark model and a storage on `quark.For[T]` over the framework's
pool, and [Concepts → Models and the
database](../concepts/models-and-database.md) teaches that shape. This page
is the hand-over: what Quark adds beyond the five CRUD calls, the second
way to build a client, and the bridges into Orbit.

## What the ORM adds

- A typed query builder, Active Record models, relations and hooks
  (`BeforeCreate` … `AfterFind`) instead of hand-written SQL and
  `database/sql` scanning — [Quark's own
  documentation](https://github.com/jcsvwinston/quark) is where they are
  written up; nothing here duplicates it.
- Orbit's Data Studio browsing and editing your domain models
  (`orbit/quarkdatasource`).
- The SQL your handlers run appearing in Orbit's live SQL feed, correlated
  to the request (`orbit/quarkbridge`).

Quark model tags (`db:"title" quark:"not_null"`) and the deprecated
`pkg/model` tags (`db:"column:title;required"`) are **two different
dialects** — a struct belongs to one layer. A Quark model is not registered
with Nucleus's model registry; it lives entirely on the Quark client. In a
Nucleus application the schema stays in the SQL migrations the CLI renders
and `nucleus migrate up` applies; Quark reads and writes the tables.

## The two ways to build the client

**Over the framework's pool** — what the generators write and the
quickstart shows: in the module's `OnStart`, `quark.NewWithDB(driver,
rt.DB())` derives the client over the connection the framework already
owns, so there is one pool and the framework closes it. The driver name is
the `database/sql` one (`sqlite`, `pgx`, `mysql`, `sqlserver`, `oracle`),
with a blank import of Quark's driver module for the engine
(`github.com/jcsvwinston/quark/drivers/<engine>`).

```go
OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
    client, err := quark.NewWithDB("sqlite", rt.DB())
    if err != nil {
        return err
    }
    m.client = client
    return nil
},
```

**A client of its own** — for an application whose Quark models and
migrations predate Nucleus, or that lets Quark migrate the models it owns
(`RegisterModel` + `MigrateRegistered`): build the client in `main`, migrate,
and hand it to the module. Two pools, each with an owner; the module closes
nothing either way.

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

`nucleus new myapp --template suite` writes this whole page as a project: a
Nucleus app whose domain runs on Quark, with Orbit mounted on `/admin` and
both bridges wired. Run it, then open `/admin` and watch the live SQL feed
while you curl the API.

## Where do I start? (the suite in one paragraph)

- **Only a data layer** — use
  [Quark](https://github.com/jcsvwinston/quark) on its own; it does not
  need Nucleus.
- **An application** — use Nucleus; keep its SQL-first layer, or put Quark
  inside your modules as above.
- **An admin over either** — mount
  [Orbit](https://github.com/jcsvwinston/orbit); it reads Nucleus's model
  registry natively and Quark models via `quarkdatasource`.
