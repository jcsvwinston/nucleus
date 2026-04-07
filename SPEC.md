# GoFrame - Full Technical Specification

## Django-like framework for Go based on Chi

**Implementation document for Claude Code**

Version: 1.0
Minimum Go: 1.22
Module name: `github.com/jcsvwinston/GoFrame`

---

## 1. DESIGN PHILOSOPHY

### 1.1. Immutable principles

1. **stdlib-first**: if the Go standard library can solve it, do not add a dependency. `log/slog`, `net/http/httptest`, `crypto/*`, `database/sql`, `encoding/json`, and `html/template` are first-class citizens.
2. **Interfaces, not structs**: each component exposes interfaces. Users can replace implementations without touching framework internals.
3. **Frozen API per major version**: follow Go compatibility promise. Once v1 is published, exported types must remain stable.
4. **Zero globals**: all configuration must be explicit. No magic `init()` or singletons.
5. **Composition over inheritance**: the framework is a set of independent packages that compose together. You can use only ORM, only admin, or only CLI.
6. **Explicit errors**: all errors are propagated with context. Never ignore errors. Use `fmt.Errorf("context: %w", err)` wrapping.

### 1.2. Allowed external dependencies (Tier 0 and Tier 1 only)

| Package | Version | Reason |
|---------|---------|--------|
| `go-chi/chi/v5` | v5.2+ | Core router, 100% net/http |
| `uptrace/bun` | v1.2+ | SQL-first ORM on top of `database/sql` |
| `uptrace/bun/migrate` | v1.2+ | SQL migrations integrated with Bun |
| `golang-jwt/jwt/v5` | v5.2+ | JWT signing/validation |
| `casbin/casbin/v2` | v2.100+ | RBAC/ABAC authorization |
| `alexedwards/scs/v2` | v2.8+ | Server-side sessions |
| `go-playground/validator/v10` | v10.23+ | Struct tag validation |
| `knadh/koanf/v2` | v2.1+ | Multi-source configuration |
| `go.mongodb.org/mongo-driver` | v1.17+ | Official MongoDB driver |
| `redis/go-redis/v9` | v9.7+ | Redis client |
| `stretchr/testify` | v1.9+ | Tests only |
| `open-telemetry/opentelemetry-go` | v1.35+ | Traces and metrics |
| `prometheus/client_golang` | v1.20+ | Prometheus metrics |

**Not allowed in core**: opaque ORMs (including GORM as default), viper (heavy deps), logrus (maintenance mode), cobra (overhead for simple CLI).

### 1.3. Persistence strategy (polyglot)

1. **Relational SQL**: official Bun implementation (`pkg/db`).
2. **Document database**: official MongoDB driver implementation (`pkg/document`).
3. **Cache and pub/sub**: Redis with `go-redis/v9` (`pkg/cache`, optional Redis in `pkg/queue`).
4. **Stable contracts**: domain layers depend on interfaces (`Repository`, `Cache`, `Queue`), never concrete drivers.
5. **No pseudo-universal ORM**: GoFrame does not force SQL, Mongo, and Redis behind one magical API. It provides coherent interfaces and explicit adapters per datastore type.

---

## 2. PROJECT STRUCTURE

