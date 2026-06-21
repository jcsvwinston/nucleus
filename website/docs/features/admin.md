---
sidebar_position: 1
title: Admin panel (orbit)
covers:
  - pkg/app.App.RegisterModel
  - pkg/authz.Enforcer
config_keys:
  - rbac_policy_file
  - multitenant.enabled
---

# Admin panel (orbit)

The admin panel has moved out of the framework core and into **orbit** — a
separate, pluggable Go module (`github.com/jcsvwinston/orbit`) that versions
and ships independently of Nucleus. The framework core no longer contains a
built-in admin panel as of ADR-019 Slice 2.4.

## Why the extraction

Bundling a React + TypeScript SPA inside the framework binary required every
Nucleus application to carry the admin's transitive dependencies whether or not
the app used the panel. Extracting it into a dedicated module lets the core
stay lightweight and lets the admin evolve on its own schedule, with its own
release tags.

## Integrating orbit into your app

orbit is a Nucleus extension module distributed as a separate Go module at
`github.com/jcsvwinston/orbit`. Add it to your project with:

```bash
go get github.com/jcsvwinston/orbit@v0.1.0
```

orbit's first tagged release is **v0.1.0** (`@latest` resolves to it; pin
`@v0.1.0` for reproducible builds). It is pre-1.0, so the API may still change
before v1.0.

Mount it with the standard `.Mount()` call in `main.go`:

```go
package main

import (
    "github.com/jcsvwinston/nucleus/pkg/nucleus"
    "github.com/jcsvwinston/orbit"
)

func main() {
    nucleus.New().
        FromConfigFile("nucleus.yml").
        Mount(orbit.Module(orbit.Config{Prefix: "/admin"})).
        Start()
}
```

orbit is not part of the `github.com/jcsvwinston/nucleus` module and versions
independently. The cluster-telemetry subsystem ships as sibling modules
(`github.com/jcsvwinston/orbit/proto`, `github.com/jcsvwinston/orbit/agent`,
`github.com/jcsvwinston/orbit/server`); most applications only need the root
`orbit` module for the admin panel.

## What orbit provides

- **Login page** with light/dark theme support.
- **Model browser** — every registered model becomes a list view with
  search, filtering, ordering and pagination.
- **CRUD UI** with form generation from `db:` and `validate:` tags.
- **Bulk actions** with per-row error reporting.
- **Import / export** — CSV, JSON, SQL, with validation on import.
- **Audit log** — every CRUD operation is recorded.
- **System pulse** — Go runtime, DB pool, feature flags, jobs, outbox,
  cluster nodes.
- **Health dashboard** — DB / Redis / mail connectivity.
- **Migration view** — applied vs. pending, with diff hints.
- **Job queue inspector** — Asynq runtime details.
- **File storage browser** — works against the configured `pkg/storage`
  backend.
- **Live traffic inspection** — HTTP, SQL, sessions.
- **Email stats** and **deployment info**.

## Configuration

All orbit configuration lives under `modules.orbit.*` in `nucleus.yml`. The
`admin_*` family of config keys that existed in earlier releases has been
removed from the framework core (see
[`docs/reference/CONFIG_KEY_REGISTRY.md`](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/CONFIG_KEY_REGISTRY.md));
consult orbit's own documentation for its configuration schema.

## Authorization and RBAC

The Casbin RBAC enforcer is a core framework feature, not an orbit feature.
Configure it with the `rbac_policy_file` key:

```yaml
rbac_policy_file: ./rbac_policy.csv
```

The `admin_rbac_policy_file` key is a deprecated alias for `rbac_policy_file`
that still works with a startup `WARN`. Prefer `rbac_policy_file` in all new
configuration.

The enforcer is available to all application code (including orbit) through
the `Runtime.Authorizer()` accessor.

## Registering models

Model registration is a core framework concern. Register a model with the
application's model registry so that orbit (and other tools) can discover it:

```go
a.Models.Register(&Article{})
```

`App.RegisterModel` is the stable method on `pkg/app.App`.

## Multi-tenancy

When `multitenant.enabled: true` is set, orbit respects the tenant context
provided by the framework's request-scope resolver.

## Effective-config inspection

The `GET /_/config` HTTP endpoint that previously shipped with the admin
subsystem has been removed from the framework core. Use
`nucleus config print --effective` from the CLI for effective merged
configuration inspection:

```bash
nucleus config print --effective --config nucleus.yml
```

See [CLI overview → Effective config](../cli/overview.md#effective-config-nucleus-config-print---effective)
for the full flag reference.
