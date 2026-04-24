# Breadcrumb — Estado del trabajo de modularización

> Documento de seguimiento "miga de pan". Actualizar cada vez que se avance.
> Última actualización: 2026-04-24.

## Última sesión

**Fecha:** 2026-04-24, ~09:00 CEST
**Rama:** `codex/point-4-admin-runtime-impl` (HEAD)
**Estado:** Todos los cambios en working tree, sin commit.

## Resumen de fases

| Fase | Descripción | Estado | Tests |
|------|-------------|--------|-------|
| **1** | go.mod autónomo en scaffold | ✅ Completa | ✅ Pasan |
| **2** | Build tags MSSQL/Oracle | ✅ Completa | ✅ Pasan |
| **3** | Extension pattern + `app.New(...Option)` | ✅ Completa | ✅ Pasan |
| **4** | Go multi-module split | ↩️ Revertida (diferida post-v1) | ✅ Pasan |

## Estado actual

**✅ Todo verde.** Build compila, todos los tests pasan:

```bash
go build ./...                    # OK
go test ./pkg/app/...             # OK
go test ./pkg/db/...              # OK
go test ./contracts/...           # OK
go test ./cmd/goframe/...         # OK (incluyendo TestRun_OpenAPIExport)
go build -tags "mssql,oracle" ... # OK
```

## Archivos modificados no commiteados

### Nuevos (untracked)
```
docs/AGENT_HANDOFF.md           # guía de reanudación para agentes
docs/BREADCRUMB.md              # este documento
docs/MODULARIZATION.md          # plan de 4 fases (1-3 completas, 4 diferida)
pkg/app/extensions.go           # Extension interface + Option/WithExtensions/WithoutDefaults
pkg/db/db_enterprise_test.go    # tests enterprise bajo build tags
pkg/db/driver_mssql.go          # MSSQL driver con build tag
pkg/db/driver_oracle.go         # Oracle driver con build tag
```

### Modificados
```
CHANGELOG.md                    # entradas Fases 1-3
SPEC.md                         # build tags + Extension pattern documentados
cmd/goframe/main_test.go        # template test actualizado (api→graphql)
docs/INDEX.md                   # links a MODULARIZATION, BREADCRUMB, AGENT_HANDOFF
docs/QUICKSTART.md              # Go 1.25+, self-contained, --template api, build tags
docs/STATUS_NEXT_STEPS.md       # Point 8 con Fases 1-3 completas
docs/governance/CI_MATRIX.md    # build tags en CI
docs/reference/DEPENDENCY_IMPACT_REPORT.md  # MSSQL/Oracle build-tagged
docs/reports/exploratory_stability.md       # nota build tags
go.mod                          # limpio (sin sub-módulos)
internal/cli/new.go             # scaffold autónomo + template api
pkg/app/app.go                  # app.New(...Option) composable
pkg/app/app_test.go             # tests Extension pattern
pkg/db/db.go                    # MSSQL/Oracle imports removidos
pkg/db/db_test.go               # enterprise tests movidos
pkg/db/sql_matrix_test.go       # enterprise tests movidos
```

## Lo que falta para cerrar

1. **Commit** — una vez el usuario lo apruebe
2. **(Opcional)** Actualizar `README.md` con positioning de modularidad
3. **(Post-v1)** Phase 4: multi-module split cuando haya infraestructura de release

## Arquitectura actual del scaffold

```
goframe new myapp --template mvc     # Full-stack: admin + storage + auth + mail
goframe new myapi --template api     # Core-only: router + model + db (ligero)
```

Ambos generan un `go.mod` autónomo con `require github.com/jcsvwinston/GoFrame <version>`.
No necesitan el binario GoFrame ni su source para compilar y ejecutar.
