# GoFrame (WIP)

Framework web para Go inspirado en Django, con nucleo `chi`, enfoque modular y DX orientada a productividad.

## Estado Del Proyecto

Repositorio en desarrollo activo. A fecha de 2026-04-01:

- `go test ./...` pasa en `main`.
- Hay base funcional de `router`, `auth`, `authz`, `errors`, `validate`, `model` y `admin`.
- Hay baseline enterprise de observabilidad (`OpenTelemetry`) y hardening HTTP (`rate limit` opcional).
- Hay capa base de tareas de fondo con Asynq en `pkg/tasks` + scaffold de `cmd/worker`.
- El producto completo (MVC + REST + admin rico + CLI tipo `manage.py`) aun no esta cerrado.

## Fase 0 Cerrada (Contrato y Direccion)

Decisiones tecnicas fijadas en [SPEC.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/SPEC.md):

1. SQL oficial del framework: `Bun`.
2. Documental oficial: `mongo-driver`.
3. Cache y pub/sub: `go-redis`.
4. Admin UI objetivo: Tailwind CSS + componentes reutilizables.
5. Arquitectura polyglot por interfaces (sin "ORM universal").

Detalle operativo de Fase 0: [docs/PHASE0.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/PHASE0.md)

## Fase 2 Cerrada (SQL Bun-first)

Estado operativo en [docs/PHASE2.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/PHASE2.md):

1. Bun como engine por defecto en `pkg/db`.
2. `model` y `admin` validados sobre Bun.
3. CLI y migraciones SQL listas para flujo diario (`migrate`, `seed`, `generate resource`, guardrails de produccion).

## Fase 3 Cerrada (Admin UI + DX)

Estado operativo en [docs/PHASE3.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/PHASE3.md):

1. Slice 1 completado en `pkg/admin/ui` (layout renovado, componentes base, command palette, estados de carga/vacio).
2. Slice 2 completado (filtros por campo, ordenacion por cabeceras y export de seleccionados).
3. Slice 3 completado (libreria base de componentes + tabs/paneles de detalle).
4. Slice 4 completado (accesibilidad reforzada + errores recuperables/retries + feedback de operaciones largas).
5. Backend API del admin mantenido sin ruptura.

## Fase 4 Cerrada (Ejemplos MVC/API + Release)

Estado operativo en [docs/PHASE4.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/PHASE4.md):

1. Slice 1 completado con ejemplo runnable en [examples/mvc_api](/Users/jcsv/GolandProjects/GoFrame/GoFrame/examples/mvc_api).
2. Slice 2 completado con `goframe new` para bootstrap MVC + API + Admin.
3. Slice 3 completado con smoke E2E + checklist de release/versionado.

## Fase 5 Cerrada (Release Candidate)

Estado operativo en [docs/PHASE5.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/PHASE5.md):

1. Slice 1 completado con baseline de CI/release, changelog y versionado.
2. Slice 2 completado con publicacion multiplataforma via GoReleaser (linux/darwin/windows + amd64/arm64 + checksums).
3. Slice 3 completado con rehearsal E2E de `v0.5.0-rc1` (script local + workflow manual).

## Estado Actual vs Target

| Area | Estado Actual (main) | Target (SPEC) |
| --- | --- | --- |
| SQL layer | Bun-first (`bun` default + `gorm` compat explicita) | Bun como capa oficial |
| Admin UI | SPA embebida rica v1 (sin build) | Tailwind + UI rica |
| Registro de modelos | `model.Registry.Register(...)` | Igual, como contrato estable |
| Alta de panel admin | `admin.NewPanel(dbConn, registry, logger, cfg)` | Mantener API clara y estable |
| CLI | Baseline completa (`new`, `startapp`, `serve`, `migrate`, `sqlmigrate`, `sqlflush`, `sqlsequencereset`, `flush`, `diffsettings`, `inspectdb`, `dumpdata`, `loaddata`, `createuser`, `seed`, `shell`, `generate`, `test`, `routes`, `health`) + aliases Django-style | CLI completa tipo `manage.py` + hardening continuo |
| Background jobs | Capa base `pkg/tasks` (Asynq) + scaffold `cmd/worker` | Worker reusable por proyecto |
| Observabilidad | `slog` + OTel (traces/metrics via OTLP) | Observabilidad enterprise completa |
| Seguridad HTTP | Headers + CSRF + rate limit configurable | Guardrails por defecto y endurecimiento incremental |

## Quick Start (Bootstrap Oficial Fase 1)

Este ejemplo usa el flujo recomendado actual: `app.New(...)`, registro de modelos y `Run(...)`.

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/jcsvwinston/GoFrame/pkg/app"
	"github.com/jcsvwinston/GoFrame/pkg/model"
)

