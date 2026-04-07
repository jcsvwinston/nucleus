# Multi-Database Modeling Guide

Reference date: 2026-04-07.
Status: Current.

This guide explains how to add models to GoFrame with a long-term, backward-compatible strategy across multiple database engines.

## 1. Stable Contract (v1 mindset)

For durability across years and framework upgrades, treat these as your public model contract:

- model structs and field names
- `db` tags (`column`, `pk`, `required`, `readonly`)
- JSON shape (`json` tags)
- admin behavior (`admin` tags)

Recommended policy:

1. Add new fields in a backward-compatible way (nullable or with defaults first).
2. Never rename/remove columns in-place without a migration bridge.
3. Deprecate first, remove later (documented in changelog + migration notes).

## 2. Two Supported Paths to Add Models

## 2.1 Code-first (new domain)

Create the model in `internal/models`:

```go
package models

import "github.com/jcsvwinston/GoFrame/pkg/model"

type Customer struct {
	model.BaseModel
	Email   string `db:"column:email;required" validate:"required,email" admin:"list,search"`
	Name    string `db:"column:name;required" admin:"list,search"`
	Active  bool   `db:"column:active" admin:"list,filter"`
	Country string `db:"column:country" admin:"list,filter"`
}
```

Register it in bootstrap:

```go
if err := a.RegisterModel(&models.Customer{}, model.ModelConfig{
	Icon:         "user",
	ListFields:   []string{"ID", "Email", "Name", "Active", "Country"},
	SearchFields: []string{"Email", "Name"},
	Filters:      []string{"Active", "Country"},
	OrderBy:      "created_at desc",
}); err != nil {
	log.Fatal(err)
}
```

Then create/apply SQL migrations:

```bash
goframe migrate --config goframe.yaml create create_customers_table
goframe migrate --config goframe.yaml up
```

## 2.2 Database-first (existing schema / legacy systems)

Use introspection:

```bash
goframe inspectdb --config goframe.yaml --tables customers,orders --output internal/models/legacy_models.go
```

Then:

1. review generated field types and tags
2. move/refactor structs into your domain modules if needed
3. register selected models with `a.RegisterModel(...)`

For geospatial schemas:

```bash
goframe ogrinspect --config goframe.yaml --output internal/models/geospatial_models.go
```

## 3. Multi-Database Configuration

Current first-class runtime URLs:

- SQLite: `sqlite://app.db`
- PostgreSQL: `postgres://...` or `postgresql://...`
- MySQL: `mysql://...`

Current exploratory runtime URLs:

- MS SQL Server: `sqlserver://...` or `mssql://...`
- Oracle: `oracle://...`

Example configs:

```yaml
# SQLite
database_default: default
databases:
  default:
    url: sqlite://app.db
```

```yaml
# PostgreSQL
database_default: default
databases:
  default:
    url: postgres://user:pass@host:5432/app?sslmode=disable
```

```yaml
# MySQL
database_default: default
databases:
  default:
    url: mysql://user:pass@host:3306/app
```

```yaml
# MS SQL Server (exploratory)
database_default: default
databases:
  default:
    url: sqlserver://sa:pass@host:1433/master
```

```yaml
# Oracle (exploratory)
database_default: default
databases:
  default:
    url: oracle://system:oracle@host:1521/FREEPDB1
```

## 4. How to Add a New Engine (Framework-Level)

When onboarding a new market database (licensed or open-source), use this checklist:

1. Runtime connection support in `pkg/db/db.go`
2. Flavor detection and SQL helper support in `internal/cli/sqlcommands.go`
3. Schema introspection in `internal/cli/inspectdb.go`
4. Type mapping for generated models in `mapInspectType(...)`
5. Identifier quoting rules in SQL helpers
6. Matrix tests:
   - `pkg/db/sql_matrix_test.go`
   - `internal/cli/sql_matrix_integration_test.go`
7. Docs update:
   - `README.md`
   - `docs/CI_MATRIX.md`
   - this guide

This keeps support explicit, testable, and maintainable across v1.x releases.

## 5. Long-Term Compatibility Rules for Teams

- Keep model field semantics stable; avoid silent behavior changes.
- Prefer additive migrations over destructive ones.
- Version data contract changes with clear migration paths.
- Use `inspectdb` regularly against legacy databases to detect drift early.
- Gate new engine support behind integration tests before declaring it first-class.

## 6. Suggested Workflow by Context

1. Greenfield product: code-first + migrations + seeds.
2. Legacy migration: inspectdb first, then incremental refactor.
3. Multi-country/enterprise rollout: per-engine CI matrix + compatibility tests before promotion.
