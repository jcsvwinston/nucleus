# 🏗️ Chi Admin Framework

**Un panel de administración tipo Django para aplicaciones Go basadas en Chi + GORM.**

## ¿Qué es esto?

Un módulo `pkg/admin` que proporciona una experiencia similar al `django.contrib.admin`:

- **Registro declarativo de modelos** → `panel.Register(&User{}, config)` 
- **CRUD automático** → List, Create, Read, Update, Delete generados desde la reflexión del struct
- **UI embebida** → SPA moderna servida directamente desde el binario Go (sin build frontend)
- **Montable en Chi** → `r.Mount("/admin", panel.Handler())`
- **Búsqueda, filtros, paginación** → Configurables por modelo
- **Hooks** → BeforeCreate, AfterCreate, BeforeUpdate, AfterUpdate, BeforeDelete
- **Permisos** → `PermissionFunc` para integrar con tu sistema de auth (JWT, Casbin, etc.)
- **Read-only models** → Para audit logs y tablas de referencia

---

## Equivalencias con Django

| Django                            | Chi Admin Framework                          |
|-----------------------------------|----------------------------------------------|
| `admin.site.register(User)`       | `panel.Register(&User{}, config)`            |
| `class UserAdmin(ModelAdmin)`     | `admin.ModelConfig{...}`                     |
| `list_display`                    | `ListFields: []string{...}`                  |
| `search_fields`                   | `SearchFields: []string{...}`                |
| `list_filter`                     | `Filters: []string{...}`                     |
| `ordering`                        | `OrderBy: "created_at desc"`                 |
| `readonly_fields` / `readonly`    | `ReadOnly: true`                             |
| `exclude`                         | `ExcludeFields: []string{...}`               |
| `list_per_page`                   | `PageSize: 25`                               |
| `save_model()` hook               | `BeforeCreate / AfterCreate`                 |
| `has_change_permission()`         | `PermissionFunc`                             |
| `path('admin/', admin.site.urls)` | `r.Mount("/admin", panel.Handler())`         |

---

## Quick Start

```bash
mkdir mi-app && cd mi-app
go mod init mi-app

# Instalar dependencias
go get github.com/go-chi/chi/v5
go get gorm.io/gorm
go get gorm.io/driver/postgres  # o sqlite, mysql
```

### 1. Define tus modelos

```go
// internal/models/models.go
package models

import "gorm.io/gorm"

type User struct {
    gorm.Model
    Email    string `gorm:"uniqueIndex;not null" json:"Email"`
    Name     string `gorm:"not null" json:"Name"`
    Role     string `gorm:"default:'user'" json:"Role"`
    Active   bool   `gorm:"default:true" json:"Active"`
}

type Product struct {
    gorm.Model
    Name        string  `gorm:"not null" json:"Name"`
    Description string  `json:"Description"`
    Price       float64 `gorm:"not null" json:"Price"`
    Stock       int     `json:"Stock"`
    SKU         string  `gorm:"uniqueIndex" json:"SKU"`
}
```

### 2. Registra en el admin

```go
// cmd/server/main.go
package main

import (
    "log"
    "net/http"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"

    "mi-app/internal/models"
    "mi-app/pkg/admin"
)

func main() {
    // Database
    db, _ := gorm.Open(sqlite.Open("app.db"), &gorm.Config{})
    db.AutoMigrate(&models.User{}, &models.Product{})

    // Admin panel — 3 líneas y tienes un Django admin
    panel := admin.NewPanel(db, admin.PanelConfig{
        Prefix:   "/admin",
        SiteName: "Mi App",
    })

    panel.Register(&models.User{}, admin.ModelConfig{
        Icon:         "👤",
        ListFields:   []string{"ID", "Email", "Name", "Role", "Active"},
        SearchFields: []string{"Email", "Name"},
        Filters:      []string{"Role", "Active"},
        OrderBy:      "created_at desc",
    })

    panel.Register(&models.Product{}, admin.ModelConfig{
        Icon:         "📦",
        ListFields:   []string{"ID", "Name", "SKU", "Price", "Stock"},
        SearchFields: []string{"Name", "SKU"},
    })

    // Router
    r := chi.NewRouter()
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)

    // Mount admin — equivalent to Django's urlpatterns
    r.Mount("/admin", panel.Handler())

    // Tu API normal
    r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte(`{"status":"ok"}`))
    })

    log.Println("Server: http://localhost:8080")
    log.Println("Admin:  http://localhost:8080/admin")
    http.ListenAndServe(":8080", r)
}
```

