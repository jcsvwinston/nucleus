# Manual Del Desarrollador Final De GoFrame

Fecha de referencia: 2026-03-31.

Este documento es la guia principal para construir, operar y desplegar aplicaciones con GoFrame.

## 1. Objetivo

GoFrame es un framework web para Go inspirado en Django, con foco en:

- aplicaciones MVC + API REST
- admin panel integrado
- CLI operativa para ciclo de vida (scaffold, migraciones, seed, inspeccion)
- base SQL Bun-first con contrato de modelo estable

## 2. Alcance Real (Estado Actual)

A dia de hoy, GoFrame incluye de forma funcional:

- `pkg/app`: contenedor de aplicacion (config, logger, router, DB, admin, lifecycle)
- `pkg/db`: conexion SQL (Bun-first, compat GORM), health y migraciones SQL por archivos
- `pkg/model`: registro de modelos, metadata por reflexion, CRUD generico, hooks
- `pkg/admin`: admin panel embebido (SPA + API CRUD)
- `pkg/tasks`: capa base para tareas asíncronas con Asynq
- `pkg/observe`: logging estructurado + bootstrap OpenTelemetry (traces/metrics OTLP)
- `pkg/router`: guardrails HTTP (`CSRF`, security headers, rate limiting configurable)
- `cmd/goframe`: CLI modular
- ejemplo oficial runnable: `examples/mvc_api`

Documentos complementarios:

- `docs/QUICKSTART.md`
- `docs/TUTORIAL_DETALLADO.md`
- `docs/PROJECT_LAYOUT.md`
- `docs/RELEASE_CHECKLIST.md`

## 3. Requisitos

## 3.1 Runtime y toolchain

- Go minimo de compatibilidad: `1.23`
- Go recomendado para desarrollo/release: `1.26.x`
- Node.js: requerido para checks de sintaxis del admin UI en CI/rehearsal

Politica completa:

- `docs/GO_VERSION_POLICY.md`

## 3.2 Base de datos

Motores SQL soportados por URL:

- SQLite: `sqlite://app.db` (o `:memory:`)
- PostgreSQL: `postgres://...` o `postgresql://...`
- MySQL: `mysql://...`

## 4. Instalacion De La CLI

Hay dos caminos recomendados.

## 4.1 Desde binarios de release (recomendado)

Descarga desde releases oficiales:

- `https://github.com/jcsvwinston/GoFrame/releases`

Assets por release:

- `goframe_<version>_linux_amd64.tar.gz`
- `goframe_<version>_linux_arm64.tar.gz`
- `goframe_<version>_darwin_amd64.tar.gz`
- `goframe_<version>_darwin_arm64.tar.gz`
- `goframe_<version>_windows_amd64.zip`
- `goframe_<version>_windows_arm64.zip`
- `checksums.txt`

Validacion recomendada:

1. verificar checksum
2. ejecutar `goframe version`

## 4.2 Desde codigo fuente

```bash
git clone https://github.com/jcsvwinston/GoFrame.git
cd GoFrame
go build -o goframe ./cmd/goframe
./goframe version
```

## 4.3 Nota sobre import path canonico

Actualmente, `go.mod` declara:

- `module github.com/jcsvwinston/GoFrame`

Si vas a consumir el framework como dependencia en un proyecto externo, mantente alineado con ese path de modulo en imports y scripts hasta cerrar la migracion de modulo canonico.

## 5. Primer Proyecto En 5 Minutos

## 5.1 Generar scaffold

```bash
goframe new miapp --module example.com/miapp --out . --port 8080 --template mvc
cd miapp
go mod tidy
```

Estructura generada principal:

- `cmd/server/main.go`
- `cmd/worker/main.go`
- `internal/models/article.go`
- `internal/controllers/home_page.go`
- `internal/controllers/article_api.go`
- `internal/tasks/article_events.go`
- `internal/web/templates/home.html`
- `migrations/000001_create_articles.up.sql`
- `migrations/000001_create_articles.down.sql`
- `seeds/001_articles.sql`
- `goframe.yaml`

## 5.2 Levantar la app

```bash
go run ./cmd/server
go run ./cmd/worker
```

Accesos por defecto:

- `http://localhost:8080/`
- `http://localhost:8080/api/health`
- `http://localhost:8080/api/articles`
- `http://localhost:8080/admin`