type User struct {
	model.BaseModel
	Email  string `db:"column:email;required" validate:"required,email" admin:"list,search,filter"`
	Name   string `db:"column:name;required" validate:"required" admin:"list,search"`
	Role   string `db:"column:role" admin:"list,filter"`
	Active bool   `db:"column:active" admin:"list,filter"`
}

func main() {
	a, err := app.New(&app.Config{
		Host:            "0.0.0.0",
		Port:            8080,
		DatabaseEngine:  "bun", // runtime recomendado
		DatabaseURL:     "sqlite://app.db",
		DatabaseMaxOpen: 10,
		DatabaseMaxIdle: 5,
		AdminPrefix:     "/admin",
		AdminTitle:      "GoFrame Admin",
		LogLevel:        "info",
		LogFormat:       "text",
	})
	if err != nil {
		log.Fatal(err)
	}

	// SQL schema para los modelos registrados (en proyectos reales, usar migraciones).
	sqlDB, err := a.DB.SqlDB()
	if err != nil {
		log.Fatal(err)
	}
	if _, err := sqlDB.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME,
			email TEXT NOT NULL,
			name TEXT NOT NULL,
			role TEXT,
			active BOOLEAN
		)
	`); err != nil {
		log.Fatal(err)
	}

	if err := a.RegisterModel(&User{}, model.ModelConfig{
		Icon:         "user",
		ListFields:   []string{"ID", "Email", "Name", "Role", "Active"},
		SearchFields: []string{"Email", "Name"},
		Filters:      []string{"Role", "Active"},
		OrderBy:      "created_at desc",
	}); err != nil {
		log.Fatal(err)
	}

	a.Router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Println("server: http://localhost:8080")
	log.Println("admin:  http://localhost:8080/admin")
	log.Fatal(a.Run(context.Background()))
}
```

## Alcance Implementado Hoy

- `pkg/router`: wrapper sobre chi + middleware base + helpers de render.
- `pkg/router`: middleware de seguridad (`CSRF`, `SecurityHeaders`), `rate limit` opcional y telemetry middleware OTel.
- `pkg/auth`: JWT, sesiones y password hashing.
- `pkg/authz`: Casbin wrapper y middleware.
- `pkg/db`: Bun-first + compatibilidad GORM explicita + migrador SQL por archivos (`Create`, `Up`, `Down`, `Steps`, `Status`).
- `pkg/model`: metadatos por reflexion, registry y CRUD generico.
- `pkg/admin`: panel embebido con CRUD, schema, export CSV y bulk delete.
- `pkg/tasks`: enqueue/worker runtime basado en Asynq para tareas asíncronas.
- `cmd/goframe` + `internal/cli`: CLI modular con comandos `new`, `startapp`, `serve`, `migrate`, `sqlmigrate`, `sqlflush`, `sqlsequencereset`, `flush`, `diffsettings`, `makemessages`, `compilemessages`, `optimizemigration`, `squashmigrations`, `sendtestemail`, `inspectdb`, `dumpdata`, `loaddata`, `createcachetable`, `createuser`, `changepassword`, `clearsessions`, `seed`, `shell`, `generate`, `test`, `testserver`, `routes`, `health`.
- `pkg/errors`, `pkg/validate`, `pkg/observe` (incluye OTel), `pkg/signals`.

## CLI (Baseline Completa)

Comandos disponibles en el binario `goframe`:

