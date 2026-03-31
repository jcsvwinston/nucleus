# GoFrame — Especificación técnica completa

## Framework tipo Django para Go basado en Chi

**Documento de implementación para Claude Code**

Versión: 1.0
Go mínimo: 1.22
Nombre del módulo: `github.com/jcsvwinston/GoFrame`

---

## 1. FILOSOFÍA DE DISEÑO

### 1.1. Principios inmutables

1. **stdlib-first**: Si Go stdlib lo resuelve, no se añade dependencia. `log/slog`, `net/http/httptest`, `crypto/*`, `database/sql`, `encoding/json`, `html/template` son ciudadanos de primera clase.
2. **Interfaces, no structs**: Cada componente expone interfaces. El usuario puede sustituir cualquier implementación sin tocar el framework.
3. **API frozen por major version**: Siguiendo Go compatibility promise. Una vez publicada una v1, los tipos exportados no cambian.
4. **Zero globals**: Toda configuración es explícita. Nada de `init()` mágicos ni singletons.
5. **Composición sobre herencia**: El framework son paquetes independientes que se ensamblan. Puedes usar solo el ORM, solo el admin, solo el CLI.
6. **Errores explícitos**: Todo error se propaga con contexto. Nunca se ignora. Se usa `fmt.Errorf("contexto: %w", err)` para wrapping.

### 1.2. Dependencias externas permitidas (solo Tier 0 y Tier 1)

| Paquete | Versión | Razón |
|---------|---------|-------|
| `go-chi/chi/v5` | v5.2+ | Router core, 100% net/http |
| `uptrace/bun` | v1.2+ | ORM SQL-first sobre `database/sql` |
| `uptrace/bun/migrate` | v1.2+ | Migraciones SQL integradas con Bun |
| `golang-jwt/jwt/v5` | v5.2+ | JWT signing/validation |
| `casbin/casbin/v2` | v2.100+ | Autorización RBAC/ABAC |
| `alexedwards/scs/v2` | v2.8+ | Sesiones server-side |
| `go-playground/validator/v10` | v10.23+ | Validación struct tags |
| `knadh/koanf/v2` | v2.1+ | Config multi-source |
| `go.mongodb.org/mongo-driver` | v1.17+ | Driver oficial MongoDB |
| `redis/go-redis/v9` | v9.7+ | Cliente Redis |
| `stretchr/testify` | v1.9+ | Solo en tests |
| `open-telemetry/opentelemetry-go` | v1.35+ | Traces y métricas |
| `prometheus/client_golang` | v1.20+ | Métricas Prometheus |

**NO se permite en el core**: ORMs que oculten SQL de forma opaca (incluido GORM como default), viper (deps excesivas), logrus (maintenance mode), cobra (overhead para CLI simple).

### 1.3. Estrategia de persistencia (polyglot)

1. **SQL relacional**: Implementación oficial con Bun (`pkg/db`).
2. **Documental**: Implementación oficial con MongoDB driver (`pkg/document`).
3. **Cache y pub/sub**: Redis con `go-redis/v9` (`pkg/cache`, `pkg/queue` opcional Redis).
4. **Contratos estables**: Las capas de dominio dependen de interfaces (`Repository`, `Cache`, `Queue`), nunca del driver concreto.
5. **Sin pseudo-ORM universal**: GoFrame no intenta abstraer SQL, Mongo y Redis en una sola API mágica; expone interfaces coherentes y adaptadores por tipo de datastore.

---

## 2. ESTRUCTURA DE PROYECTO