## 6. Arquitectura De Aplicacion

## 6.1 Contenedor `app.App`

Creacion:

```go
cfg, _ := app.LoadConfig("goframe.yaml")
a, _ := app.New(cfg)
```

`App` expone:

- `Config`
- `Logger`
- `Router`
- `DB`
- `Models`
- `Admin`

Funciones clave:

- `RegisterModel(...)`
- `MountAdmin()`
- `Run(ctx)`
- `Shutdown(ctx)`
- `OnShutdown(fn)`

## 6.2 Router y lifecycle

- Router base sobre `chi`
- `Run` arranca HTTP server
- parada limpia por contexto o `SIGINT/SIGTERM`

## 6.3 Admin auto-montado

`app.New` monta el admin automaticamente en `admin_prefix` (default `/admin`).

## 7. Configuracion (`goframe.yaml`)

Ejemplo minimo:

```yaml
database_engine: bun
database_url: sqlite://app.db
redis_url: redis://127.0.0.1:6379/0
host: 0.0.0.0
port: 8080
env: development
log_level: info
log_format: text
otlp_endpoint: ""
rate_limit_requests: 0
rate_limit_window: 1m
admin_prefix: /admin
admin_title: Mi Admin
```

Campos frecuentes:

- server: `host`, `port`, `read_timeout`, `write_timeout`, `idle_timeout`
- database: `database_engine`, `database_url`, `database_max_open`, `database_max_idle`, `database_max_lifetime`
- queue/background: `redis_url`
- auth/session: `jwt_secret`, `jwt_expiry`, `session_lifetime`
- admin: `admin_prefix`, `admin_title`
- observabilidad: `log_level`, `log_format`, `metrics_path`, `otlp_endpoint`
- hardening HTTP: `rate_limit_requests`, `rate_limit_window`
- entorno: `env`, `debug`

Soporte de entorno por prefijo `GOFRAME_`.
Ejemplo:

- `GOFRAME_PORT=9090`
- `GOFRAME_DATABASE_URL=postgres://...`

## 8. Modelos

## 8.1 BaseModel

Embed recomendado:

```go
type Project struct {
    model.BaseModel
    Name string
}
```

## 8.2 Tags soportados

`db` / `gorm` (metadata de almacenamiento):

- `column:<name>`
- `primaryKey` / `pk`
- `required`
- `readonly`

`validate`:

- `required` (detecta requerido para metadata)

`admin`:

- `list`
- `search`
- `filter`
- `readonly`
- `exclude`
- `label:<texto>`
- `choices:valor|Label;valor2|Label2`

Ejemplo completo:

```go
type User struct {
    model.BaseModel
    Email string `db:"column:email;required" validate:"required,email" admin:"list,search"`
    Role  string `db:"column:role" admin:"list,filter,choices:admin|Admin;user|User"`
    Bio   string `db:"column:bio" admin:"label:Biografia"`
}
```

## 8.3 Registro de modelos

```go
err := a.RegisterModel(&User{}, model.ModelConfig{
    Icon:         "user",
    ListFields:   []string{"ID", "Email", "Role"},
    SearchFields: []string{"Email"},
    Filters:      []string{"Role"},
    OrderBy:      "created_at desc",
    PageSize:     25,
    ReadOnly:     false,
})
```

## 9. MVC + API

En GoFrame, MVC y API conviven en el mismo router.

Ejemplo:

```go
a.Router.Get("/", controllers.HomePage(tpl))
a.Router.Get("/api/health", controllers.Health)
a.Router.Get("/api/articles", controllers.ListArticles(sqlDB))
a.Router.Post("/api/articles", controllers.CreateArticle(sqlDB))
```

Recomendacion:

- mantener handlers HTTP en `internal/controllers` o `handlers`
- mantener logica de negocio fuera del handler

## 10. Admin Panel

## 10.1 Alta

El panel se crea y monta desde `app.New`.

Configuracion:

- `admin_prefix` (default `/admin`)
- `admin_title`

## 10.2 Capacidades

- listado de modelos
- schema por modelo
- CRUD
- filtros y busqueda
- export CSV
- acciones bulk

## 10.3 API admin

Rutas principales:

