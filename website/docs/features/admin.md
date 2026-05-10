---
sidebar_position: 1
title: Admin panel
---

# Admin panel

`pkg/admin` is an embedded admin panel — React 19 + TypeScript + Vite +
Tailwind + shadcn/ui — generated from your registered models. It is part
of the binary; there is no separate build step at runtime, no CDN
dependency, no external service.

## Enabling it

```yaml
# nucleus.yml
admin:
  enabled: true
  base_path: /admin
```

It is on by default in the `mvc` template and off by default in `api`.

## What you get out of the box

- **Login page** with light/dark theme support.
- **Model browser** — every registered model becomes a list view with
  search, filtering, ordering and pagination.
- **CRUD UI** with form generation from `db:` and `validate:` tags.
- **Bulk actions** with per-row error reporting.
- **Import / export** — CSV, JSON, SQL, with validation on import.
- **Audit log** — every CRUD operation is recorded (in-memory, bounded;
  10 000 entries by default).
- **System pulse** — Go runtime, DB pool, feature flags, jobs, outbox,
  cluster nodes.
- **Health dashboard** — DB / Redis / mail connectivity.
- **Migration view** — applied vs. pending, with diff hints.
- **Job queue inspector** — Asynq runtime details.
- **File storage browser** — works against the configured `pkg/storage`
  backend.
- **Live traffic inspection** — HTTP, SQL, sessions, with optional
  cluster relay.
- **Email stats** and **deployment info**.

## Authorization

Two layers stack:

1. **`AdminAuth.Authorize`** — a per-action hook called before every
   admin operation. The default implementation checks that the session
   belongs to a superuser; replace it to plug in your own policy.
2. **Casbin RBAC** — optional, enabled by setting
   `admin_rbac_policy_file`. Policies and role assignments are managed
   from the UI itself or via the API.

## Multi-tenancy

When `multi_tenant.enabled: true`, the admin:

- detects the tenant column via `db:"tenant"` or the conventional
  `tenant_id` field,
- auto-filters CRUD queries by the current tenant,
- auto-injects the tenant ID on insert,
- exposes a tenant selector in the header for users with cross-tenant
  scope.

Tenant context propagates through the request middleware, so application
code reads from the same source.

## Customizing the UI

The admin UI is part of the framework and ships pre-built. You can:

- add a custom action to a model via the model registry (`AdminActions`
  hook),
- override individual model labels and field metadata via the `admin:`
  struct tag,
- replace the `AdminAuth` interface with your own,
- mount the admin under a different `base_path`.

For deeper customisation — replacing pages, adding new sections — fork
the `pkg/admin/ui` source. The UI has zero CDN dependencies, so a fork
remains self-hostable.