```text
goframe/
├── cmd/
│   └── goframe/              # CLI tool (Django-like manage.py)
│       └── main.go
│
├── pkg/                      # Public framework API (importable by users)
│   ├── app/                  # Application bootstrap and lifecycle
│   │   ├── app.go            # type App struct, New(), Run(), Shutdown()
│   │   └── config.go         # AppConfig parsing from env/yaml
│   │
│   ├── router/               # Thin wrapper over chi with framework conventions
│   │   ├── router.go         # type Router struct (embed chi.Mux)
│   │   ├── middleware.go     # Standard middleware stack
│   │   └── render.go         # JSON/XML response helpers
│   │
│   ├── db/                   # SQL abstraction layer (Bun)
│   │   ├── db.go             # type DB struct (wraps bun.DB)
│   │   ├── migrate.go        # Wrapper over bun/migrate
│   │   ├── tx.go             # Transaction helpers with context
│   │   └── health.go         # DB health check
│   │
│   ├── document/             # Document DB abstraction (MongoDB)
│   │   ├── mongo.go          # Mongo client, DB, collection helpers
│   │   └── repository.go     # Repository helpers for documents
│   │
│   ├── model/                # Base model and metadata reflection
│   │   ├── registry.go       # type Registry, Register(), GetModel()
│   │   ├── meta.go           # Metadata extraction via reflection
│   │   ├── fields.go         # FieldMeta, HTML type inference
│   │   └── crud.go           # GenericCRUD[T] - type-safe operations
│   │
│   ├── admin/                # Auto-generated administration panel
│   │   ├── panel.go          # type Panel struct, NewPanel(), Handler()
│   │   ├── handlers.go       # REST API handlers for CRUD
│   │   ├── ui.go             # Embedded HTML/JS/CSS (embed.FS)
│   │   ├── ui/               # Admin static files
│   │   │   ├── index.html
│   │   │   ├── app.js
│   │   │   └── style.css
│   │   └── actions.go        # Bulk actions, CSV export
│   │
│   ├── auth/                 # Authentication and sessions
│   │   ├── jwt.go            # JWT middleware and helpers
│   │   ├── session.go        # Session middleware (wraps scs)
│   │   ├── password.go       # bcrypt/argon2 hashing
│   │   └── user.go           # Interface UserProvider
│   │
│   ├── authz/                # Authorization
│   │   ├── enforcer.go       # Casbin wrapper with hot-reload
│   │   ├── middleware.go     # Authorization middleware for chi
│   │   └── policies.go       # Policy helpers
│   │
│   ├── validate/             # Validation
│   │   ├── validate.go       # Wrapper over go-playground/validator
│   │   ├── errors.go         # ValidationError -> JSON response
│   │   └── rules.go          # Reusable custom rules
│   │
│   ├── cache/                # Cache abstraction
│   │   ├── cache.go          # Cache interface (Get, Set, Delete, Invalidate)
│   │   ├── redis.go          # Redis implementation
│   │   ├── memory.go         # In-memory implementation (dev/tests)
│   │   └── middleware.go     # HTTP cache middleware
│   │
│   ├── queue/                # Background jobs and events
│   │   ├── queue.go          # Queue interface (Enqueue, Process)
│   │   ├── worker.go         # Worker pool with graceful shutdown
│   │   ├── pgqueue.go        # PostgreSQL implementation (pg_notify + polling)
│   │   └── redis_queue.go    # Redis implementation (BRPOP)
│   │
│   ├── mail/                 # Email
│   │   ├── mailer.go         # Mailer interface (Send, SendTemplate)
│   │   ├── smtp.go           # net/smtp implementation
│   │   ├── templates.go      # html/template email rendering
│   │   └── console.go        # Mailer that prints to stdout (dev)
│   │
│   ├── observe/              # Observability
│   │   ├── logger.go         # slog wrapper with context extraction
│   │   ├── tracing.go        # OpenTelemetry setup
│   │   ├── metrics.go        # Prometheus metrics
│   │   └── middleware.go     # Request logging + tracing middleware
│   │
│   ├── errors/               # Unified error handling
│   │   ├── errors.go         # DomainError, NotFound, Validation, etc.
│   │   ├── handler.go        # Error -> HTTP response mapper
│   │   └── codes.go          # Error code registry
│   │
│   └── testing/              # Test utilities
│       ├── suite.go          # TestSuite with DB, fixtures, cleanup
│       ├── factory.go        # Factory pattern for test data generation
│       ├── assertions.go     # Domain-specific assertions
│       └── httptest.go       # Request builder for handlers
│
├── internal/                 # Framework private code
│   ├── cli/                  # CLI implementation
│   │   ├── root.go           # Root command
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
├── examples/                 # Example projects
│   ├── blog/                 # Full CRUD blog
│   ├── api/                  # Pure REST API
│   └── fullstack/            # App with admin + auth + queue
│
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
└── README.md
```

---

## 3. COMPONENT: APP (Bootstrap and Lifecycle)

### 3.1. `pkg/app/app.go`

```go
// App is the main application container. Equivalent to django.setup().
type App struct {
    Config   *Config
    Router   *router.Router
    DB       *db.DB               // SQL (Bun)
    Document *document.Client     // MongoDB (optional)
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

// New builds an App with the provided config.
// It does NOT start connections; it only prepares wiring.
func New(cfg *Config) (*App, error) { ... }

// Run starts all services and blocks until SIGINT/SIGTERM.
// It executes graceful shutdown in reverse startup order.
func (a *App) Run(ctx context.Context) error { ... }

// OnShutdown registers a function executed during shutdown.
func (a *App) OnShutdown(fn func(context.Context) error) { ... }
```

### 3.2. `pkg/app/config.go`