```
goframe/
├── cmd/
│   └── goframe/              # CLI tool (el "manage.py" de Django)
│       └── main.go
│
├── pkg/                      # API pública del framework — importable por usuarios
│   ├── app/                  # Application bootstrap y lifecycle
│   │   ├── app.go            # type App struct, New(), Run(), Shutdown()
│   │   └── config.go         # AppConfig parsing desde env/yaml
│   │
│   ├── router/               # Thin wrapper sobre chi con convenciones
│   │   ├── router.go         # type Router struct (embed chi.Mux)
│   │   ├── middleware.go      # Middleware stack estándar
│   │   └── render.go         # JSON/XML response helpers
│   │
│   ├── db/                   # SQL abstraction layer (Bun)
│   │   ├── db.go             # type DB struct (wraps bun.DB)
│   │   ├── migrate.go        # Wrapper sobre bun/migrate
│   │   ├── tx.go             # Transaction helpers con context
│   │   └── health.go         # DB health check
│   │
│   ├── document/             # Document DB abstraction (MongoDB)
│   │   ├── mongo.go          # Mongo client, DB, collection helpers
│   │   └── repository.go     # Repository helpers para documentos
│   │
│   ├── model/                # Base model y reflexión de metadatos
│   │   ├── registry.go       # type Registry, Register(), GetModel()
│   │   ├── meta.go           # Extracción de metadatos via reflect
│   │   ├── fields.go         # FieldMeta, inferencia de tipos HTML
│   │   └── crud.go           # GenericCRUD[T] — operaciones tipo-safe
│   │
│   ├── admin/                # Panel de administración auto-generado
│   │   ├── panel.go          # type Panel struct, NewPanel(), Handler()
│   │   ├── handlers.go       # API REST handlers para CRUD
│   │   ├── ui.go             # HTML/JS/CSS embebido (embed.FS)
│   │   ├── ui/               # Archivos estáticos del admin
│   │   │   ├── index.html
│   │   │   ├── app.js
│   │   │   └── style.css
│   │   └── actions.go        # Bulk actions, export CSV
│   │
│   ├── auth/                 # Autenticación y sesiones
│   │   ├── jwt.go            # JWT middleware y helpers
│   │   ├── session.go        # Session middleware (wraps scs)
│   │   ├── password.go       # bcrypt/argon2 hashing
│   │   └── user.go           # Interface UserProvider
│   │
│   ├── authz/                # Autorización
│   │   ├── enforcer.go       # Casbin wrapper con hot-reload
│   │   ├── middleware.go      # Chi middleware de autorización
│   │   └── policies.go       # Policy helpers
│   │
│   ├── validate/             # Validación
│   │   ├── validate.go       # Wrapper sobre go-playground/validator
│   │   ├── errors.go         # ValidationError → JSON response
│   │   └── rules.go          # Reglas custom reutilizables
│   │
│   ├── cache/                # Cache abstraction
│   │   ├── cache.go          # Interface Cache (Get, Set, Delete, Invalidate)
│   │   ├── redis.go          # Implementación Redis
│   │   ├── memory.go         # Implementación in-memory (dev/tests)
│   │   └── middleware.go      # HTTP cache middleware
│   │
│   ├── queue/                # Background jobs y eventos
│   │   ├── queue.go          # Interface Queue (Enqueue, Process)
│   │   ├── worker.go         # Worker pool con graceful shutdown
│   │   ├── pgqueue.go        # Implementación PostgreSQL (pg_notify + polling)
│   │   └── redis_queue.go    # Implementación Redis (BRPOP)
│   │
│   ├── mail/                 # Email
│   │   ├── mailer.go         # Interface Mailer (Send, SendTemplate)
│   │   ├── smtp.go           # Implementación net/smtp
│   │   ├── templates.go      # html/template para emails
│   │   └── console.go        # Mailer que imprime en stdout (dev)
│   │
│   ├── observe/              # Observabilidad
│   │   ├── logger.go         # slog wrapper con context extraction
│   │   ├── tracing.go        # OpenTelemetry setup
│   │   ├── metrics.go        # Prometheus metrics
│   │   └── middleware.go      # Request logging + tracing middleware
│   │
│   ├── errors/               # Error handling unificado
│   │   ├── errors.go         # DomainError, NotFound, Validation, etc.
│   │   ├── handler.go        # Error → HTTP response mapper
│   │   └── codes.go          # Error codes registry
│   │
│   └── testing/              # Test utilities
│       ├── suite.go          # TestSuite con DB, fixtures, cleanup
│       ├── factory.go        # Factory pattern para generar test data
│       ├── assertions.go     # Domain-specific assertions
│       └── httptest.go       # Request builder para handlers
│
├── internal/                 # Código privado del framework
│   ├── cli/                  # Implementación del CLI
│   │   ├── root.go           # Comando raíz
│   │   ├── serve.go          # `goframe serve`
│   │   ├── migrate.go        # `goframe migrate`
│   │   ├── seed.go           # `goframe seed`
│   │   ├── createuser.go     # `goframe createuser`
│   │   ├── shell.go          # `goframe shell` (REPL-like DB inspector)
│   │   └── generate.go       # `goframe generate model|handler|migration`
│   │
│   ├── codegen/              # Code generation templates
│   │   ├── model.go.tmpl
│   │   ├── handler.go.tmpl
│   │   ├── migration.sql.tmpl
│   │   └── test.go.tmpl
│   │
│   └── embed/                # Embedded assets
│       └── templates/        # Default templates (admin UI, emails)
│
├── examples/                 # Proyectos de ejemplo
│   ├── blog/                 # Blog CRUD completo
│   ├── api/                  # REST API pura
│   └── fullstack/            # App con admin + auth + queue
│
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
└── README.md
```

---

## 3. COMPONENTE: APP (Bootstrap y Lifecycle)

### 3.1. `pkg/app/app.go`

```go
// App es el contenedor principal de la aplicación. Equivale a django.setup().
type App struct {
    Config   *Config
    Router   *router.Router
    DB       *db.DB               // SQL (Bun)
    Document *document.Client     // MongoDB (opcional)
    Cache    cache.Cache
    Mailer   mail.Mailer
    Queue    queue.Queue
    Logger   *slog.Logger
    Admin    *admin.Panel
    Auth     *auth.Manager
    Authz    *authz.Enforcer
    Models   *model.Registry

    // Internal
    shutdownFns []func(context.Context) error
}

// New crea una App con la config proporcionada.
// NO inicia conexiones. Solo prepara el wiring.
func New(cfg *Config) (*App, error) { ... }

// Run inicia todos los servicios y bloquea hasta SIGINT/SIGTERM.
// Ejecuta graceful shutdown en orden inverso de inicio.
func (a *App) Run(ctx context.Context) error { ... }

// OnShutdown registra una función que se ejecuta durante el shutdown.
func (a *App) OnShutdown(fn func(context.Context) error) { ... }
```

### 3.2. `pkg/app/config.go`

