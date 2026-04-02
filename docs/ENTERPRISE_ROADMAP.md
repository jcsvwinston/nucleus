# Hoja De Ruta Enterprise (Estado Actual)

Fecha de referencia: 2026-04-02.

Esta hoja resume el plan de alineacion enterprise acordado para avanzar sin romper ritmo de entrega.

## Objetivo

Cerrar un baseline enterprise incremental sobre el core actual:

1. Worker + colas para tareas de fondo.
2. Observabilidad OpenTelemetry real (traces y metrics).
3. Hardening HTTP con rate limiting configurable.

## Cerrado En Esta Iteracion

- [x] Capa de tareas asíncronas en `pkg/tasks` (Asynq):
  - manager de worker/enqueue
  - utilidades JSON task payload
  - parsing de `redis_url` y defaults operativos
- [x] Scaffold `goframe new` extendido con:
  - `cmd/worker/main.go`
  - `internal/tasks/article_events.go`
  - `redis_url` en `goframe.yaml`
- [x] OpenTelemetry en core:
  - bootstrap global en `pkg/observe/otel.go`
  - shutdown limpio de providers desde `app.New` / lifecycle
  - middleware HTTP con spans y métricas por request
- [x] Rate limiting:
  - middleware fijo por ventana en `pkg/router/ratelimit.go`
  - key por `user_id` (si existe) o IP
  - configuración vía `rate_limit_requests` + `rate_limit_window`
- [x] Validación:
  - tests unitarios y de integración actualizados
  - `go test ./...` en verde
- [x] Tramo de paridad CLI con Django:
  - aliases Django-style (`runserver`, `makemigrations`, `showmigrations`, `createsuperuser`, `dbshell`, `check`)
  - comandos nuevos `startapp`, `test`, `testserver`, `sqlmigrate`, `sqlflush`, `sqlsequencereset`, `flush`, `diffsettings`, `createcachetable`, `makemessages`, `compilemessages`, `optimizemigration`, `squashmigrations`, `inspectdb`, `dumpdata`, `loaddata`, `changepassword`, `clearsessions`
  - matriz de paridad documentada en `docs/CLI_DJANGO_PARITY.md`

## Estado De Alineacion (Resumen)

- Chi + Bun: alineado.
- Logging estructurado (`slog`): alineado.
- Validación DTO (`validator`): alineado.
- RBAC Casbin + JWT/sesiones: alineado.
- Worker/colas: baseline alineado (Asynq).
- OpenTelemetry: baseline alineado.
- Rate limiting: baseline alineado.
- `templ + htmx`: pendiente (se mantiene stack actual sin ruptura).
- Migración total de estructura a `internal/api/service/repository/tasks/auth`: parcial y progresiva.
- CLI en Cobra: no alineado a propósito (se mantiene `flag` por contrato actual del SPEC).

## Pendientes (Siguiente Tramo)

1. Instrumentación OTel de DB/colas con atributos semánticos por operación.
2. Política de rate limiting avanzada (burst, por ruta y por rol).
3. Sanitización de input/render en endpoints sensibles (hardening XSS específico).
4. Templates `templ + htmx` como alternativa opt-in de scaffold.
5. Tests en matriz SQL ampliada (PostgreSQL/MySQL en CI para CLI y ejemplos).

## Criterio De Salida De Esta Fase

- El core queda estable y backward-compatible.
- El scaffold nuevo trae server + worker listos.
- La observabilidad y seguridad HTTP no se activan de forma disruptiva por defecto.
- La documentación y changelog reflejan los cambios.