```go
// Config is parsed from env vars and/or YAML using koanf.
// Every field has a default. Zero configuration should work for dev.
type Config struct {
    // Server
    Host         string        `koanf:"host" default:"0.0.0.0"`
    Port         int           `koanf:"port" default:"8080"`
    ReadTimeout  time.Duration `koanf:"read_timeout" default:"30s"`
    WriteTimeout time.Duration `koanf:"write_timeout" default:"60s"`
    IdleTimeout  time.Duration `koanf:"idle_timeout" default:"120s"`

    // Database
    DatabaseDefault string                    `koanf:"database_default" default:"default"`
    Databases       map[string]DatabaseConfig `koanf:"databases"`

    // Non-relational datastores
    MongoURL string `koanf:"mongo_url"` // optional
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
    LogLevel     string `koanf:"log_level" default:"info"`
    LogFormat    string `koanf:"log_format" default:"json"` // json | text
    OTLPEndpoint string `koanf:"otlp_endpoint"`              // if empty, traces are not exported
    MetricsPath  string `koanf:"metrics_path" default:"/metrics"`

    // Environment
    Env   string `koanf:"env" default:"development"` // development | staging | production
    Debug bool   `koanf:"debug" default:"false"`
}

// LoadConfig loads from: 1) defaults, 2) yaml file, 3) env vars (GOFRAME_ prefix).
// Env vars take precedence over yaml.
func LoadConfig(path ...string) (*Config, error) { ... }
```

**Implementation detail**: use `koanf.New("")` with providers in this order: `structs.Provider(defaults)` -> `file.Provider(path)` -> `env.Provider("GOFRAME_", ".", ...)`. Validate `required` fields at the end.

---

## 4. COMPONENT: ROUTER

### 4.1. `pkg/router/router.go`

```go
// Router extends chi.Mux with framework conventions.
type Router struct {
    chi.Router
    app *app.App // reference to the app container
}

// New creates a Router with the default middleware stack already applied.
func New(a *app.App) *Router { ... }

// JSON writes a JSON response with the provided status code.
func JSON(w http.ResponseWriter, status int, data interface{}) { ... }

// Error writes an error as JSON response.
func Error(w http.ResponseWriter, err error) { ... }

// Bind decodes JSON body into the struct and validates it.
// Returns ValidationError on failure.
func Bind(r *http.Request, v interface{}) error { ... }

// Paginate extracts page/page_size from query string.
func Paginate(r *http.Request, defaultSize int) (page, pageSize int) { ... }
```

### 4.2. `pkg/router/middleware.go`

Default middleware stack applied when creating a Router:

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

`SecurityHeaders` must set: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `X-XSS-Protection: 0`, `Referrer-Policy: strict-origin-when-cross-origin`, `Content-Security-Policy: default-src 'self'`.

---

## 5. COMPONENT: DB (Database)

### 5.1. `pkg/db/db.go`

```go
// DB wraps bun.DB and adds health checks and transaction helpers.
type DB struct {
    *bun.DB
    sql    *sql.DB
    logger *slog.Logger
}

// New opens SQL connection and mounts Bun on top.
// Must support: postgres, mysql, sqlite, sqlserver.
func New(cfg *app.Config, logger *slog.Logger) (*DB, error) {
    // Parse cfg.DatabaseURL to select driver + Bun dialect
    // Build *sql.DB with database/sql
    // bun.NewDB(sqlDB, dialect)
    // Configure MaxOpenConns, MaxIdleConns, ConnMaxLifetime
    // Ping for connectivity verification
}

// Tx executes fn inside a transaction.
// If fn returns error -> rollback. Otherwise commit.
// Supports nested transactions via savepoints.
func (db *DB) Tx(ctx context.Context, fn func(ctx context.Context, tx bun.Tx) error) error { ... }

// Health returns nil if DB responds, or error otherwise.
func (db *DB) Health(ctx context.Context) error {
    return db.sql.PingContext(ctx)
}
```

### 5.2. `pkg/db/migrate.go`

```go
// Migrator wraps bun/migrate for schema migrations.
type Migrator struct {
    migrator *migrate.Migrator
    logger   *slog.Logger
}

// NewMigrator creates a migrator from *db.DB and migrations from FS/path.
func NewMigrator(db *DB, migrationsPath string) (*Migrator, error) { ... }

// Up applies all pending migrations.
func (m *Migrator) Up() error { ... }

// Down reverts last migration.
func (m *Migrator) Down() error { ... }

// Steps applies n migrations (positive=up, negative=down).
func (m *Migrator) Steps(n int) error { ... }

// Status returns migration state.
func (m *Migrator) Status() ([]MigrationStatus, error) { ... }

// Create generates empty migration files with timestamp.
func (m *Migrator) Create(name string) error { ... }
```