```go
// Config se parsea desde env vars y/o archivo YAML via koanf.
// Cada campo tiene su default. Cero configuración = funciona en dev.
type Config struct {
    // Server
    Host         string        `koanf:"host" default:"0.0.0.0"`
    Port         int           `koanf:"port" default:"8080"`
    ReadTimeout  time.Duration `koanf:"read_timeout" default:"30s"`
    WriteTimeout time.Duration `koanf:"write_timeout" default:"60s"`
    IdleTimeout  time.Duration `koanf:"idle_timeout" default:"120s"`

    // Database
    DatabaseURL     string `koanf:"database_url" default:"postgres://localhost:5432/app?sslmode=disable"`
    DatabaseMaxOpen int    `koanf:"database_max_open" default:"25"`
    DatabaseMaxIdle int    `koanf:"database_max_idle" default:"5"`

    // Datastores no relacionales
    MongoURL string `koanf:"mongo_url"` // opcional
    MongoDB  string `koanf:"mongo_db"`  // default "app"
    RedisURL string `koanf:"redis_url"`

    // Auth
    JWTSecret       string        `koanf:"jwt_secret" required:"true"`
    JWTExpiry       time.Duration `koanf:"jwt_expiry" default:"24h"`
    SessionLifetime time.Duration `koanf:"session_lifetime" default:"72h"`

    // Admin
    AdminPrefix string `koanf:"admin_prefix" default:"/admin"`
    AdminTitle  string `koanf:"admin_title" default:"Admin"`

    // Mail
    SMTPHost string `koanf:"smtp_host"`
    SMTPPort int    `koanf:"smtp_port" default:"587"`
    SMTPUser string `koanf:"smtp_user"`
    SMTPPass string `koanf:"smtp_pass"`
    MailFrom string `koanf:"mail_from" default:"noreply@localhost"`

    // Observability
    LogLevel    string `koanf:"log_level" default:"info"`
    LogFormat   string `koanf:"log_format" default:"json"` // json | text
    OTLPEndpoint string `koanf:"otlp_endpoint"`             // Si vacío, no exporta traces
    MetricsPath  string `koanf:"metrics_path" default:"/metrics"`

    // Environment
    Env   string `koanf:"env" default:"development"` // development | staging | production
    Debug bool   `koanf:"debug" default:"false"`
}

// LoadConfig carga desde: 1) defaults, 2) archivo yaml, 3) env vars (prefijo GOFRAME_).
// Los env vars tienen precedencia sobre el yaml.
func LoadConfig(path ...string) (*Config, error) { ... }
```

**Detalle de implementación**: Usar `koanf.New("")` con providers en orden: `structs.Provider(defaults)` → `file.Provider(path)` → `env.Provider("GOFRAME_", ".", ...)`. Validar campos `required` al final.

---

## 4. COMPONENTE: ROUTER

### 4.1. `pkg/router/router.go`

```go
// Router extiende chi.Mux con convenciones del framework.
type Router struct {
    chi.Router
    app *app.App // Referencia al contenedor
}

// New crea un Router con el middleware stack estándar ya aplicado.
func New(a *app.App) *Router { ... }

// JSON escribe una respuesta JSON con status code.
func JSON(w http.ResponseWriter, status int, data interface{}) { ... }

// Error escribe un error como JSON response.
func Error(w http.ResponseWriter, err error) { ... }

// Bind decodifica el body JSON en el struct y lo valida.
// Retorna ValidationError si falla.
func Bind(r *http.Request, v interface{}) error { ... }

// Paginate extrae page/page_size del query string.
func Paginate(r *http.Request, defaultSize int) (page, pageSize int) { ... }
```

### 4.2. `pkg/router/middleware.go`

Middleware stack estándar que se aplica al crear un Router:

```go
func DefaultStack(a *app.App) []func(http.Handler) http.Handler {
    return []func(http.Handler) http.Handler{
        middleware.RequestID,
        middleware.RealIP,
        observe.RequestLogger(a.Logger),    // slog structured logging
        observe.TracingMiddleware(),         // OpenTelemetry span
        observe.MetricsMiddleware(),         // Prometheus request_duration
        middleware.Recoverer,
        middleware.Timeout(a.Config.ReadTimeout),
        CORSMiddleware(a.Config),
        middleware.Compress(5),
        SecurityHeaders(),                   // X-Content-Type-Options, etc.
    }
}
```

**SecurityHeaders** debe establecer: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `X-XSS-Protection: 0`, `Referrer-Policy: strict-origin-when-cross-origin`, `Content-Security-Policy: default-src 'self'`.

---

## 5. COMPONENTE: DB (Base de datos)

### 5.1. `pkg/db/db.go`

```go
// DB wrappea bun.DB añadiendo health checks y transaction helpers.
type DB struct {
    *bun.DB
    sql    *sql.DB
    logger *slog.Logger
}

// New abre una conexión SQL y monta Bun encima.
// Debe soportar: postgres, mysql, sqlite, sqlserver.
func New(cfg *app.Config, logger *slog.Logger) (*DB, error) {
    // Parsear cfg.DatabaseURL para elegir driver + dialect de Bun
    // Crear *sql.DB con database/sql
    // bun.NewDB(sqlDB, dialect)
    // Configurar MaxOpenConns, MaxIdleConns, ConnMaxLifetime
    // Ping para verificar conectividad
}

// Tx ejecuta fn dentro de una transacción.
// Si fn retorna error, hace rollback. Si no, commit.
// Soporta nested transactions via savepoints.
func (db *DB) Tx(ctx context.Context, fn func(ctx context.Context, tx bun.Tx) error) error { ... }

// Health retorna nil si la DB responde, error si no.
func (db *DB) Health(ctx context.Context) error {
    return db.sql.PingContext(ctx)
}
```

### 5.2. `pkg/db/migrate.go`