```bash
go run ./cmd/goframe help
go run ./cmd/goframe new myapp --module github.com/acme/myapp --template mvc
go run ./cmd/goframe startapp billing --out .
go run ./cmd/goframe serve --config goframe.yaml
go run ./cmd/goframe routes --config goframe.yaml
go run ./cmd/goframe health --config goframe.yaml

go run ./cmd/goframe migrate --config goframe.yaml --migrations migrations create create_users
go run ./cmd/goframe migrate --config goframe.yaml --migrations migrations
go run ./cmd/goframe migrate --config goframe.yaml --migrations migrations status
go run ./cmd/goframe migrate --config goframe.yaml --migrations migrations down 1
go run ./cmd/goframe migrate --config goframe.yaml --migrations migrations steps -1
go run ./cmd/goframe migrate --config goframe.yaml --force reset
go run ./cmd/goframe migrate --config goframe.yaml --force refresh
go run ./cmd/goframe sqlmigrate --migrations migrations 20260401120000_create_users
go run ./cmd/goframe sqlmigrate --migrations migrations --down 20260401120000_create_users
go run ./cmd/goframe sqlflush --config goframe.yaml
go run ./cmd/goframe sqlsequencereset --config goframe.yaml
go run ./cmd/goframe flush --config goframe.yaml --yes
go run ./cmd/goframe diffsettings --config goframe.yaml
go run ./cmd/goframe createcachetable --config goframe.yaml
go run ./cmd/goframe makemessages --config goframe.yaml --locale es --input .
go run ./cmd/goframe compilemessages --config goframe.yaml --locale es
go run ./cmd/goframe optimizemigration --migrations migrations add_users_table
go run ./cmd/goframe squashmigrations --migrations migrations --from init --to add_users --name baseline --write
go run ./cmd/goframe sendtestemail --config goframe.yaml --to dev@example.com --dry-run
go run ./cmd/goframe inspectdb --config goframe.yaml --output internal/models/inspected.go
go run ./cmd/goframe dumpdata --config goframe.yaml --output fixtures.json
go run ./cmd/goframe loaddata --config goframe.yaml fixtures.json

go run ./cmd/goframe generate model User
go run ./cmd/goframe generate handler User
go run ./cmd/goframe generate migration add_users_table
go run ./cmd/goframe generate resource Project

go run ./cmd/goframe seed --config goframe.yaml --seeds seeds
go run ./cmd/goframe seed --config goframe.yaml --seeds seeds --force
go run ./cmd/goframe createuser --config goframe.yaml --no-input --username admin --email admin@example.com --password supersecret123
go run ./cmd/goframe changepassword admin --config goframe.yaml --password newsecret123 --no-input
go run ./cmd/goframe clearsessions --config goframe.yaml
go run ./cmd/goframe shell --config goframe.yaml -c \"SELECT 1\"
go run ./cmd/goframe shell --config goframe.yaml --sandbox -c \"SELECT count(*) FROM users\"
go run ./cmd/goframe test --run TestRun_MigrateLifecycle ./cmd/goframe
go run ./cmd/goframe testserver --config goframe.yaml --dry-run fixtures.json

# aliases estilo Django
go run ./cmd/goframe runserver 0.0.0.0:8080
go run ./cmd/goframe startproject myapp --module github.com/acme/myapp
go run ./cmd/goframe makemigrations add_users_table
go run ./cmd/goframe showmigrations --config goframe.yaml
go run ./cmd/goframe createsuperuser --config goframe.yaml --no-input --username admin --email admin@example.com --password supersecret123
go run ./cmd/goframe dbshell --config goframe.yaml -c \"SELECT 1\"
go run ./cmd/goframe check --config goframe.yaml
go run ./cmd/goframe check --deploy --config goframe.yaml
```

Scaffold actualizado de `goframe new`:

- genera `cmd/worker/main.go` y `internal/tasks/article_events.go`
- incluye `redis_url`, `otlp_endpoint` y `rate_limit_*` en `goframe.yaml`
- permite ejecutar worker de fondo con `go run ./cmd/worker` en el proyecto generado

Guia de decisiones y checklist de buenas practicas de CLI: [docs/CLI_BEST_PRACTICES.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/CLI_BEST_PRACTICES.md)

Comparativa de comandos GoFrame vs Django 6.0: [docs/CLI_DJANGO_PARITY.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/CLI_DJANGO_PARITY.md)

## Guias De Desarrollo

- Manual completo de desarrollo (end-to-end): [docs/MANUAL_DESARROLLADOR.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/MANUAL_DESARROLLADOR.md)
- Quickstart: [docs/QUICKSTART.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/QUICKSTART.md)
- Tutorial detallado (MVC + API): [docs/TUTORIAL_DETALLADO.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/TUTORIAL_DETALLADO.md)
- Estructura de proyecto (controllers/models/templates/etc): [docs/PROJECT_LAYOUT.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/PROJECT_LAYOUT.md)
- Ejemplo runnable MVC + API + Admin: [examples/mvc_api](/Users/jcsv/GolandProjects/GoFrame/GoFrame/examples/mvc_api)
- Checklist release/versionado: [docs/RELEASE_CHECKLIST.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/RELEASE_CHECKLIST.md)
- Estrategia de versionado: [docs/VERSIONING.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/VERSIONING.md)
- Politica de version de Go: [docs/GO_VERSION_POLICY.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/GO_VERSION_POLICY.md)
- Hoja de ruta enterprise y estado de alineacion: [docs/ENTERPRISE_ROADMAP.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/ENTERPRISE_ROADMAP.md)
- Paridad de comandos CLI contra Django: [docs/CLI_DJANGO_PARITY.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/CLI_DJANGO_PARITY.md)
- Changelog: [CHANGELOG.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/CHANGELOG.md)

## Nota De Transicion

La capa SQL opera en modo Bun-first y mantiene `gorm` como via de compatibilidad temporal (opt-in), fuera del camino recomendado para nuevos proyectos.

## Roadmap De Alto Nivel

1. Fase 0: contrato, decisiones y alineacion documental.
2. Fase 1: nucleo `App` y lifecycle completo.
3. Fase 2: capa SQL consolidada en Bun + migraciones + capa documental.
4. Fase 3: admin Tailwind rico + hardening de UX/DX.
5. Fase 4: ejemplos MVC/API y release.
6. Fase 5: release candidate y automatizacion de publicacion (cerrada).

## Licencia

MIT