Migration directory in user project root:

```text
migrations/
├── 000001_create_users.up.sql
├── 000001_create_users.down.sql
├── 000002_create_products.up.sql
└── 000002_create_products.down.sql
```

### 5.3. `pkg/document/mongo.go`

```go
// Client encapsulates MongoDB connection for document repositories.
type Client struct {
    raw    *mongo.Client
    db     *mongo.Database
    logger *slog.Logger
}

// NewDocumentClient opens Mongo connection if cfg.MongoURL is configured.
// If empty, returns nil, nil (feature is optional per app).
func NewDocumentClient(cfg *app.Config, logger *slog.Logger) (*Client, error) { ... }

// Collection returns typed handle to the requested collection.
func (c *Client) Collection(name string) *mongo.Collection { ... }

// Health verifies MongoDB server availability.
func (c *Client) Health(ctx context.Context) error { ... }
```

`pkg/document/repository.go` should provide safe filtering, pagination, and sorting helpers to avoid duplication across repositories.

---

## 6. COMPONENT: MODEL (Registry and metadata)

### 6.1. `pkg/model/registry.go`

```go
// Registry stores registered models and metadata.
// Equivalent to Django AppRegistry.
type Registry struct {
    models map[string]*ModelMeta
    mu     sync.RWMutex
}

// ModelMeta holds all extracted struct metadata.
type ModelMeta struct {
    Name       string       // struct name (e.g. "User")
    Plural     string       // plural (e.g. "Users")
    Table      string       // SQL table name (e.g. "users")
    Fields     []FieldMeta  // extracted fields
    PrimaryKey string       // primary key field name
    Config     ModelConfig  // user-provided config
    Type       reflect.Type // struct reflect.Type
}

// Register registers a model with configuration.
// Metadata is extracted via reflection at registration time, not runtime.
func (r *Registry) Register(model interface{}, cfg ...ModelConfig) { ... }
```

### 6.2. `pkg/model/meta.go`

Metadata extraction must:

1. Traverse struct fields including embeds (such as custom `model.BaseModel`).
2. Read tags: `bun:"column:column_name,pk,notnull"`, `json:"name"`, `validate:"required,email"`, `admin:"list,search,filter,readonly,exclude,label:Name"`.
3. Infer HTML input type: string->text, int->number, bool->checkbox, time.Time->datetime-local, fields with `email` -> email, fields with `password` -> password, fields with `description/body/content` -> textarea.
4. Detect PK via `ID` field or Bun `pk` tag.
5. Compute table name as snake_case plural of struct name.

### 6.3. `pkg/model/fields.go`

```go
type FieldMeta struct {
    Name       string // Go field name (e.g. "Email")
    Column     string // SQL column (e.g. "email")
    Label      string // Human label (e.g. "Email address")
    GoType     string // Go type as string
    HTMLType   string // HTML form input type
    IsPK       bool
    IsRequired bool
    IsReadOnly bool     // hidden from edit forms
    IsList     bool     // shown in list view
    IsSearch   bool     // searchable field
    IsFilter   bool     // filterable field (sidebar)
    IsExcluded bool     // hidden from admin
    MaxLength  int
    Choices    []Choice // enum/select fields
}

type Choice struct {
    Value string
    Label string
}
```

### 6.4. `pkg/model/crud.go`

```go
// CRUD[T] provides generic type-safe CRUD operations.
// Uses SQL-first Bun queries with explicit builders.
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
    Search   string            // ILIKE across search fields
    Filters  map[string]string // field=value exact match
    OrderBy  string            // "created_at desc"
    Fields   []string          // explicit SELECT fields
}
```

**Implementation detail**: build queries with Bun query builder and parameterized SQL. Never concatenate SQL strings. Use `ILIKE` on PostgreSQL and `LOWER(col) LIKE LOWER(?)` on dialects without `ILIKE`. Pagination uses `LIMIT/OFFSET` plus consistent `COUNT(*)`.

---

## 7. COMPONENT: ADMIN (Administration panel)

### 7.1. Architecture

The admin has two parts:

- **Backend**: REST API mounted on chi, powered by `model.Registry` and `model.CRUD` to generate CRUD endpoints automatically.
- **Frontend**: embedded vanilla JS SPA via `embed.FS`. No build tools, npm, or node required for the base implementation. Single `index.html` + `app.js` + `style.css`.

