# Tutorial Detallado: Construir una App con GoFrame (MVC + API)

Este tutorial recorre el flujo completo para construir una aplicacion real con GoFrame:

1. Bootstrap del proyecto
2. Modelado de dominio
3. Migraciones y seeds
4. API REST
5. Vista HTML (enfoque MVC clasico)
6. Admin panel
7. Operacion diaria con CLI
8. Checklist para pasar a produccion

## 0) Estructura recomendada

Guia ampliada de estructura: [docs/PROJECT_LAYOUT.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/PROJECT_LAYOUT.md)

```text
miapp/
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

## 1) Configuracion del framework

Archivo `goframe.yaml`:

```yaml
database_engine: bun
database_url: sqlite://app.db
host: 0.0.0.0
port: 8080
env: development
log_level: info
log_format: text
admin_prefix: /admin
admin_title: MiApp Admin
```

Notas:

- En desarrollo, `sqlite://app.db` acelera el arranque.
- En produccion, cambia `database_url` y `env: production`.

## 2) Generar primer recurso de dominio

Genera un recurso base (modelo + handler CRUD scaffold + test + migracion):

```bash
go run ./cmd/goframe generate resource Project
```

Esto crea, entre otros:

- `models/project.go`
- `handlers/project_handler.go`
- `handlers/project_handler_test.go`
- `migrations/<timestamp>_create_projects_table.up.sql`
- `migrations/<timestamp>_create_projects_table.down.sql`

## 3) Ajustar modelo de dominio

Edita `models/project.go` para reflejar tu caso real:

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

## 4) Ajustar migracion SQL

Edita el `.up.sql` generado para igualar columnas:

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

Y el `.down.sql`:

```sql
DROP TABLE IF EXISTS projects;
```

## 5) Aplicar migraciones y cargar seed

```bash
go run ./cmd/goframe migrate --config goframe.yaml
go run ./cmd/goframe migrate --config goframe.yaml status
```

Crea `seeds/001_projects.sql`:

```sql
INSERT INTO projects (name, description, active)
VALUES ('Roadmap 2026', 'Plan principal del producto', 1);
```

Ejecuta:

```bash
go run ./cmd/goframe seed --config goframe.yaml --seeds seeds
```

## 6) Bootstrap de aplicacion y registro de modelo

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
	"miapp/models"
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
			"Title": "MiApp",
		})
	})
}
```

`templates/home.html`:

```html
<!doctype html>
<html lang="es">
  <head>
    <meta charset="utf-8" />
    <title>{{ .Title }}</title>
  </head>
  <body>
    <h1>{{ .Title }}</h1>
    <p>App MVC + API con GoFrame</p>
  </body>
</html>
```

## 7) Usuario admin y arranque

Crear admin:

```bash
go run ./cmd/goframe createuser \
  --config goframe.yaml \
  --no-input \
  --username admin \
  --email admin@example.com \
  --password supersecret123
```

Arrancar server:

```bash
go run ./cmd/server
```

Verificar:

- `http://localhost:8080/`
- `http://localhost:8080/api/health`
- `http://localhost:8080/admin`

## 8) Flujo de desarrollo diario

Comandos recomendados:

```bash
# Ver rutas efectivas
go run ./cmd/goframe routes --config goframe.yaml

# Health de dependencias
go run ./cmd/goframe health --config goframe.yaml --json

# Crear nueva migracion
go run ./cmd/goframe migrate --config goframe.yaml create add_project_owner

# Ejecutar SQL puntual
go run ./cmd/goframe shell --config goframe.yaml -c "SELECT count(*) FROM projects"
```

## 9) Guardrails de produccion

Con `env: production`, GoFrame protege acciones sensibles (`seed`, `migrate down/reset/refresh`):

- Usa `--force` en CI/CD no interactivo.
- O usa `--yes` para confirmar sin prompt.

Ejemplos:

```bash
go run ./cmd/goframe seed --config goframe.yaml --seeds seeds --force
go run ./cmd/goframe migrate --config goframe.yaml reset --force
```

## 10) Extender la CLI por proyecto

Puedes añadir comandos propios sin tocar el core creando binarios ejecutables `goframe-<nombre>` en `PATH`.

Ejemplo:

- Si existe `goframe-report` en `PATH`, entonces:

```bash
go run ./cmd/goframe report --from 2026-01-01
```

GoFrame delega automaticamente en ese comando externo.

## 11) Siguiente evolucion sugerida

- Crear carpeta `internal/repository` para aislar acceso a datos.
- Añadir tests HTTP sobre handlers API.
- Migrar de SQLite a Postgres para entornos staging/produccion.
- Incorporar pipeline CI con `go test ./...` + migraciones de smoke test.