```go
// Migrator wrappea bun/migrate para gestionar schema migrations.
type Migrator struct {
    migrator *migrate.Migrator
    logger   *slog.Logger
}

// NewMigrator crea un migrator usando db *db.DB y migrations embebidas o en FS.
func NewMigrator(db *DB, migrationsPath string) (*Migrator, error) { ... }

// Up aplica todas las migrations pendientes.
func (m *Migrator) Up() error { ... }

// Down revierte la última migration.
func (m *Migrator) Down() error { ... }

// Steps aplica n migrations (positivo=up, negativo=down).
func (m *Migrator) Steps(n int) error { ... }

// Status retorna el estado actual de las migrations.
func (m *Migrator) Status() ([]MigrationStatus, error) { ... }

// Create genera archivos de migración vacíos con timestamp.
func (m *Migrator) Create(name string) error { ... }
```

El directorio de migraciones es `migrations/` en la raíz del proyecto del usuario:
```
migrations/
├── 000001_create_users.up.sql
├── 000001_create_users.down.sql
├── 000002_create_products.up.sql
└── 000002_create_products.down.sql
```

### 5.3. `pkg/document/mongo.go`

```go
// Client encapsula la conexión a MongoDB para repositorios documentales.
type Client struct {
    raw    *mongo.Client
    db     *mongo.Database
    logger *slog.Logger
}

// NewDocumentClient abre conexión Mongo si cfg.MongoURL está definido.
// Si está vacío, retorna nil, nil (feature opcional por app).
func NewDocumentClient(cfg *app.Config, logger *slog.Logger) (*Client, error) { ... }

// Collection retorna un handle tipado a la colección solicitada.
func (c *Client) Collection(name string) *mongo.Collection { ... }

// Health verifica disponibilidad del server MongoDB.
func (c *Client) Health(ctx context.Context) error { ... }
```

`pkg/document/repository.go` debe proveer helpers de filtros, paginación y ordenación seguros para evitar duplicación entre repositorios.

---

## 6. COMPONENTE: MODEL (Registro y metadatos)

### 6.1. `pkg/model/registry.go`

```go
// Registry almacena los modelos registrados y sus metadatos.
// Equivale al AppRegistry de Django.
type Registry struct {
    models map[string]*ModelMeta
    mu     sync.RWMutex
}

// ModelMeta contiene toda la información extraída de un struct.
type ModelMeta struct {
    Name       string       // Nombre del struct (e.g. "User")
    Plural     string       // Plural (e.g. "Users")
    Table      string       // Nombre de tabla SQL (e.g. "users")
    Fields     []FieldMeta  // Campos extraídos
    PrimaryKey string       // Nombre del campo PK
    Config     ModelConfig  // Config proporcionada por el usuario
    Type       reflect.Type // El reflect.Type del struct
}

// Register registra un modelo con su configuración.
// Extrae metadatos via reflect al momento del registro, no en runtime.
func (r *Registry) Register(model interface{}, cfg ...ModelConfig) { ... }
```

### 6.2. `pkg/model/meta.go`

La extracción de metadatos debe:

1. Recorrer los campos del struct incluyendo embeds (como `model.BaseModel` equivalente propio).
2. Leer tags: `bun:"column:column_name,pk,notnull"`, `json:"name"`, `validate:"required,email"`, `admin:"list,search,filter,readonly,exclude,label:Nombre"`.
3. Inferir tipo HTML del campo: string→text, int→number, bool→checkbox, time.Time→datetime-local, campos con "email" en nombre→email, campos con "password"→password, campos con "description/body/content"→textarea.
4. Detectar PK: campo `ID` o tag Bun con `pk`.
5. Calcular nombre de tabla: snake_case plural del nombre del struct.

### 6.3. `pkg/model/fields.go`

```go
type FieldMeta struct {
    Name       string // Go field name (e.g. "Email")
    Column     string // SQL column (e.g. "email")
    Label      string // Human label (e.g. "Correo electrónico")
    GoType     string // Tipo Go como string
    HTMLType   string // Tipo para formularios HTML
    IsPK       bool
    IsRequired bool
    IsReadOnly bool   // No aparece en formularios de edición
    IsList     bool   // Aparece en la vista de lista
    IsSearch   bool   // Campo buscable
    IsFilter   bool   // Campo filtrable (sidebar)
    IsExcluded bool   // Oculto del admin
    MaxLength  int
    Choices    []Choice // Para select/enum fields
}

type Choice struct {
    Value string
    Label string
}
```

### 6.4. `pkg/model/crud.go`

```go
// CRUD[T] proporciona operaciones CRUD genéricas type-safe.
// Usa Bun SQL-first con consultas explícitas.
type CRUD[T any] struct {
    db *db.DB
}

func NewCRUD[T any](db *db.DB) *CRUD[T] { ... }

func (c *CRUD[T]) FindAll(ctx context.Context, opts QueryOpts) ([]T, int64, error) { ... }
func (c *CRUD[T]) FindByID(ctx context.Context, id interface{}) (*T, error) { ... }
func (c *CRUD[T]) Create(ctx context.Context, entity *T) error { ... }
func (c *CRUD[T]) Update(ctx context.Context, id interface{}, updates map[string]interface{}) error { ... }
func (c *CRUD[T]) Delete(ctx context.Context, id interface{}) error { ... }

type QueryOpts struct {
    Page     int
    PageSize int
    Search   string              // ILIKE across search fields
    Filters  map[string]string   // field=value exact match
    OrderBy  string              // "created_at desc"
    Fields   []string            // SELECT específico
}
```