### 7.2. `pkg/admin/panel.go`

```go
//go:embed ui/*
var uiFS embed.FS

type Panel struct {
    db       *db.DB
    registry *model.Registry
    config   PanelConfig
    auth     AdminAuth // nil = no auth (dev mode)
}

type PanelConfig struct {
    Prefix string // default "/admin"
    Title  string // shown in sidebar
    Auth   AdminAuth
}

type AdminAuth interface {
    // Authenticate validates credentials and returns admin user.
    Authenticate(r *http.Request) (*AdminUser, error)
    // Authorize checks whether user can perform action over model.
    Authorize(user *AdminUser, model string, action string) bool
    // LoginHandler renders/processes login form.
    LoginHandler() http.Handler
}

type AdminUser struct {
    ID          string
    Username    string
    Email       string
    IsSuperuser bool
}

func NewPanel(db *db.DB, registry *model.Registry, cfg PanelConfig) *Panel { ... }

// Handler returns a mountable chi.Router.
// Includes auth middleware when AdminAuth is configured.
func (p *Panel) Handler() chi.Router { ... }
```

### 7.3. Admin endpoints

```text
GET  {prefix}/                          -> Dashboard HTML (embed.FS serves index.html)
GET  {prefix}/static/*                  -> Static files (app.js, style.css)
GET  {prefix}/login                     -> Login form (if auth configured)
POST {prefix}/login                     -> Login submit

# JSON API consumed by frontend
GET  {prefix}/api/models                -> Registered models + count
GET  {prefix}/api/models/{name}/schema  -> Model metadata (fields, config)
GET  {prefix}/api/models/{name}         -> List records (pagination, search, filter)
POST {prefix}/api/models/{name}         -> Create record
GET  {prefix}/api/models/{name}/{id}    -> Get record
PUT  {prefix}/api/models/{name}/{id}    -> Update record
DELETE {prefix}/api/models/{name}/{id}  -> Delete record (soft delete if deleted_at exists)
POST {prefix}/api/models/{name}/bulk    -> Bulk action (delete, export)
GET  {prefix}/api/models/{name}/export  -> CSV export
```

### 7.4. Admin frontend (`pkg/admin/ui/`)

Frontend must implement:

1. **Sidebar**: model list with icon, name, count; click navigates to list view.
2. **Dashboard**: summary cards per model (count, latest row).
3. **List view**: table using `ListFields`, pagination, search bar, sidebar filters, sort by header click, multi-select checkbox.
4. **Detail/Edit view**: auto-generated form from schema, proper input types, basic client-side validation.
5. **Create view**: new record form.
6. **Bulk actions**: dropdown (Delete selected, Export CSV).
7. **Toast notifications**: visual success/error feedback.
8. **Dark mode**: detect `prefers-color-scheme` automatically.
9. **Responsive layout**: mobile support (collapsible sidebar).
10. **SPA routing**: no full page reloads, History API.
11. **Rich UI**: advanced table, modals, dropdowns, tabs, command palette, coherent empty/loading states.

Implementation constraints:

- Tailwind CSS as baseline design system and utilities.
- Reusable UI components (buttons, tables, forms, modals, toasts, pagination, breadcrumbs).
- Vanilla ES2020+ JS or lightweight micro-libraries (`alpinejs` optional); avoid heavy SPA frameworks.
- Frontend build allowed only for framework-owned assets (not mandatory for GoFrame end users).
- Use `fetch()` for all API calls.
- Serve all assets through `embed.FS`.

---

## 8. COMPONENT: AUTH (Authentication)

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

// Generate creates a signed JWT token.
func (m *JWTManager) Generate(claims Claims) (string, error) { ... }

// Validate parses and validates a JWT token. Returns Claims or error.
func (m *JWTManager) Validate(tokenString string) (*Claims, error) { ... }

// Middleware reads Authorization: Bearer <token>
// and stores claims in context. Returns 401 on failure.
func (m *JWTManager) Middleware() func(http.Handler) http.Handler { ... }

// FromContext extracts Claims from context (set by middleware).
func FromContext(ctx context.Context) (*Claims, bool) { ... }
```

### 8.2. `pkg/auth/password.go`

```go
// HashPassword creates bcrypt hash using cost 12.
func HashPassword(password string) (string, error) { ... }

