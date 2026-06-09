# MultiSite & MultiTenant Guide

Reference date: 2026-06-09.
Status: Current.

This guide covers Nucleus's MultiSite and MultiTenant request scope resolution, including site resolution, tenant routing, database alias routing, and security isolation.

## Table of Contents

- [Overview](#overview)
- [MultiSite Configuration](#multisite-configuration)
- [Site Resolution](#site-resolution)
- [MultiTenant Configuration](#multitenant-configuration)
- [Tenant Resolution](#tenant-resolution)
- [Tenant-to-Database Routing](#tenant-to-database-routing)
- [Security Isolation](#security-isolation)
- [Request Context Usage](#request-context-usage)
- [Production Checklist](#production-checklist)

---

## Overview

Nucleus provides request-scope resolution for:

| Feature | Purpose | Resolution Method |
|---------|---------|-------------------|
| **MultiSite** | Multiple sites/domains on one app | Host matching (exact or wildcard) |
| **MultiTenant** | Multiple tenants with data isolation | Subdomain or header-based resolution |

Both features work together and are resolved at the middleware level before reaching your handlers.

---

## MultiSite Configuration

Sites are defined as named entries under `multisite.sites`.  Each site lists
one or more host patterns in a `hosts` array (exact hosts or
`*.example.com` wildcards):

```yaml
# nucleus.yml
multisite:
  enabled: true
  default_site: main
  sites:
    main:
      hosts:
        - "example.com"
      database: default
    spanish:
      hosts:
        - "es.example.com"
      database: default
    wildcard:
      hosts:
        - "*.example.org"
      database: default
```

---

## Site Resolution

Nucleus resolves the current site from the request `Host` header.

### Exact host matching

```yaml
multisite:
  enabled: true
  sites:
    main:
      hosts:
        - "example.com"
    admin:
      hosts:
        - "admin.example.com"
```

### Wildcard matching

```yaml
multisite:
  enabled: true
  sites:
    regional:
      hosts:
        - "*.example.com"
```

Matches:

- `us.example.com`
- `eu.example.com`
- `asia.example.com`

### Accessing current site in handlers

`RequestScopeFromContext` returns a `RequestScope` value (not a pointer) and a
boolean `ok`.  The resolved site name is available as the `Site` string field:

```go
import "github.com/jcsvwinston/nucleus/pkg/app"

func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
    scope, ok := app.RequestScopeFromContext(r.Context())
    if !ok {
        // No scope resolved (multisite not configured, or middleware not mounted).
        return
    }

    if scope.Site != "" {
        log.Printf("Current site: %s", scope.Site)
    }
}
```

The convenience helper `app.SiteFromContext` returns the site name directly:

```go
site := app.SiteFromContext(r.Context()) // "" when not configured
```

---

## MultiTenant Configuration

```yaml
# nucleus.yml
multitenant:
  enabled: true
  resolver: subdomain       # Options: subdomain, header
  header: X-Tenant-ID       # Only used when resolver=header
  require_isolated_db: true
  tenants:
    acme:
      database: acme_db
    globex:
      database: globex_db
```

---

## Tenant Resolution

### Subdomain resolution

The tenant ID is extracted from the request subdomain.  With
`resolver: subdomain`, the framework strips the matched wildcard prefix or
the first subdomain label:

```
acme.example.com   -> tenant: "acme"
globex.example.com -> tenant: "globex"
```

```yaml
multitenant:
  resolver: subdomain
```

### Header resolution

The tenant ID is extracted from a request header:

```yaml
multitenant:
  resolver: header
  header: X-Tenant-ID
```

```bash
curl -H "X-Tenant-ID: acme" https://api.example.com/articles
```

### Accessing current tenant in handlers

`RequestScope.Tenant` is a plain string field.  The convenience helper
`app.TenantFromContext` reads it directly:

```go
import "github.com/jcsvwinston/nucleus/pkg/app"

func (h *Handler) GetArticles(w http.ResponseWriter, r *http.Request) {
    tenant := app.TenantFromContext(r.Context())
    if tenant == "" {
        http.Error(w, "tenant required", http.StatusUnauthorized)
        return
    }

    log.Printf("Current tenant: %s", tenant)

    // Query tenant-scoped data.
    articles, err := h.repo.FindByTenant(tenant)
    // ...
}
```

---

## Tenant-to-Database Routing

Nucleus maps tenants to database aliases.

### Explicit mapping

```yaml
multitenant:
  tenants:
    acme:
      database: acme_db
    globex:
      database: globex_db
```

### Template-based mapping

For large numbers of tenants, use a global template (`%s` or `{tenant}`):

```yaml
multitenant:
  database_alias_template: "tenant_%s"
```

This maps:

- `acme` -> `tenant_acme`
- `globex` -> `tenant_globex`

Per-site templates are also supported via
`multisite.sites.<site>.tenant_database_alias_template`.

### Using tenant-scoped database in handlers

`App.DatabaseForRequest` and `App.Database` are **methods on `*app.App`**,
not package-level functions.  Both return `(*db.DB, error)`:

```go
import "github.com/jcsvwinston/nucleus/pkg/app"

func (h *Handler) GetArticles(w http.ResponseWriter, r *http.Request) {
    // Get the database selected for this request's resolved tenant and site.
    database, err := h.app.DatabaseForRequest(r)
    if err != nil {
        http.Error(w, "tenant database not available", http.StatusInternalServerError)
        return
    }

    // Or look up a database by explicit alias.
    database, err = h.app.Database("acme_db")
    if err != nil {
        http.Error(w, "database not configured", http.StatusInternalServerError)
        return
    }

    // Query with tenant-scoped DB.
    rows, err := database.QueryContext(r.Context(), "SELECT * FROM articles WHERE tenant_id = ?", tenant)
    // ...
}
```

The primary (default) database is also exposed as the `App.DB` field:

```go
db := h.app.DB // *db.DB — the default database alias
```

---

## Security Isolation

### `require_isolated_db` guardrail

When `require_isolated_db: true` (the default), Nucleus enforces:

1. **Startup validation**: Rejects configuration where multiple tenants map to
   the same database alias.
2. **Request routing**: Returns a 500 error when the resolved database alias
   equals the site's shared alias, preventing cross-tenant data exposure.

```yaml
multitenant:
  require_isolated_db: true  # Recommended for production
```

### What this prevents

```yaml
# INVALID when require_isolated_db=true
multitenant:
  require_isolated_db: true
  tenants:
    acme:
      database: shared_db  # REJECTED: multiple tenants sharing one DB
    globex:
      database: shared_db  # REJECTED
```

### When to disable isolation

Only disable for specific controlled use cases:

```yaml
multitenant:
  require_isolated_db: false  # Use with caution
  tenants:
    internal:
      database: main_db  # Internal tenant shares main DB
```

---

## Request Context Usage

### Full scope access

`RequestScopeFromContext` returns a `RequestScope` struct value with plain
string fields (`Site`, `Tenant`, `DatabaseAlias`, `Host`):

```go
import "github.com/jcsvwinston/nucleus/pkg/app"

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
    scope, ok := app.RequestScopeFromContext(r.Context())
    if !ok {
        return // No multisite/multitenant middleware mounted.
    }

    // Site name (empty string when multisite is disabled).
    if scope.Site != "" {
        fmt.Printf("Site: %s\n", scope.Site)
    }

    // Tenant ID (empty string when multitenant is disabled).
    if scope.Tenant != "" {
        fmt.Printf("Tenant: %s\n", scope.Tenant)
    }

    // Database alias selected for this request.
    if scope.DatabaseAlias != "" {
        fmt.Printf("Database alias: %s\n", scope.DatabaseAlias)
    }
}
```

### Convenience helpers

```go
// Convenience helpers — each delegates to RequestScopeFromContext.
site   := app.SiteFromContext(r.Context())          // string
tenant := app.TenantFromContext(r.Context())         // string
alias  := app.DatabaseAliasFromContext(r.Context())  // string
```

---

## Production Checklist

- [ ] `multisite.enabled: true` configured with explicit site definitions
- [ ] Sites list host patterns under `hosts:` (an array, not a scalar string)
- [ ] Wildcard hosts use valid patterns (`*.example.com`)
- [ ] `multitenant.enabled: true` when tenant isolation is required
- [ ] `multitenant.require_isolated_db: true` (production default)
- [ ] All tenants have explicit database mappings or a global template configured
- [ ] Tenant resolution method chosen (`subdomain` or `header`) via `resolver:`
- [ ] Handlers call `app.TenantFromContext` / `app.SiteFromContext` (not pointer method calls)
- [ ] Database access uses `App.DatabaseForRequest(r)` (method, not package function)
- [ ] Database queries use tenant-scoped connections
- [ ] Health checks validate tenant database connectivity
