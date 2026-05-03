# Quark CLI — Outputs de ejemplo

Este directorio contiene ejemplos **reales** generados por el CLI de Quark contra los engines Docker del proyecto.

## Modelos generados (PostgreSQL → `quark model generate --from-table`)

| Archivo | Tabla origen | Dialect |
|---|---|---|
| `suiteusers.go` | `suite_users` | PostgreSQL |
| `posts.go` | `posts` | PostgreSQL |
| `logs.go` | `logs` | PostgreSQL |
| `tenantdatas.go` | `tenant_datas` | PostgreSQL |
| `benchmodels.go` | `bench_models` | PostgreSQL |
| `stressrecords.go` | `stress_records` | PostgreSQL |
| `txusers.go` | `tx_users` | PostgreSQL |
| `qbusers.go` | `q_b_users` | PostgreSQL |
| `eventusers.go` | `event_users` | PostgreSQL |
| `hookusers.go` | `hook_users` | PostgreSQL |
| `midusers.go` | `mid_users` | PostgreSQL |
| `rawtest.go` | `raw_test` | PostgreSQL |

## Outputs de inspección y validación

| Archivo | Comando |
|---|---|
| `inspect_schema_postgres.txt` | `quark inspect schema` (PostgreSQL) |
| `inspect_schema_mysql.txt` | `quark inspect schema` (MySQL) |
| `inspect_table_suite_users.txt` | `quark inspect table suite_users` + `quark inspect sql --model posts` |
| `validate_suite_users.txt` | `quark validate --table suite_users` |
| `migrate_status.txt` | `quark migrate status` + `quark migrate up --dry-run` |

## Cómo reproducir

```bash
# Construir el binario
go build -o quark ./cmd/quark/main.go

# Con PostgreSQL (puerto 5433 del docker-compose)
./quark --config .quark.yml inspect schema
./quark --config .quark.yml model generate --from-table users,orders --out ./models --package models
./quark --config .quark.yml validate --table users
./quark --config .quark.yml migrate up --dry-run
./quark --config .quark.yml migrate up
./quark --config .quark.yml migrate status
```

## Tags generados en los modelos

Los modelos generados usan los tags del ORM Quark:

```go
type SuiteUsers struct {
    ID    int     `db:"id" pk:"true" json:"id"`
    Name  *string `db:"name"         json:"name"`
    Email *string `db:"email"        json:"email"`
}

func (SuiteUsers) TableName() string { return "suite_users" }
```

- `db:"column_name"` — mapeo de columna
- `pk:"true"` — clave primaria
- Campos nullable → punteros Go (`*string`, `*int`, `*time.Time`)
- `time.Time` solo se importa cuando hay columnas de tipo fecha/timestamp