// CheckPassword compares password against hash.
func CheckPassword(password, hash string) bool { ... }
```

Use `golang.org/x/crypto/bcrypt` (official Go x/ package).

### 8.3. `pkg/auth/session.go`

Wrapper over `alexedwards/scs/v2` that:

- stores sessions in Redis when available, memory otherwise
- configures cookie as HttpOnly, Secure (in production), SameSite=Lax
- exposes typed `Put`, `Get`, `Destroy`

---

## 9. COMPONENT: AUTHZ (Authorization)

### 9.1. `pkg/authz/enforcer.go`

```go
type Enforcer struct {
    *casbin.Enforcer
    logger *slog.Logger
}

// New creates an enforcer with default RBAC model.
// If modelPath is empty, use embedded RBAC model.
func New(modelPath, policyPath string) (*Enforcer, error) { ... }

// Middleware returns chi middleware that verifies permissions.
// Extracts user from auth.FromContext(), resource from URL path,
// and action from HTTP method (GET->read, POST->create, PUT->update, DELETE->delete).
func (e *Enforcer) Middleware() func(http.Handler) http.Handler { ... }
```

Embedded default RBAC model:

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

## 10. COMPONENT: CACHE

### 10.1. `pkg/cache/cache.go`

```go
// Cache is the interface every implementation must satisfy.
type Cache interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
    Flush(ctx context.Context) error
}
```

Two implementations: `redis.go` (`go-redis/v9`) and `memory.go` (`sync.Map` + time-based expiry for dev/tests).

---

## 11. COMPONENT: QUEUE (Background jobs)

### 11.1. `pkg/queue/queue.go`

```go
type Queue interface {
    // Enqueue adds a job to queue.
    Enqueue(ctx context.Context, job Job) error
    // EnqueueAt schedules a job for a future instant.
    EnqueueAt(ctx context.Context, job Job, at time.Time) error
}

type Job struct {
    Type       string          // Job type identifier
    Payload    json.RawMessage // Job payload
    MaxRetries int             // Retry limit (default 3)
}

type Handler func(ctx context.Context, payload json.RawMessage) error

