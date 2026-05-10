---
sidebar_position: 1
slug: /
title: Introduction
---

# Nucleus

> Stdlib-first MVC + REST framework for Go.
>
> **Status:** pre-1.0 (`v0.x`). The compatibility SLO begins to accrue at
> `v1.0`. Until then, public surfaces may evolve between minor releases —
> changes are documented in [`CHANGELOG.md`](https://github.com/jcsvwinston/nucleus/blob/main/CHANGELOG.md).

Nucleus is a batteries-included framework for building MVC web applications
and REST APIs in Go. It targets the same productivity bar as Django while
staying as light and explicit as Gin. The runtime is built on the standard
library — `net/http`, `database/sql`, `log/slog`, `context` — and every
public symbol on the stable surface is governed by a contract.

## What you get

- **`pkg/app`** — the application container. One construction call wires
  config, logger, databases, sessions, mail, router, request scope, model
  registry and the admin panel. Lifecycle is explicit; there are no
  hidden globals.
- **`pkg/router`** — HTTP router and middleware chain (CORS, CSRF, rate
  limiting, OpenTelemetry instrumentation).
- **`pkg/db` + `pkg/model`** — `database/sql`-backed data layer with model
  metadata, migrations and a generic CRUD operator.
- **`pkg/admin`** — embedded admin panel (React + TypeScript, no CDN
  dependencies) auto-generated from registered models.
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

These are formalised in [`SPEC.md`](https://github.com/jcsvwinston/nucleus/blob/main/SPEC.md):

1. **Stdlib-first runtime** — third-party deps require an ADR.
2. **Explicit configuration & lifecycle** — no hidden global singletons.
3. **Compatibility by contract** — `pkg/*`, registered CLI commands and
   registered config keys are frozen by the tests under `contracts/`.
4. **Security by default** — sessions, CSRF, headers, transport ship with
   sane defaults.
5. **SQL-first operations** — deterministic CLI behaviour and explicit
   migrations.

## Who Nucleus is for

- Teams shipping internal tools, line-of-business apps and B2B SaaS that
  need a real admin out of the box.
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