**Detalle de implementación**: Las queries se construyen con Bun Query Builder y SQL parametrizado. NUNCA concatenación de strings. El search usa `ILIKE` en PostgreSQL y `LOWER(col) LIKE LOWER(?)` en dialectos sin `ILIKE`. La paginación usa `LIMIT/OFFSET` y un `COUNT(*)` consistente.

---

## 7. COMPONENTE: ADMIN (Panel de administración)

### 7.1. Arquitectura

El admin tiene dos partes:
- **Backend**: API REST montada en chi que consume `model.Registry` y `model.CRUD` para generar endpoints CRUD automáticos.
- **Frontend**: SPA vanilla JS embebida via `embed.FS`. Sin build tools, sin npm, sin node. Un solo `index.html` + `app.js` + `style.css`.

### 7.2. `pkg/admin/panel.go`

```go
//go:embed ui/*
var uiFS embed.FS

type Panel struct {
    db       *db.DB
    registry *model.Registry
    config   PanelConfig
    auth     AdminAuth     // nil = sin auth (dev mode)
}

type PanelConfig struct {
    Prefix    string // default "/admin"
    Title     string // Nombre mostrado en sidebar
    Auth      AdminAuth
}

type AdminAuth interface {
    // Authenticate valida las credenciales y retorna el usuario admin.
    Authenticate(r *http.Request) (*AdminUser, error)
    // Authorize verifica si el usuario puede hacer la acción.
    Authorize(user *AdminUser, model string, action string) bool
    // LoginHandler renderiza/procesa el form de login.
    LoginHandler() http.Handler
}

type AdminUser struct {
    ID       string
    Username string
    Email    string
    IsSuperuser bool
}

func NewPanel(db *db.DB, registry *model.Registry, cfg PanelConfig) *Panel { ... }

// Handler retorna un chi.Router montable.
// Incluye auth middleware si AdminAuth está configurado.
func (p *Panel) Handler() chi.Router { ... }
```

### 7.3. Endpoints del admin

```
GET  {prefix}/                          → Dashboard HTML (embed.FS sirve index.html)
GET  {prefix}/static/*                  → Archivos estáticos (app.js, style.css)
GET  {prefix}/login                     → Login form (si auth configurado)
POST {prefix}/login                     → Login submit

# API JSON consumida por el frontend
GET  {prefix}/api/models                → Lista modelos registrados + count
GET  {prefix}/api/models/{name}/schema  → Metadatos del modelo (campos, config)
GET  {prefix}/api/models/{name}         → Listar registros (paginado, search, filter)
POST {prefix}/api/models/{name}         → Crear registro
GET  {prefix}/api/models/{name}/{id}    → Obtener registro
PUT  {prefix}/api/models/{name}/{id}    → Actualizar registro
DELETE {prefix}/api/models/{name}/{id}  → Eliminar registro (soft delete si tiene deleted_at)
POST {prefix}/api/models/{name}/bulk    → Bulk action (delete, export)
GET  {prefix}/api/models/{name}/export  → Export CSV
```

### 7.4. Frontend del admin (`pkg/admin/ui/`)

**Funcionalidades que debe implementar el frontend**:

1. **Sidebar**: Lista de modelos con icono, nombre, count. Click navega a list view.
2. **Dashboard**: Cards con resumen de cada modelo (count, último registro).
3. **List view**: Tabla con columnas configuradas en `ListFields`. Paginación. Barra de búsqueda. Filtros en sidebar. Ordenación por click en header. Checkbox para selección múltiple.
4. **Detail/Edit view**: Formulario auto-generado desde schema. Tipos de input correctos. Validación client-side básica.
5. **Create view**: Formulario para nuevo registro.
6. **Bulk actions**: Dropdown con acciones (Eliminar seleccionados, Exportar CSV).
7. **Toast notifications**: Feedback visual de éxito/error.
8. **Dark mode**: Detecta `prefers-color-scheme` automáticamente.
9. **Responsive**: Funciona en móvil (sidebar colapsable).
10. **SPA routing**: Navegación sin recargas via History API.
11. **UI rica**: Data table avanzada, modales, dropdowns, tabs, command palette y estados vacíos/carga consistentes.

**Restricciones de implementación**:
- Tailwind CSS como sistema de diseño base y utilidades.
- Componentes UI reutilizables (botones, tablas, formularios, modales, toasts, paginación, breadcrumbs).
- JS en vanilla ES2020+ o micro-librerías ligeras (`alpinejs` opcional); evitar frameworks SPA pesados.
- Build frontend permitido solo para assets del framework (no obligatorio para el usuario final de GoFrame).
- `fetch()` para todas las llamadas API
- `embed.FS` para servir los archivos

---

## 8. COMPONENTE: AUTH (Autenticación)

### 8.1. `pkg/auth/jwt.go`

```go
type JWTManager struct {
    secret []byte
    expiry time.Duration
}

type Claims struct {
    UserID   string `json:"uid"`
    Username string `json:"username"`
    Role     string `json:"role"`
    jwt.RegisteredClaims
}

func NewJWTManager(secret string, expiry time.Duration) *JWTManager { ... }

// Generate crea un token JWT firmado.
func (m *JWTManager) Generate(claims Claims) (string, error) { ... }

// Validate parsea y valida un token JWT. Retorna Claims o error.
func (m *JWTManager) Validate(tokenString string) (*Claims, error) { ... }

// Middleware extrae el token del header Authorization: Bearer <token>
// y lo pone en el context. Si falla, retorna 401.
func (m *JWTManager) Middleware() func(http.Handler) http.Handler { ... }

// FromContext extrae Claims del context (puestos por el middleware).
func FromContext(ctx context.Context) (*Claims, bool) { ... }
```