type Worker interface {
    // Register maps handler to a job type.
    Register(jobType string, handler Handler)
    // Start begins processing. Blocks until ctx.Done().
    Start(ctx context.Context) error
}
```

**PostgreSQL implementation** (`pgqueue.go`): table `goframe_jobs` with `status` (pending/processing/completed/failed), `SKIP LOCKED` for concurrency, and `pg_notify` for immediate wakeup.

```sql
CREATE TABLE goframe_jobs (
    id           BIGSERIAL PRIMARY KEY,
    type         TEXT NOT NULL,
    payload      JSONB NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL DEFAULT 'pending',
    attempts     INT NOT NULL DEFAULT 0,
    max_retries  INT NOT NULL DEFAULT 3,
    scheduled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    failed_at    TIMESTAMPTZ,
    error        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_goframe_jobs_pending ON goframe_jobs (scheduled_at)
    WHERE status = 'pending';
```

---

## 12. COMPONENT: MAIL

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
    Body    string // plain text
    HTML    string // optional html
    From    string // default override
    ReplyTo string
}

// TemplateMailer composes messages from html/template.
type TemplateMailer struct {
    mailer    Mailer
    templates *template.Template
    from      string
}

func (t *TemplateMailer) SendTemplate(ctx context.Context, to []string, tmplName string, data interface{}) error { ... }
```

Two implementations:

- `smtp.go`: uses stdlib `net/smtp`; TLS via `crypto/tls`
- `console.go`: prints formatted email to stdout (development)

---

## 13. COMPONENT: OBSERVE (Observability)

### 13.1. `pkg/observe/logger.go`

```go
// NewLogger creates a slog.Logger configured for the selected environment.
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

// WithContext returns logger enriched with context-derived fields
// (request_id, user_id, trace_id).
func WithContext(ctx context.Context, logger *slog.Logger) *slog.Logger { ... }
```

### 13.2. `pkg/observe/middleware.go`

Request logging middleware should emit entries like:

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

## 14. COMPONENT: ERRORS

### 14.1. `pkg/errors/errors.go`

```go
type DomainError struct {
    Code       string `json:"code"`
    Message    string `json:"message"`
    StatusCode int    `json:"-"`
    Details    any    `json:"details,omitempty"`
}

func (e *DomainError) Error() string { return e.Message }

// Predefined constructors
func NotFound(resource, id string) *DomainError { ... }              // 404
func BadRequest(message string) *DomainError { ... }                 // 400
func Unauthorized(message string) *DomainError { ... }               // 401
func Forbidden(message string) *DomainError { ... }                  // 403
func Conflict(message string) *DomainError { ... }                   // 409
func InternalError(message string) *DomainError { ... }              // 500
func ValidationFailed(fields map[string]string) *DomainError { ... } // 422
```

### 14.2. `pkg/errors/handler.go`

```go
// ErrorHandler is middleware that catches DomainErrors and serializes JSON.
func ErrorHandler(logger *slog.Logger) func(http.Handler) http.Handler { ... }
```

Standard error payload:

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

## 15. COMPONENT: CLI (The "manage.py")

### 15.1. Commands

The CLI uses stdlib `flag` + manual dispatch (no cobra). Invocation: `go run ./cmd/goframe <command> [flags]`.

| Command | Django equivalent | Description |
|---------|-------------------|-------------|
| `serve` | `runserver` | Start HTTP server |
| `migrate` | `migrate` | Apply pending migrations |
| `migrate down` | `migrate <app> zero` | Revert latest migration |
| `migrate reset` | `migrate <app> zero` | Revert all applied migrations |
| `migrate refresh` | `migrate` + `migrate <app> zero` | Revert all then reapply |
| `migrate create <name>` | `makemigrations` | Generate empty migration files |
| `migrate status` | `showmigrations` | Show migration state |
| `createuser` | `createsuperuser` | Create or update admin user (interactive or `--no-input`) |
| `seed` | `loaddata` | Execute registered seeds |
| `shell` | `shell` | Open DB inspector (interactive queries) |
| `generate model <name>` | `startapp` | Generate model scaffold |
| `generate handler <name>` | - | Generate handler scaffold |
| `generate migration <name>` | `makemigrations --empty` | Generate empty SQL migration |
| `generate resource <name>` | `startapp` | Generate base CRUD scaffold (model + handler + test + migration) |
| `routes` | `show_urls` | List all registered routes |
| `health` | - | Verify DB, Redis, dependencies |

### 15.2. CLI implementation

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

## 16. COMPONENT: TESTING

### 16.1. `pkg/testing/suite.go`

```go
// Suite provides isolated test environment with clean DB.
type Suite struct {
    DB      *db.DB
    App     *app.App
    Client  *httptest.Server
    cleanup []func()
}

// NewSuite creates suite using isolated test schema
// (CREATE SCHEMA test_<random>; SET search_path TO test_<random>;).
func NewSuite(t *testing.T) *Suite { ... }

// Cleanup executes cleanup functions in reverse order.
// Automatically registered with t.Cleanup().
func (s *Suite) Cleanup() { ... }

// Request performs HTTP request against test server.
func (s *Suite) Request(method, path string, body interface{}) *httptest.ResponseRecorder { ... }
```

### 16.2. `pkg/testing/factory.go`

```go
// Factory generates test data for a model.
type Factory[T any] struct {
    db      *db.DB
    builder func(overrides map[string]interface{}) T
}

func NewFactory[T any](db *db.DB, builder func(map[string]interface{}) T) *Factory[T] { ... }

// Create inserts a record with defaults + overrides.
func (f *Factory[T]) Create(overrides ...map[string]interface{}) (*T, error) { ... }

// CreateBatch inserts N records.
func (f *Factory[T]) CreateBatch(n int, overrides ...map[string]interface{}) ([]T, error) { ... }
```

---

## 17. IMPLEMENTATION ORDER

Claude Code should implement in this order because each phase builds on prior work:

### Phase 1: Foundations (no external dependencies)

1. `pkg/errors/` - error types and handler
2. `pkg/app/config.go` - config parsing with koanf
3. `pkg/observe/logger.go` - slog wrapper

### Phase 2: Database

4. `pkg/db/db.go` - multi-dialect SQL connection with Bun
5. `pkg/db/migrate.go` - wrapper over bun/migrate
6. `pkg/db/tx.go` - transaction helpers
7. `pkg/document/` - MongoDB client + document repositories

### Phase 3: Model and CRUD

8. `pkg/model/fields.go` - FieldMeta and utilities
9. `pkg/model/meta.go` - reflection metadata extraction
10. `pkg/model/registry.go` - registry
11. `pkg/model/crud.go` - generic Bun CRUD

### Phase 4: Router and HTTP

12. `pkg/router/router.go` - router wrapper
13. `pkg/router/middleware.go` - middleware stack
14. `pkg/router/render.go` - response helpers
15. `pkg/validate/` - validation
16. `pkg/errors/handler.go` - error middleware

### Phase 5: Auth and security

17. `pkg/auth/password.go` - hashing
18. `pkg/auth/jwt.go` - JWT manager
19. `pkg/auth/session.go` - session manager
20. `pkg/authz/` - Casbin wrapper

### Phase 6: Admin

21. `pkg/admin/panel.go` - panel core
22. `pkg/admin/handlers.go` - API handlers
23. `pkg/admin/ui/` - embedded frontend
24. `pkg/admin/actions.go` - bulk actions

### Phase 7: Infrastructure

25. `pkg/cache/` - cache interface + implementations
26. `pkg/queue/` - job queue
27. `pkg/mail/` - mailer

### Phase 8: Observability

28. `pkg/observe/tracing.go` - OpenTelemetry
29. `pkg/observe/metrics.go` - Prometheus
30. `pkg/observe/middleware.go` - request middleware

### Phase 9: CLI

31. `internal/cli/` - all commands
32. `internal/codegen/` - generation templates
33. `cmd/goframe/main.go` - entry point

### Phase 10: Testing and examples

34. `pkg/testing/` - suite, factory, helpers
35. `examples/` - sample projects
36. framework integration tests

---

## 18. FRAMEWORK BASE MODEL

The framework defines a BaseModel that users embed (equivalent to Django `models.Model`):

```go
// pkg/model/base.go
type BaseModel struct {
    ID        int64      `db:"id" json:"id" admin:"list"`
    CreatedAt time.Time  `db:"created_at" json:"created_at" admin:"list,readonly"`
    UpdatedAt time.Time  `db:"updated_at" json:"updated_at" admin:"readonly"`
    DeletedAt *time.Time `db:"deleted_at" json:"-" admin:"exclude"`
}
```

Corresponding SQL migration (generated by `goframe generate model`):

```sql
-- Each user model generates its own migration; BaseModel contributes these columns.
CREATE TABLE {table_name} (
    id          BIGSERIAL PRIMARY KEY,
    -- ... user-defined fields ...
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

CREATE INDEX idx_{table_name}_deleted_at ON {table_name} (deleted_at) WHERE deleted_at IS NULL;
```

---

## 19. FINAL USAGE EXAMPLE

A GoFrame application may look like this:

```go
package main

import (
    "context"

    "github.com/go-chi/chi/v5"
    "github.com/jcsvwinston/GoFrame/pkg/admin"
    "github.com/jcsvwinston/GoFrame/pkg/app"
    "github.com/jcsvwinston/GoFrame/pkg/model"
    "myapp/internal/handlers"
    "myapp/internal/models"
)

func main() {
    // 1. Config (from env vars and/or goframe.yaml)
    cfg, _ := app.LoadConfig("goframe.yaml")

    // 2. App
    a, _ := app.New(cfg)

    // 3. Register models
    a.Models.Register(&models.User{}, model.ModelConfig{
        Icon: "user",
        Admin: model.AdminConfig{
            ListFields:   []string{"ID", "Email", "Name", "Role", "CreatedAt"},
            SearchFields: []string{"Email", "Name"},
            Filters:      []string{"Role", "Active"},
        },
    })
    a.Models.Register(&models.Product{})
    a.Models.Register(&models.Order{})

    // 4. Admin (auto-detects models from registry)
    a.Admin = admin.NewPanel(a.DB, a.Models, admin.PanelConfig{
        Title: "My Store Admin",
    })

    // 5. Application routes
    a.Router.Route("/api/v1", func(r chi.Router) {
        r.Use(a.Auth.JWTMiddleware())
        r.Mount("/users", handlers.UserRoutes(a))
        r.Mount("/products", handlers.ProductRoutes(a))
    })

    // 6. Mount admin
    a.Router.Mount(cfg.AdminPrefix, a.Admin.Handler())

    // 7. Run
    a.Run(context.Background())
}
```

---

## 20. CONVENTIONS FOR CLAUDE CODE

1. **Every Go file starts with a package comment** explaining purpose.
2. **Interfaces before structs** in each file.
3. **Constructors are named `New`** (e.g., `NewPanel`, `NewCRUD`).
4. **Wrap errors with context**: `fmt.Errorf("admin.ListRecords model=%s: %w", name, err)`.
5. **Context as first parameter** in every I/O function.
6. **Tests in same package** with `_test.go`; table-driven tests preferred.
7. **No `init()`** except DB driver registration.
8. **Every config value has a sensible dev default**.
9. **Logging**: use `slog.Info/Warn/Error` key-value style, never `fmt.Printf`.
10. **Use English for both code and documentation**.

---
