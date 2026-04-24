# Agent Handoff — GoFrame Modularization

> Documento para que cualquier agente (humano o IA) pueda reanudar el trabajo.
> Última actualización: 2026-04-24.

## Contexto rápido

GoFrame es un framework Go full-stack (tipo Django/Rails). El trabajo reciente
ha sido **modularizar** el framework para que los proyectos generados sean
autónomos, ligeros, y no necesiten el binario GoFrame ni su código fuente.

## Estado de verdad (2026-04-24)

**✅ Fases 1-3 completas. Todos los tests pasan. Phase 4 diferida a post-v1.**

Consultar `docs/BREADCRUMB.md` para el estado detallado con archivos modificados.

### Lo que se logró

| Fase | Qué hace | Cómo verificar |
|------|----------|----------------|
| **1** | `goframe new` genera `go.mod` con versión publicada | `go test -run TestRun_New ./cmd/goframe/...` |
| **2** | MSSQL/Oracle bajo build tags (opt-in) | `go build -tags "mssql,oracle" ./...` |
| **3** | `app.New(cfg, ...Option)` composable + `--template api` | `go test ./pkg/app/...` |

### Lo que quedó pendiente (no bloqueante)

- `README.md` no actualizado con messaging de modularidad
- Phase 4 (multi-module Go) diferida — requiere infra de release multi-tag

## Cómo verificar

```bash
cd /Users/jcsv/GolandProjects/GoFrame/GoFrame
go build ./...                                          # debe compilar OK
go test ./pkg/app/... ./pkg/db/... ./contracts/...      # deben pasar
go test ./cmd/goframe/...                               # debe pasar
go build -tags "mssql,oracle" ./...                     # debe compilar OK
```

## Mapa de archivos clave

### Código de modularización
| Archivo | Propósito |
|---------|-----------|
| `pkg/app/extensions.go` | Interface `Extension`, `Option`, `WithExtensions()`, `WithoutDefaults()` |
| `pkg/app/app.go` | `app.New(cfg, ...Option)` refactorizado |
| `pkg/db/driver_mssql.go` | Driver MSSQL bajo `//go:build mssql` |
| `pkg/db/driver_oracle.go` | Driver Oracle bajo `//go:build oracle` |
| `internal/cli/new.go` | Scaffold autónomo + `--template api` |

### Documentación
| Archivo | Propósito |
|---------|-----------|
| `docs/BREADCRUMB.md` | Estado detallado (miga de pan) |
| `docs/AGENT_HANDOFF.md` | Este documento (guía de reanudación) |
| `docs/MODULARIZATION.md` | Plan de 4 fases (1-3 ✅, 4 diferida) |
| `docs/STATUS_NEXT_STEPS.md` | Estado general del proyecto |

## Convenciones del proyecto

- **Idioma código:** inglés
- **Idioma docs:** inglés
- **Idioma comunicación con usuario:** español
- **Tests:** siempre ejecutar antes de reportar
- **Backward compat:** obligatoria — `app.New(cfg)` sin opciones debe funcionar idéntico
- **Branch actual:** `codex/point-4-admin-runtime-impl`
- **Última versión publicada:** `v0.5.5`

## Contexto de la codebase

```
GoFrame/
├── cmd/goframe/          # CLI binary (goframe new, serve, migrate, etc.)
├── contracts/            # Freeze tests para contratos estables
├── docs/                 # Documentación completa
├── examples/             # Ejemplo mvc_api
├── internal/cli/         # Comandos CLI (new.go = scaffold)
├── migrations/           # Migraciones SQL
├── pkg/
│   ├── admin/           # Admin panel (React SPA + Go backend) — 9K LOC
│   ├── app/             # App container — app.New() con Extension pattern
│   ├── auth/            # JWT + session auth
│   ├── authz/           # Casbin RBAC
│   ├── db/              # Database layer (sqlite/pg/mysql + build-tagged mssql/oracle)
│   ├── errors/          # Domain error types
│   ├── mail/            # Mail providers
│   ├── model/           # Model registry + metadata
│   ├── observe/         # OpenTelemetry bootstrap
│   ├── openapi/         # OpenAPI contract helpers
│   ├── outbox/          # Transactional outbox
│   ├── plugins/         # Plugin SDK
│   ├── router/          # HTTP router + middleware
│   ├── signals/         # Event bus + Redis relay
│   ├── storage/         # File storage (S3/GCS/Azure/Local)
│   ├── tasks/           # Background jobs via Asynq
│   └── validate/        # Input validation
├── scripts/             # CI, release, compatibility
└── storage/             # Local storage directory
```
