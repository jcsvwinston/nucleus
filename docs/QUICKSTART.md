# GoFrame Quickstart

Este Quickstart te deja una app funcional en pocos minutos con:

- servidor HTTP
- admin panel
- rutas API
- migraciones SQL
- seed inicial

Si quieres validar primero un ejemplo ya montado del propio repo:

```bash
go run ./examples/mvc_api
```

## 1) Requisitos

- Go 1.23+
- Un proyecto Go (modulo) inicializado

Referencia de estructura recomendada: [docs/PROJECT_LAYOUT.md](/Users/jcsv/GolandProjects/GoFrame/GoFrame/docs/PROJECT_LAYOUT.md)

## 2) Configuracion minima

Crea `goframe.yaml` en la raiz del proyecto:

```yaml
database_engine: bun
database_url: sqlite://app.db
host: 0.0.0.0
port: 8080
env: development
log_level: info
log_format: text
admin_prefix: /admin
admin_title: Mi Admin
```

## 3) Bootstrap de aplicacion

Crea `main.go`:

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
	Name  string `db:"name" validate:"required" admin:"list,search"`
	Email string `db:"email" validate:"required,email" admin:"list,search"`
}

func main() {
	cfg, err := app.LoadConfig("goframe.yaml")
	if err != nil {
		log.Fatal(err)
	}

	a, err := app.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	if err := a.RegisterModel(&User{}, model.ModelConfig{
		ListFields:   []string{"ID", "Name", "Email"},
		SearchFields: []string{"Name", "Email"},
		OrderBy:      "created_at desc",
	}); err != nil {
		log.Fatal(err)
	}

	a.Router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Println("http://localhost:8080")
	log.Fatal(a.Run(context.Background()))
}
```

## 4) Genera recurso y migraciones

```bash
go run ./cmd/goframe generate resource User
```

Aplica migraciones:

```bash
go run ./cmd/goframe migrate --config goframe.yaml
```

## 5) Seed de datos

Crea `seeds/001_users.sql`:

```sql
INSERT INTO users (name, email) VALUES ('Admin', 'admin@example.com');
```

Ejecuta seed:

```bash
go run ./cmd/goframe seed --config goframe.yaml --seeds seeds
```

## 6) Usuario admin

```bash
go run ./cmd/goframe createuser \
  --config goframe.yaml \
  --no-input \
  --username admin \
  --email admin@example.com \
  --password supersecret123
```

## 7) Arranca servidor

```bash
go run .
```

Accesos:

- App: `http://localhost:8080`
- Health: `http://localhost:8080/health`
- Admin: `http://localhost:8080/admin`

## 8) Comandos utiles de diagnostico

```bash
go run ./cmd/goframe routes --config goframe.yaml
go run ./cmd/goframe health --config goframe.yaml --json
go run ./cmd/goframe migrate --config goframe.yaml status
```
