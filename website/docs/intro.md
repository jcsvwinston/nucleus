---
sidebar_position: 1
slug: /
title: Introduction
covers:
  - pkg/app.New
  - pkg/app.App
  - pkg/nucleus.New
  - pkg/nucleus.AppBuilder
  - pkg/router.New
  - pkg/db.NewMigrator
  - pkg/auth.NewJWTManager
  - pkg/mail.NewSender
  - pkg/storage.New
  - pkg/observe.NewLogger
---

# Nucleus

> Stdlib-first MVC + REST framework for Go.
>
> **Status:** stable `v1.x` line (v1.0.0 tagged 2026-07-10; current release
> v1.3.1 {/* x-release-please-version */}).
> The Compatibility SLO is active: stable surfaces are frozen by
> contract tests and change only through the documented deprecation policy —
> see [`CHANGELOG.md`](https://github.com/jcsvwinston/nucleus/blob/main/CHANGELOG.md).

Nucleus is a batteries-included framework for building MVC web applications
and REST APIs in Go. It targets the same productivity bar as Django while
staying as light and explicit as Gin. The runtime is built on the standard
library — `net/http`, `database/sql`, `log/slog`, `context` — and every
public symbol on the stable surface is governed by a contract.

## The shape of a Nucleus app

```go
package main

import "github.com/jcsvwinston/nucleus/pkg/nucleus"

func main() {
    nucleus.New().
        FromConfigFile("nucleus.yml").
        Use( /* middlewares */ ).
        Mount( /* modules */ ).
        Start()
}
```

The same `App` can be assembled three equivalent ways and the result is
verified identical by the contract tests:

- **Fluent** (shown above) — `nucleus.New().FromConfigFile(...).Mount(...).Start()` for the common case.
- **Direct struct** — construct `nucleus.App{Config: cfg, Options: opts}` for full programmatic control.
- **Bootstrap** — `pkg/app.New(cfg, opts...)` for tests and embedding inside another binary.

See [Concepts → Application](./concepts/application.md) for the full
lifecycle and the equivalences between surfaces.

## What you get

- **`pkg/app`** — the application container. One construction call wires
  config, logger, databases, sessions, mail, router, request scope and model
  registry. Lifecycle is explicit; there are no hidden globals.
- **`pkg/router`** — HTTP router and default middleware chain (CORS, rate
  limiting, security headers, OpenTelemetry instrumentation). CSRF protection
  is available but opt-in — mount it with `router.WithCSRF(...)`; it is not in
  the default chain.
- **`pkg/db` + `pkg/model`** — `database/sql`-backed data layer with model
  metadata, migrations and a generic CRUD operator.
- **`pkg/auth` / `pkg/authz`** — JWT, password hashing, session manager
  with `memory` / `sql` / `redis` stores, Casbin-based RBAC.
- **`pkg/mail`** — pluggable mail drivers (`noop`, `smtp`, `sendgrid`).
- **`pkg/storage`** — provider-agnostic file storage (local, S3, GCS,
  Azure).
- **`pkg/tasks`** — background jobs on Asynq + Redis with the
  transactional outbox pattern in `pkg/outbox`.
- **`pkg/observe`** — structured logging on `log/slog` and OpenTelemetry
  hooks.
- **`pkg/openapi`** — explicit OpenAPI document mounting.
- **`nucleus`** — a deterministic CLI that scaffolds projects, runs
  migrations, manages fixtures, and inspects the running app.

## Design principles

Five principles guide every decision in the framework:

1. **Stdlib-first runtime** — every new third-party dependency has to be
   argued for in writing and reviewed for its maintenance and supply-chain
   cost before it is taken.
2. **Explicit configuration & lifecycle** — no hidden global singletons.
3. **Compatibility by contract** — `pkg/*`, registered CLI commands and
   registered config keys are frozen by the tests under `contracts/`.
4. **Security by default** — sessions, security headers and CORS ship with
   safe defaults (RBAC default-deny, cross-origin denied unless allow-listed).
   CSRF protection is available via `router.WithCSRF(...)` but is opt-in, not
   auto-mounted.
5. **SQL-first operations** — deterministic CLI behaviour and explicit
   migrations.

## Who Nucleus is for

- Teams shipping internal tools, line-of-business apps and B2B SaaS. The
  orbit module (a separate, pluggable product) adds a full admin panel when
  you need one.
- Backend services that prefer SQL and explicit migrations to ORM magic.
- Operators who want a single binary and a deterministic CLI rather than a
  collection of half-integrated libraries.

## Who Nucleus is not for

- Toy services where `net/http` plus three handler functions are enough.
- Teams whose primary requirement is an opinionated GraphQL stack.
- Pre-microservice exercises in maximum modularity. Nucleus assumes you
  want a coherent application boundary first; modularization is a v1.x
  concern.

## Where to start

- **[Getting started](./getting-started/installation.md)** — install the
  CLI, scaffold a project, run the server.
- **[Concepts](./concepts/application.md)** — the application container,
  the configuration model, routing, the data layer.
- **[Architecture](./architecture/principles.md)** — the principles and
  the compatibility policy that pin the public surface.