### 8.2. `pkg/auth/password.go`

```go
// HashPassword genera un hash bcrypt con cost 12.
func HashPassword(password string) (string, error) { ... }

// CheckPassword compara un password con su hash.
func CheckPassword(password, hash string) bool { ... }
```

Usar `golang.org/x/crypto/bcrypt` (paquete x/ oficial de Go, no dependencia tercera).

### 8.3. `pkg/auth/session.go`

Wrapper sobre `alexedwards/scs/v2` que:
- Almacena sesiones en Redis si disponible, en memoria si no.
- Configura cookie: HttpOnly, Secure (en producción), SameSite=Lax.
- Expone `Put`, `Get`, `Destroy` con tipado.

---

## 9. COMPONENTE: AUTHZ (Autorización)

### 9.1. `pkg/authz/enforcer.go`

```go
type Enforcer struct {
    *casbin.Enforcer
    logger *slog.Logger
}

// New crea un enforcer con modelo RBAC por defecto.
// Si modelPath es vacío, usa el modelo RBAC embebido.
func New(modelPath, policyPath string) (*Enforcer, error) { ... }

// Middleware retorna un chi middleware que verifica permisos.
// Extrae el usuario de auth.FromContext(), el recurso del URL path,
// y la acción del HTTP method (GET→read, POST→create, PUT→update, DELETE→delete).
func (e *Enforcer) Middleware() func(http.Handler) http.Handler { ... }
```

Modelo RBAC por defecto embebido:
```ini
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && keyMatch(r.obj, p.obj) && (r.act == p.act || p.act == "*")
```

---

## 10. COMPONENTE: CACHE

### 10.1. `pkg/cache/cache.go`

```go
// Cache es la interface que toda implementación debe cumplir.
type Cache interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
    Flush(ctx context.Context) error
}
```

Dos implementaciones: `redis.go` (go-redis/v9) y `memory.go` (sync.Map + time-based expiry para dev/tests).

---

## 11. COMPONENTE: QUEUE (Background jobs)

### 11.1. `pkg/queue/queue.go`

```go
type Queue interface {
    // Enqueue añade un job a la cola.
    Enqueue(ctx context.Context, job Job) error
    // EnqueueAt programa un job para un momento futuro.
    EnqueueAt(ctx context.Context, job Job, at time.Time) error
}

type Job struct {
    Type    string          // Identificador del tipo de job
    Payload json.RawMessage // Datos del job
    MaxRetries int          // Reintentos (default 3)
}

type Handler func(ctx context.Context, payload json.RawMessage) error

type Worker interface {
    // Register asocia un handler a un tipo de job.
    Register(jobType string, handler Handler)
    // Start inicia el procesamiento. Bloquea hasta ctx.Done().
    Start(ctx context.Context) error
}
```

**Implementación PostgreSQL** (`pgqueue.go`): Usa una tabla `goframe_jobs` con `status` (pending/processing/completed/failed), `SKIP LOCKED` para concurrencia, y `pg_notify` para wakeup inmediato.

```sql
CREATE TABLE goframe_jobs (
    id          BIGSERIAL PRIMARY KEY,
    type        TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'pending',
    attempts    INT NOT NULL DEFAULT 0,
    max_retries INT NOT NULL DEFAULT 3,
    scheduled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at  TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    failed_at   TIMESTAMPTZ,
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_goframe_jobs_pending ON goframe_jobs (scheduled_at)
    WHERE status = 'pending';
```

---

## 12. COMPONENTE: MAIL

### 12.1. `pkg/mail/mailer.go`

```go
type Mailer interface {
    Send(ctx context.Context, msg Message) error
}

type Message struct {
    To      []string
    CC      []string
    BCC     []string
    Subject string
    Body    string          // Plain text
    HTML    string          // HTML (opcional)
    From    string          // Override del default
    ReplyTo string
}

// TemplateMailer compone mensajes desde html/template.
type TemplateMailer struct {
    mailer    Mailer
    templates *template.Template
    from      string
}

func (t *TemplateMailer) SendTemplate(ctx context.Context, to []string, tmplName string, data interface{}) error { ... }
```

Dos implementaciones:
- `smtp.go`: Usa `net/smtp` de stdlib. TLS via `crypto/tls`.
- `console.go`: Imprime el email formateado en stdout. Para desarrollo.

---

## 13. COMPONENTE: OBSERVE (Observabilidad)

### 13.1. `pkg/observe/logger.go`

```go
// NewLogger crea un slog.Logger configurado según el entorno.
func NewLogger(level, format string) *slog.Logger {
    var handler slog.Handler
    lvl := parseLevel(level)

    switch format {
    case "json":
        handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
    default:
        handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
    }

    return slog.New(handler)
}

// WithContext devuelve un logger con campos extraídos del context
// (request_id, user_id, trace_id).
func WithContext(ctx context.Context, logger *slog.Logger) *slog.Logger { ... }
```

### 13.2. `pkg/observe/middleware.go`

El middleware de request logging debe logear:
```json
{
    "level": "info",
    "msg": "http_request",
    "method": "GET",
    "path": "/api/users",
    "status": 200,
    "duration_ms": 12.5,
    "request_id": "abc-123",
    "user_id": "usr_456",
    "remote_addr": "192.168.1.1",
    "user_agent": "curl/8.0"
}
```

---

## 14. COMPONENTE: ERRORS

### 14.1. `pkg/errors/errors.go`