- `GET /admin/api/models`
- `GET /admin/api/models/{name}/schema`
- `GET /admin/api/models/{name}`
- `POST /admin/api/models/{name}`
- `GET /admin/api/models/{name}/{id}`
- `PUT /admin/api/models/{name}/{id}`
- `DELETE /admin/api/models/{name}/{id}`
- `POST /admin/api/models/{name}/bulk`
- `GET /admin/api/models/{name}/export`

## 11. Migraciones SQL

Comando raiz:

```bash
goframe migrate [flags] [accion]
```

Flags:

- `--config <path>`
- `--migrations <dir>` (default `migrations`)
- `--force`
- `--yes`

Acciones:

- `up [n]`
- `down [n]`
- `steps <n>`
- `status`
- `create <name>`
- `reset`
- `refresh`

Ejemplos:

```bash
goframe migrate --config goframe.yaml create add_project_owner
goframe migrate --config goframe.yaml
goframe migrate --config goframe.yaml status
goframe migrate --config goframe.yaml down 1
goframe migrate --config goframe.yaml steps -1
goframe migrate --config goframe.yaml --force reset
```

## 12. Seeds

Comando:

```bash
goframe seed --config goframe.yaml --seeds seeds
```

Flags:

- `--file <seed.sql>`
- `--dry-run`
- `--force`
- `--yes`

Ejemplos:

```bash
goframe seed --config goframe.yaml --seeds seeds --dry-run
goframe seed --config goframe.yaml --seeds seeds --file 001_users.sql
goframe seed --config goframe.yaml --seeds seeds --force
```

## 13. Creacion De Usuario Admin

```bash
goframe createuser --config goframe.yaml \
  --username admin \
  --email admin@example.com \
  --password supersecret123 \
  --superuser=true \
  --no-input
```

Notas:

- si no usas `--no-input`, pide datos interactivos
- valida username/email/password
- crea o actualiza usuario existente por username/email

Cambiar password de un admin existente:

```bash
goframe changepassword admin --config goframe.yaml --password newsecret123 --no-input
```

## 14. Shell SQL

Ejecucion puntual:

```bash
goframe shell --config goframe.yaml -c "SELECT count(*) FROM users"
goframe shell --config goframe.yaml --sandbox -c "SELECT count(*) FROM users"
```

Modo interactivo:

```bash
goframe shell --config goframe.yaml
```

Soporta entrada por `stdin` (scripts SQL).
Con `--sandbox`, limita la ejecucion a sentencias SQL de solo lectura.

## 14.1 Tareas De Fondo (Asynq)

El scaffold `goframe new` genera:

- `cmd/worker/main.go`: entrypoint de worker
- `internal/tasks/article_events.go`: ejemplo de registro de handlers

Arranque:

```bash
go run ./cmd/worker
```

Requiere `redis_url` configurado en `goframe.yaml`.

## 15. Generadores (`generate`)

```bash
goframe generate model User
goframe generate handler User
goframe generate migration add_users_table
goframe generate resource Project
```

Flags:

- `--out <dir>`
- `--force`
- `--migrations <dir>`

`resource` crea:

- modelo
- handler CRUD scaffold
- test de handler
- migracion up/down

## 16. Comandos De Diagnostico

## 16.1 routes

```bash
goframe routes --config goframe.yaml
goframe routes --config goframe.yaml --json
goframe routes --config goframe.yaml --path /api --verbose
```

## 16.2 health

```bash
goframe health --config goframe.yaml
goframe health --config goframe.yaml --json --timeout 5s
```

## 17. Guardrails En Produccion

Cuando `env: production`:

- operaciones destructivas (`migrate down/reset/refresh`, `seed`) requieren confirmacion
- en CI no interactivo usa `--force` o `--yes`

Recomendacion CI/CD:

- usar `--force` explicitamente
- ejecutar siempre `health` y smoke test post deploy

## 18. Flujo Recomendado De Desarrollo

1. generar modulo con `goframe new`
2. definir modelos y tags
3. registrar modelos en `main`
4. ajustar migraciones SQL
5. ejecutar `migrate` + `seed`
6. crear usuario admin
7. desarrollar handlers MVC/API
8. revisar rutas y health
9. automatizar pruebas

## 19. Pruebas

Local:

```bash
go test ./...
```

Smoke oficial del ejemplo:

```bash
go test ./examples/mvc_api -run TestExampleMVCAPIAdmin_Smoke -v
```

Rehearsal de release:

```bash
./scripts/release/rehearse_rc.sh
```

