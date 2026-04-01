# Estructura Recomendada De Proyecto

Esta es la estructura recomendada para aplicaciones GoFrame que mezclan MVC + API REST:

```text
miapp/
  cmd/
    server/
      main.go
    worker/
      main.go
  internal/
    controllers/         # Controladores HTTP (MVC y API)
    models/              # Entidades de dominio (model.BaseModel + tags db/admin/validate)
    services/            # Casos de uso y logica de negocio
    repositories/        # Acceso a datos SQL/NoSQL
    tasks/               # Handlers de tareas asynq por dominio
    web/
      templates/         # html/template (vistas MVC)
      static/            # CSS/JS/imagenes
  migrations/            # Archivos SQL versionados (*.up.sql / *.down.sql)
  seeds/                 # Seeds SQL para datos iniciales
  goframe.yaml           # Configuracion del framework
  go.mod
```

## Donde va cada cosa

- `controllers`: handlers HTTP y montaje de rutas (`/api/...`, paginas MVC, etc).
- `models`: estructuras de dominio usadas por registro de modelos/admin.
- `templates`: vistas HTML de la capa MVC.
- `migrations`: cambios de schema versionados.
- `seeds`: insercion de datos base para desarrollo/testing.
- `services`: reglas de negocio (idealmente sin dependencias HTTP).
- `repositories`: persistencia (Bun SQL, Mongo, Redis, etc. segun el modulo).
- `tasks`: procesamiento async (colas Redis/Asynq) y workers por tipo de evento.

## Convenciones practicas

- API REST:
  - `internal/controllers/<recurso>_api.go`
  - rutas bajo `/api/v1/...`
- MVC:
  - `internal/controllers/<modulo>_page.go`
  - plantillas en `internal/web/templates/<modulo>/...`
- Codegen CLI:
  - `goframe generate resource <Name>` genera por defecto en `models/`, `handlers/` y `migrations/`.
  - En proyectos reales puedes mover ese scaffold a `internal/controllers` / `internal/models` manteniendo el mismo contenido.
- Worker:
  - `cmd/worker/main.go` arranca procesos de cola.
  - handlers en `internal/tasks/*.go`.

## Minimo para arrancar

1. `cmd/server/main.go`
2. `cmd/worker/main.go` (si usas background jobs)
3. `goframe.yaml`
4. `migrations/` con al menos una migracion
5. `internal/models/` con tus entidades
6. `internal/controllers/` con rutas API/MVC