### 3. Ejecuta

```bash
go run ./cmd/server
# Abre http://localhost:8080/admin
```

---

## Arquitectura

```
pkg/admin/
├── registry.go    # Registro de modelos, extracción de metadatos via reflect
├── handlers.go    # CRUD handlers montables en chi.Router
└── ui.go          # SPA embebida (vanilla JS, sin build tools)

# El panel expone:
GET  /admin/              → Dashboard HTML
GET  /admin/api/models    → Lista de modelos registrados
GET  /admin/api/models/{name}/schema  → Metadatos del modelo
GET  /admin/api/models/{name}/        → Listar registros (paginado)
POST /admin/api/models/{name}/        → Crear registro
GET  /admin/api/models/{name}/{id}    → Obtener registro
PUT  /admin/api/models/{name}/{id}    → Actualizar registro
DELETE /admin/api/models/{name}/{id}  → Eliminar registro
```

---

## Features avanzados

### Hooks (pre/post operación)

```go
panel.Register(&models.Order{}, admin.ModelConfig{
    BeforeCreate: func(db *gorm.DB, obj interface{}) error {
        order := obj.(*models.Order)
        if order.Total <= 0 {
            return fmt.Errorf("el total debe ser mayor a 0")
        }
        return nil
    },
    AfterCreate: func(db *gorm.DB, obj interface{}) error {
        order := obj.(*models.Order)
        // Enviar email de confirmación, crear audit log, etc.
        slog.Info("order_created", "id", order.ID, "total", order.Total)
        return nil
    },
})
```

### Permisos con Casbin

```go
import "github.com/casbin/casbin/v2"

enforcer, _ := casbin.NewEnforcer("model.conf", "policy.csv")

panel := admin.NewPanel(db, admin.PanelConfig{
    Prefix:   "/admin",
    SiteName: "Mi App",
    PermissionFunc: func(req admin.PermissionRequest) bool {
        ok, _ := enforcer.Enforce(req.UserID, req.ModelName, req.Action)
        return ok
    },
})
```

### Modelos de solo lectura

```go
panel.Register(&models.AuditLog{}, admin.ModelConfig{
    Icon:     "📝",
    ReadOnly: true,  // No se puede crear, editar ni eliminar
    OrderBy:  "created_at desc",
})
```

### Labels personalizados

```go
panel.Register(&models.User{}, admin.ModelConfig{
    FieldLabels: map[string]string{
        "CreatedAt": "Fecha de registro",
        "Active":    "¿Activo?",
        "Email":     "Correo electrónico",
    },
})
```

---

## Stack completo recomendado

```
┌─────────────────────────────────────────────┐
│  Chi v5          — Router + middleware       │
│  GORM v2         — ORM (o sqlc/ent)         │
│  pkg/admin       — Admin panel automático   │
│  golang-jwt v5   — Autenticación JWT        │
│  casbin v2       — Autorización RBAC/ABAC   │
│  ozzo-validation — Validación de datos      │
│  golang-migrate  — Migraciones de DB        │
│  go-redis v9     — Cache + pub/sub          │
│  asynq           — Task queue (Celery)      │
│  log/slog        — Logging estructurado     │
│  OpenTelemetry   — Traces + métricas        │
│  testify         — Testing                  │
│  pgx v5          — Driver PostgreSQL        │
└─────────────────────────────────────────────┘
```

---

## Próximos pasos (roadmap)

- [ ] Inline editing (editar campos directamente en la tabla)
- [ ] Bulk actions (seleccionar múltiples y aplicar acción)
- [ ] Relaciones FK con selects dinámicos
- [ ] Export CSV / Excel
- [ ] Dashboard con gráficos (Chart.js embebido)
- [ ] Dark mode toggle
- [ ] Audit trail automático
- [ ] File upload para campos de imagen
- [ ] Integración con templ + HTMX como alternativa a la SPA

---

## Licencia

MIT