```go
type DomainError struct {
    Code       string `json:"code"`
    Message    string `json:"message"`
    StatusCode int    `json:"-"`
    Details    any    `json:"details,omitempty"`
}

func (e *DomainError) Error() string { return e.Message }

// Constructores predefinidos
func NotFound(resource, id string) *DomainError { ... }          // 404
func BadRequest(message string) *DomainError { ... }             // 400
func Unauthorized(message string) *DomainError { ... }           // 401
func Forbidden(message string) *DomainError { ... }              // 403
func Conflict(message string) *DomainError { ... }               // 409
func InternalError(message string) *DomainError { ... }          // 500
func ValidationFailed(fields map[string]string) *DomainError { ... } // 422
```

### 14.2. `pkg/errors/handler.go`

```go
// ErrorHandler es un middleware que captura DomainErrors y los serializa como JSON.
func ErrorHandler(logger *slog.Logger) func(http.Handler) http.Handler { ... }
```

Respuesta estándar de error:
```json
{
    "error": {
        "code": "NOT_FOUND",
        "message": "User 'abc123' not found",
        "details": null
    }
}
```

---

## 15. COMPONENTE: CLI (El "manage.py")

### 15.1. Comandos

El CLI usa stdlib `flag` + dispatch manual (sin cobra). Invocación: `go run ./cmd/goframe <command> [flags]`.

| Comando | Equivalente Django | Descripción |
|---------|-------------------|-------------|
| `serve` | `runserver` | Inicia el servidor HTTP |
| `migrate` | `migrate` | Aplica migraciones pendientes |
| `migrate down` | `migrate <app> zero` | Revierte última migración |
| `migrate reset` | `migrate <app> zero` | Revierte todas las migraciones aplicadas |
| `migrate refresh` | `migrate` + `migrate <app> zero` | Revierte todo y reaplica |
| `migrate create <name>` | `makemigrations` | Genera archivos de migración vacíos |
| `migrate status` | `showmigrations` | Muestra estado actual |
| `createuser` | `createsuperuser` | Crea o actualiza un usuario admin (interactivo o `--no-input`) |
| `seed` | `loaddata` | Ejecuta seeders registrados |
| `shell` | `shell` | Abre inspector DB (queries interactivas) |
| `generate model <name>` | `startapp` | Genera scaffold de modelo |
| `generate handler <name>` | - | Genera scaffold de handler |
| `generate migration <name>` | `makemigrations --empty` | Genera migración SQL vacía |
| `generate resource <name>` | `startapp` | Genera scaffold CRUD base (modelo + handler + test + migración) |
| `routes` | `show_urls` | Lista todas las rutas registradas |
| `health` | - | Verifica DB, Redis, dependencias |

### 15.2. Implementación del CLI

```go
// cmd/goframe/main.go
func main() {
    if len(os.Args) < 2 {
        printUsage()
        os.Exit(1)
    }

    cmd := os.Args[1]
    args := os.Args[2:]

    switch cmd {
    case "serve":
        cli.Serve(args)
    case "migrate":
        cli.Migrate(args)
    case "createuser":
        cli.CreateUser(args)
    case "seed":
        cli.Seed(args)
    case "generate":
        cli.Generate(args)
    case "routes":
        cli.Routes(args)
    case "health":
        cli.Health(args)
    default:
        fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
        os.Exit(1)
    }
}
```

---

## 16. COMPONENTE: TESTING

### 16.1. `pkg/testing/suite.go`

```go
// Suite proporciona un entorno de test con DB limpia.
type Suite struct {
    DB       *db.DB
    App      *app.App
    Client   *httptest.Server
    cleanup  []func()
}

// NewSuite crea una suite con DB de test (usa la misma DB con schema aislado
// via CREATE SCHEMA test_<random>; SET search_path TO test_<random>;).
func NewSuite(t *testing.T) *Suite { ... }

// Cleanup ejecuta todas las funciones de limpieza en orden inverso.
// Se llama automáticamente con t.Cleanup().
func (s *Suite) Cleanup() { ... }

// Request ejecuta un HTTP request contra el test server.
func (s *Suite) Request(method, path string, body interface{}) *httptest.ResponseRecorder { ... }
```

### 16.2. `pkg/testing/factory.go`

```go
// Factory genera datos de test para un modelo.
type Factory[T any] struct {
    db      *db.DB
    builder func(overrides map[string]interface{}) T
}

func NewFactory[T any](db *db.DB, builder func(map[string]interface{}) T) *Factory[T] { ... }

// Create inserta un registro con valores por defecto + overrides.
func (f *Factory[T]) Create(overrides ...map[string]interface{}) (*T, error) { ... }

// CreateBatch inserta N registros.
func (f *Factory[T]) CreateBatch(n int, overrides ...map[string]interface{}) ([]T, error) { ... }
```

---

## 17. ORDEN DE IMPLEMENTACIÓN

Claude Code debe implementar en este orden, ya que cada fase construye sobre la anterior:

### Fase 1: Fundamentos (sin dependencias externas)
1. `pkg/errors/` — Tipos de error y handler
2. `pkg/app/config.go` — Config parsing con koanf
3. `pkg/observe/logger.go` — slog wrapper

### Fase 2: Base de datos
4. `pkg/db/db.go` — Conexión SQL multi-dialecto con Bun
5. `pkg/db/migrate.go` — Wrapper sobre bun/migrate
6. `pkg/db/tx.go` — Transaction helpers
7. `pkg/document/` — Cliente MongoDB + repositorios documentales

