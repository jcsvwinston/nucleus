---
sidebar_position: 1
title: Principles
---

# Principles

Five non-negotiable principles guide every decision in Nucleus. They are
formalised in [`SPEC.md`](https://github.com/jcsvwinston/nucleus/blob/main/SPEC.md);
this page is the human-readable summary.

## 1. Stdlib-first runtime

The runtime is built on `net/http`, `database/sql`, `log/slog`, and
`context.Context`. New third-party dependencies require an
[ADR](https://github.com/jcsvwinston/nucleus/tree/main/docs/adrs) and a
dependency-impact review.

The reasoning is operational, not aesthetic:

- The standard library is the most reviewed Go code in existence; its
  semantics rarely surprise.
- Third-party deps are the dominant source of maintenance debt and
  supply-chain risk.
- Go's release cadence is predictable; building on the stdlib lets us
  adopt new features without coordinating across vendors.

When a dependency is genuinely worth taking — Asynq for the task queue,
Casbin for RBAC, OpenTelemetry for tracing — the ADR records why, what
the abstraction boundary is, and how we would replace it.

## 2. Explicit configuration & lifecycle

There are no hidden global singletons, no implicit `init()` registration,
no package-level state masquerading as the framework. Every Nucleus
application is created by an explicit call:

```go
a, err := app.New(cfg, opts...)
```

The implications:

- Multiple `App` instances coexist trivially in the same process —
  end-to-end tests run a real `App`, not a mock.
- Lifecycle is observable. `App.Run` blocks; `App.Shutdown` runs hooks
  in reverse order; `defer a.Shutdown(ctx)` is enough.
- Wiring is reviewable. The composition root is in `cmd/server/main.go`
  (or in a fluent call when you opt in); nothing happens at import
  time.

## 3. Compatibility by contract

The stable surface is explicit:

- exported symbols in `pkg/*`,
- registered CLI commands,
- registered config keys.

Each of these is frozen by tests under
[`contracts/`](https://github.com/jcsvwinston/nucleus/tree/main/contracts).
Removals require a deprecation entry; rename-and-keep-the-shim is the
default path. Details: [Compatibility policy](./compatibility).

## 4. Security by default

Production-sensitive defaults ship enabled. The defaults are designed so
that "I forgot to configure CSRF" looks like an explicit opt-out rather
than a missing line:

- CSRF on for state-changing form posts.
- Session cookies `Secure` in `production`.
- CORS denies unknown origins.
- Rate limiting on every public endpoint.
- Argon2id password hashing with versioned cost parameters.
- TLS hardening on the embedded HTTP server when it is used directly.

Each setting is reachable from `nucleus.yml`. If you need to relax a
default, you do it deliberately, in writing.

## 5. SQL-first operations

There is no ORM. Migrations are SQL files; the CLI applies them in
order; queries are written in SQL. `pkg/db` adds `database/sql`
ergonomics, telemetry and health checks; `pkg/model` adds metadata for
the admin panel and CRUD helpers.

This is a constraint we want, not one we tolerate:

- Postgres, MySQL and SQLite all behave differently in subtle ways.
  Hiding that behind an ORM either produces the lowest-common-denominator
  query or quietly emits incompatible SQL.
- Migrations as SQL files are reviewable and replayable independently
  of the binary that wrote them.
- The CLI is deterministic: the same `nucleus migrate` against the
  same database ends in the same state.

## What follows from the principles

- The compatibility SLO has bite — see [Compatibility policy](./compatibility).
- The CLI is a first-class product, not an afterthought — see
  [CLI reference](../cli/overview).
- The admin panel is part of the framework, not a separate project —
  see [Features → Admin panel](../features/admin).