## 20. Despliegue Real

## 20.1 Estrategia recomendada

1. descargar binario release para el SO/arquitectura objetivo
2. verificar `checksums.txt`
3. provisionar `goframe.yaml`
4. ejecutar binario de tu app (`cmd/server`) o servicio equivalente

## 20.2 Health post-deploy

Validar:

- endpoint de app (`/`)
- endpoint API (`/api/health`)
- admin (`/admin`)
- comando `goframe health --json`

## 20.3 Artefactos de release

Cada release publica:

- 6 binarios empaquetados (3 SO x 2 arquitecturas)
- checksum SHA256

## 21. Troubleshooting

## 21.1 `go install` falla por module path

Si aparece error de mismatch de modulo, revisa `module` en `go.mod` y usa temporalmente:

- binarios de release, o
- build local desde repo

## 21.2 `admin` sin modelos

Causa comun:

- no se llamo `RegisterModel(...)`

## 21.3 `migrate` no aplica cambios

Revisar:

- directorio `migrations`
- archivos `.up.sql` / `.down.sql`
- estado: `goframe migrate status`

## 21.4 `health` devuelve degraded

Revisar:

- `database_url`
- conectividad
- credenciales

## 22. Referencia Rapida De Comandos

```bash
goframe help
goframe version
goframe new <name> [--module ...] [--out ...] [--port ...] [--template mvc] [--force]
goframe startapp <name> [--out ...] [--migrations ...] [--skip-migration] [--force]
goframe serve [--config ...] [--host ...] [--port ...]
goframe migrate [--config ...] [--migrations ...] [--force] [--yes] [accion]
goframe sqlmigrate [--migrations ...] [--down] <migration_id_or_name>
goframe sqlflush [--config ...]
goframe sqlsequencereset [--config ...] [tables...]
goframe flush [--config ...] [--force] [--yes] [--dry-run]
goframe diffsettings [--config ...] [--all] [--json]
goframe inspectdb [--config ...] [--tables users,posts] [--exclude ...] [--package models] [--output internal/models/inspected.go]
goframe dumpdata [--config ...] [--tables users,posts] [--exclude ...] [--output fixtures.json]
goframe loaddata [--config ...] [--tables users] [--truncate] [--dry-run] [--force] [--yes] <fixture.json>
goframe seed [--config ...] [--seeds ...] [--file ...] [--dry-run] [--force] [--yes]
goframe createuser [--config ...] [--username ...] [--email ...] [--password ...] [--superuser] [--no-input]
goframe changepassword [--config ...] [--username ...] [--password ...] [--no-input] <username>
goframe shell [--config ...] [--command ...|-c ...] [--timeout 10s] [--sandbox]
goframe generate [--out ...] [--migrations ...] [--force] <model|handler|migration|resource> <name>
goframe test [--run ...] [--count 1] [--race] [--v] [--failfast] [--cover] [--timeout ...] [--dry-run] [packages...]
goframe testserver [--config ...] [--fixture ...] [--tables users] [--truncate] [--dry-run] [--host ...] [--port ...] <fixture.json>
goframe routes [--config ...] [--path ...] [--json] [--verbose]
goframe health [--config ...] [--timeout 3s] [--json] [--deploy]
```

Aliases estilo Django:

```bash
goframe runserver [addr:port]
goframe startproject <name> [flags de new]
goframe makemigrations <name>
goframe showmigrations [--config ...] [--migrations ...]
goframe createsuperuser [flags de createuser]
goframe dbshell [flags de shell]
goframe check [flags de health]             # alias de health
goframe check --deploy [--config ...]       # hardening checks de despliegue
```

En proyectos generados con `goframe new`, tambien dispones de:

```bash
go run ./cmd/server
go run ./cmd/worker
```

## 23. Siguiente Lectura

- onboarding rapido: `docs/QUICKSTART.md`
- tutorial paso a paso: `docs/TUTORIAL_DETALLADO.md`
- layout recomendado: `docs/PROJECT_LAYOUT.md`
- buenas practicas CLI: `docs/CLI_BEST_PRACTICES.md`
- paridad CLI contra Django: `docs/CLI_DJANGO_PARITY.md`
- hoja de ruta enterprise: `docs/ENTERPRISE_ROADMAP.md`
- checklist release: `docs/RELEASE_CHECKLIST.md`