### Fase 3: Modelo y CRUD
8. `pkg/model/fields.go` — FieldMeta y utilidades
9. `pkg/model/meta.go` — Extracción de metadatos reflect
10. `pkg/model/registry.go` — Registry
11. `pkg/model/crud.go` — CRUD genérico con Bun

### Fase 4: Router y HTTP
12. `pkg/router/router.go` — Router wrapper
13. `pkg/router/middleware.go` — Middleware stack
14. `pkg/router/render.go` — Response helpers
15. `pkg/validate/` — Validación
16. `pkg/errors/handler.go` — Error middleware

### Fase 5: Auth y seguridad
17. `pkg/auth/password.go` — Hashing
18. `pkg/auth/jwt.go` — JWT manager
19. `pkg/auth/session.go` — Session manager
20. `pkg/authz/` — Casbin wrapper

### Fase 6: Admin
21. `pkg/admin/panel.go` — Panel core
22. `pkg/admin/handlers.go` — API handlers
23. `pkg/admin/ui/` — Frontend embebido
24. `pkg/admin/actions.go` — Bulk actions

### Fase 7: Infraestructura
25. `pkg/cache/` — Cache interface + implementaciones
26. `pkg/queue/` — Job queue
27. `pkg/mail/` — Mailer

### Fase 8: Observabilidad
28. `pkg/observe/tracing.go` — OpenTelemetry
29. `pkg/observe/metrics.go` — Prometheus
30. `pkg/observe/middleware.go` — Request middleware

### Fase 9: CLI
31. `internal/cli/` — Todos los comandos
32. `internal/codegen/` — Templates de generación
33. `cmd/goframe/main.go` — Entry point

### Fase 10: Testing y ejemplos
34. `pkg/testing/` — Suite, factory, helpers
35. `examples/` — Proyectos de ejemplo
36. Tests de integración del framework

---

## 18. BASE MODEL DEL FRAMEWORK

El framework define un BaseModel que los usuarios embeben (equivalente a `models.Model` de Django):

```go
// pkg/model/base.go
type BaseModel struct {
    ID        int64      `db:"id" json:"id" admin:"list"`
    CreatedAt time.Time  `db:"created_at" json:"created_at" admin:"list,readonly"`
    UpdatedAt time.Time  `db:"updated_at" json:"updated_at" admin:"readonly"`
    DeletedAt *time.Time `db:"deleted_at" json:"-" admin:"exclude"`
}
```

Migración SQL correspondiente (generada por `goframe generate model`):
```sql
-- Cada modelo del usuario genera su migración. El BaseModel aporta estas columnas:
CREATE TABLE {table_name} (
    id          BIGSERIAL PRIMARY KEY,
    -- ... campos del usuario ...
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

CREATE INDEX idx_{table_name}_deleted_at ON {table_name} (deleted_at) WHERE deleted_at IS NULL;
```

---

## 19. EJEMPLO DE USO FINAL

Así se ve una aplicación construida con GoFrame:

```go
package main

import (
    "context"
    "github.com/jcsvwinston/GoFrame/pkg/app"
    "github.com/jcsvwinston/GoFrame/pkg/admin"
    "github.com/jcsvwinston/GoFrame/pkg/auth"
    "github.com/jcsvwinston/GoFrame/pkg/model"
    "myapp/internal/models"
    "myapp/internal/handlers"
)

func main() {
    // 1. Config (desde env vars y/o goframe.yaml)
    cfg, _ := app.LoadConfig("goframe.yaml")

    // 2. App
    a, _ := app.New(cfg)

    // 3. Registrar modelos
    a.Models.Register(&models.User{}, model.ModelConfig{
        Icon:   "👤",
        Admin: model.AdminConfig{
            ListFields:   []string{"ID", "Email", "Name", "Role", "CreatedAt"},
            SearchFields: []string{"Email", "Name"},
            Filters:      []string{"Role", "Active"},
        },
    })
    a.Models.Register(&models.Product{})
    a.Models.Register(&models.Order{})

    // 4. Admin (auto-detecta modelos del registry)
    a.Admin = admin.NewPanel(a.DB, a.Models, admin.PanelConfig{
        Title: "Mi Tienda Admin",
    })

    // 5. Rutas de la aplicación
    a.Router.Route("/api/v1", func(r chi.Router) {
        r.Use(a.Auth.JWTMiddleware())
        r.Mount("/users", handlers.UserRoutes(a))
        r.Mount("/products", handlers.ProductRoutes(a))
    })

    // 6. Montar admin
    a.Router.Mount(cfg.AdminPrefix, a.Admin.Handler())

    // 7. Ejecutar
    a.Run(context.Background())
}
```

---

## 20. CONVENCIONES PARA CLAUDE CODE

1. **Cada archivo Go comienza con un comment de paquete** que explica su propósito.
2. **Interfaces antes de structs** en cada archivo.
3. **Constructores se llaman `New`** (e.g. `NewPanel`, `NewCRUD`).
4. **Errores se wrappean con contexto**: `fmt.Errorf("admin.ListRecords model=%s: %w", name, err)`.
5. **Context como primer parámetro** en toda función que haga I/O.
6. **Tests en el mismo paquete** con `_test.go`. Tabla-driven tests preferidos.
7. **Sin init()** excepto para registro de drivers de DB.
8. **Toda config tiene un default** sensato para desarrollo.
9. **Logging**: usar `slog.Info/Warn/Error` con key-value pairs, nunca `fmt.Printf`.
10. **Nombres en inglés** para código, **documentación** en español.

---
