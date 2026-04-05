# Detailed Tutorial: Build an App with GoFrame (MVC + API)

Reference date: 2026-04-05.
Status: Current.

This tutorial walks through the full flow to build a real app with GoFrame:

1. Project bootstrap
2. Domain modeling
3. Migrations and seeds
4. REST API
5. HTML view (classic MVC approach)
6. Admin panel
7. Daily CLI operations
8. Production readiness checklist

## 0) Recommended structure

Expanded structure guide: [PROJECT_LAYOUT.md](PROJECT_LAYOUT.md)

```text
myapp/
  cmd/
    server/
      main.go
  handlers/
  models/
  migrations/
  seeds/
  templates/
    home.html
  goframe.yaml
```

## 1) Framework configuration

`goframe.yaml`:

```yaml
database_engine: bun
database_url: sqlite://app.db
host: 0.0.0.0
port: 8080
env: development
log_level: info
log_format: text
admin_prefix: /admin
admin_title: MyApp Admin
```

Notes:

- In development, `sqlite://app.db` speeds up bootstrapping.
- In production, update `database_url` and set `env: production`.

## 2) Generate your first domain resource

Generate a base resource (model + scaffold CRUD handler + test + migration):

```bash
go run ./cmd/goframe generate resource Project
```

This creates, among others:

- `models/project.go`
- `handlers/project_handler.go`
- `handlers/project_handler_test.go`
- `migrations/<timestamp>_create_projects_table.up.sql`
- `migrations/<timestamp>_create_projects_table.down.sql`

## 3) Adjust your domain model

Edit `models/project.go` to match your real use case:

```go
package models

import "github.com/jcsvwinston/GoFrame/pkg/model"

type Project struct {
	model.BaseModel
	Name        string `db:"name" validate:"required" admin:"list,search"`
	Description string `db:"description" admin:"list"`
	Active      bool   `db:"active" admin:"list,filter"`
}
```

## 4) Adjust SQL migration

Edit the generated `.up.sql` so columns match your model:

```sql
CREATE TABLE IF NOT EXISTS projects (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at DATETIME,
  updated_at DATETIME,
  deleted_at DATETIME,
  name TEXT NOT NULL,
  description TEXT,
  active BOOLEAN NOT NULL DEFAULT 1
);
```

And `.down.sql`:

```sql
DROP TABLE IF EXISTS projects;
```

## 5) Apply migrations and load seed data

```bash
go run ./cmd/goframe migrate --config goframe.yaml
go run ./cmd/goframe migrate --config goframe.yaml status
```

Create `seeds/001_projects.sql`:

```sql
INSERT INTO projects (name, description, active)
VALUES ('Roadmap 2026', 'Main product plan', 1);
```

Run:

```bash
go run ./cmd/goframe seed --config goframe.yaml --seeds seeds
```

## 6) App bootstrap and model registration

`cmd/server/main.go`:

```go
package main

import (
	"context"
	"html/template"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jcsvwinston/GoFrame/pkg/app"
	"github.com/jcsvwinston/GoFrame/pkg/model"
	"myapp/models"
)

func main() {
	cfg, err := app.LoadConfig("goframe.yaml")
	if err != nil {
		log.Fatal(err)
	}

	a, err := app.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	if err := a.RegisterModel(&models.Project{}, model.ModelConfig{
		ListFields:   []string{"ID", "Name", "Description", "Active"},
		SearchFields: []string{"Name", "Description"},
		Filters:      []string{"Active"},
		OrderBy:      "created_at desc",
	}); err != nil {
		log.Fatal(err)
	}

	registerAPIRoutes(a.Router)
	registerMVCRoutes(a.Router)

	log.Println("server: http://localhost:8080")
	log.Println("admin:  http://localhost:8080/admin")
	log.Fatal(a.Run(context.Background()))
}

func registerAPIRoutes(r chi.Router) {
	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
}

func registerMVCRoutes(r chi.Router) {
	tpl := template.Must(template.ParseFiles("templates/home.html"))
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		_ = tpl.Execute(w, map[string]any{
			"Title": "MyApp",
		})
	})
}
```

`templates/home.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>{{ .Title }}</title>
  </head>
  <body>
    <h1>{{ .Title }}</h1>
    <p>MVC + API app with GoFrame</p>
  </body>
</html>
```

## 7) Admin user and startup

Create admin:

```bash
go run ./cmd/goframe createuser \
  --config goframe.yaml \
  --no-input \
  --username admin \
  --email admin@example.com \
  --password supersecret123
```

Start server:

```bash
go run ./cmd/server
```

Verify:

- `http://localhost:8080/`
- `http://localhost:8080/api/health`
- `http://localhost:8080/admin`

## 8) Daily development workflow

Recommended commands:

```bash
# Show effective routes
go run ./cmd/goframe routes --config goframe.yaml

# Dependency health check
go run ./cmd/goframe health --config goframe.yaml --json

# Create a new migration
go run ./cmd/goframe migrate --config goframe.yaml create add_project_owner

# Run an ad-hoc SQL query
go run ./cmd/goframe shell --config goframe.yaml -c "SELECT count(*) FROM projects"
```

## 9) Production guardrails

With `env: production`, GoFrame protects sensitive actions (`seed`, `migrate down/reset/refresh`):

- Use `--force` in non-interactive CI/CD.
- Or use `--yes` to confirm without a prompt.

Examples:

```bash
go run ./cmd/goframe seed --config goframe.yaml --seeds seeds --force
go run ./cmd/goframe migrate --config goframe.yaml reset --force
```

## 10) Extend CLI per project

You can add custom commands without touching core by creating executables named `goframe-<name>` in `PATH`.

Example:

- If `goframe-report` exists in `PATH`, then:

```bash
go run ./cmd/goframe report --from 2026-01-01
```

GoFrame automatically delegates to that external command.

## 11) Suggested next evolution

- Create `internal/repository` to isolate data access.
- Add HTTP tests for API handlers.
- Migrate from SQLite to PostgreSQL for staging/production.
- Add CI pipeline with `go test ./...` + migration smoke tests.
