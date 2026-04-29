package db

import (
	"database/sql"
	"time"
)

// Seed handles initial data population for the showcase demo.
func Seed(sqlDB *sql.DB) error {
	var count int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM articles").Scan(&count); err != nil {
		// Table might not exist yet if automigrate hasn't run, but QuickStart calls it first.
		return err
	}
	if count > 0 {
		return nil
	}

	now := time.Now().UTC()

	// Insert Authors
	authors := []struct {
		name, email, bio, position, avatar, github, twitter string
	}{
		{"María García", "maria@example.com", "Full-stack developer con 10 años de experiencia en Go y React. Apasionada por el código limpio y las arquitecturas escalables.", "Lead Developer", "/static/images/authors/maria.jpg", "mariagarcia", "mariagarcia"},
		{"Carlos Ruiz", "carlos@example.com", "DevOps engineer especializado en Kubernetes, CI/CD y cloud infrastructure. AWS Certified Solutions Architect.", "DevOps Lead", "/static/images/authors/carlos.jpg", "cruiz", "cruizdev"},
		{"Ana Martínez", "ana@example.com", "UX/UI Designer con enfoque en accesibilidad y diseño inclusivo. Creadora de interfaces intuitivas y hermosas.", "Lead Designer", "/static/images/authors/ana.jpg", "anamtz", "anadesigns"},
		{"Pedro Sánchez", "pedro@example.com", "Backend developer especializado en bases de datos, APIs RESTful y sistemas distribuidos.", "Senior Backend", "/static/images/authors/pedro.jpg", "pedrosan", "pedrosan"},
	}

	authorIDs := make([]int64, len(authors))
	for i, a := range authors {
		res, err := sqlDB.Exec(
			"INSERT INTO authors (created_at, updated_at, name, email, bio, position, avatar_url, social_github, social_twitter) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			now, now, a.name, a.email, a.bio, a.position, a.avatar, a.github, a.twitter,
		)
		if err != nil {
			return err
		}
		authorIDs[i], _ = res.LastInsertId()
	}

	// Insert Categories
	categories := []struct {
		name, slug, desc, color, icon string
	}{
		{"Tecnología", "tecnologia", "Artículos sobre tecnología, programación y desarrollo de software.", "#3b82f6", "code"},
		{"Tutoriales", "tutoriales", "Guías paso a paso para aprender nuevas tecnologías y herramientas.", "#10b981", "book-open"},
		{"Opinión", "opinion", "Reflexiones y opiniones sobre el mundo tech y la industria.", "#f59e0b", "message-circle"},
		{"Noticias", "noticias", "Últimas novedades y actualizaciones del mundo tecnológico.", "#ef4444", "newspaper"},
		{"Recursos", "recursos", "Herramientas, librerías y recursos útiles para desarrolladores.", "#8b5cf6", "package"},
	}

	categoryIDs := make([]int64, len(categories))
	for i, c := range categories {
		res, err := sqlDB.Exec(
			"INSERT INTO categories (created_at, updated_at, name, slug, description, color, icon) VALUES (?, ?, ?, ?, ?, ?, ?)",
			now, now, c.name, c.slug, c.desc, c.color, c.icon,
		)
		if err != nil {
			return err
		}
		categoryIDs[i], _ = res.LastInsertId()
	}

	// Insert Tags
	tags := []struct {
		name, slug, color string
	}{
		{"Go", "go", "#00add8"},
		{"React", "react", "#61dafb"},
		{"TypeScript", "typescript", "#3178c6"},
		{"Docker", "docker", "#2496ed"},
		{"Kubernetes", "kubernetes", "#326ce5"},
		{"PostgreSQL", "postgresql", "#336791"},
		{"API", "api", "#ff6b6b"},
		{"Microservicios", "microservicios", "#4ecdc4"},
		{"Testing", "testing", "#95e1d3"},
		{"DevOps", "devops", "#f38181"},
	}

	tagIDs := make([]int64, len(tags))
	for i, t := range tags {
		res, err := sqlDB.Exec(
			"INSERT INTO tags (created_at, updated_at, name, slug, color) VALUES (?, ?, ?, ?, ?)",
			now, now, t.name, t.slug, t.color,
		)
		if err != nil {
			return err
		}
		tagIDs[i], _ = res.LastInsertId()
	}

	// Insert Articles
	articles := []struct {
		title, slug, summary, content string
		published                     bool
		authorIdx, categoryIdx        int
		tagIndices                    []int
		viewCount                     int
	}{
		{
			"Introducción a GoFrame: Framework MVC para Go",
			"introduccion-goframe",
			"Descubre GoFrame, el framework MVC moderno que simplifica el desarrollo de aplicaciones web en Go con scaffolding automático.",
			`# Introducción a GoFrame

GoFrame es un framework MVC completo para Go que proporciona:

- **Scaffolding automático** con CLI
- **Admin panel** integrado
- **API RESTful** automática
- **Autenticación y autorización** built-in
- **Migraciones de base de datos**
- **Tareas en background** con queue system

## Características principales

### 1. CLI Potente
El comando ` + "`goframe`" + ` te permite:
- Crear nuevos proyectos
- Generar modelos, controladores y vistas
- Ejecutar migraciones
- Gestionar usuarios admin

### 2. Admin Panel
Panel de administración automático con:
- Data Studio con AG Grid
- Gestión de modelos
- Sistema de RBAC
- Live telemetry

### 3. API Automática
Cada modelo registrado expone automáticamente endpoints RESTful.

¡Empieza hoy con GoFrame!`,
			true,
			0, 0, []int{0, 2, 6}, 1250,
		},
		{
			"Guía Completa de Docker para Desarrolladores",
			"guia-docker-desarrolladores",
			"Aprende a containerizar tus aplicaciones Go con Docker, desde conceptos básicos hasta configuraciones avanzadas.",
			`# Docker para Desarrolladores

## ¿Qué es Docker?

Docker es una plataforma que permite desarrollar, enviar y ejecutar aplicaciones en contenedores.

## Beneficios para desarrolladores

1. **Consistencia**: Mismo entorno en dev, staging y prod
2. **Portabilidad**: Corre en cualquier sistema con Docker
3. **Aislamiento**: Dependencias encapsuladas
4. **Escalabilidad**: Fácil de escalar horizontalmente

## Dockerfile para Go

` + "```dockerfile" + `
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o main .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/main .
CMD ["./main"]
` + "```" + `

## Docker Compose

Configura tu stack completo con un solo archivo.`,
			true,
			1, 1, []int{3, 9}, 890,
		},
		{
			"Patrones de Diseño en Go: Factory y Singleton",
			"patrones-diseno-go",
			"Implementa los patrones Factory y Singleton de forma idiomática en Go, aprovechando sus características únicas.",
			`# Patrones de Diseño en Go

## Factory Pattern

El patrón Factory es ideal cuando necesitas crear objetos sin especificar la clase exacta.

### Ejemplo en Go

` + "```go" + `
type Database interface {
    Connect() error
    Query(sql string) ([]Row, error)
}

type DatabaseFactory struct {
    drivers map[string]func() Database
}

func (f *DatabaseFactory) Register(name string, ctor func() Database) {
    f.drivers[name] = ctor
}

func (f *DatabaseFactory) Create(name string) (Database, error) {
    if ctor, ok := f.drivers[name]; ok {
        return ctor(), nil
    }
    return nil, fmt.Errorf("driver not found: %s", name)
}
` + "```" + `

## Singleton Pattern

Go tiene una forma elegante de implementar singletons:

` + "```go" + `
var (
    once sync.Once
    instance *Config
)

func GetConfig() *Config {
    once.Do(func() {
        instance = loadConfig()
    })
    return instance
}
` + "```" + `

## Conclusión

Go favorece la composición sobre la herencia, haciendo algunos patrones más simples.`,
			true,
			0, 0, []int{0, 6}, 650,
		},
		{
			"Kubernetes: Orquestación de Contenedores",
			"kubernetes-orquestacion",
			"Guía práctica para desplegar aplicaciones Go en Kubernetes, incluyendo Deployments, Services y ConfigMaps.",
			`# Kubernetes para Aplicaciones Go

## Conceptos Fundamentales

### Pods
La unidad más pequeña en Kubernetes. Un Pod puede contener uno o más contenedores.

### Deployments
Gestionan la creación y actualización de Pods:

` + "```yaml" + `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: goframe-app
spec:
  replicas: 3
  selector:
    matchLabels:
      app: goframe
  template:
    metadata:
      labels:
        app: goframe
    spec:
      containers:
      - name: app
        image: goframe:latest
        ports:
        - containerPort: 8080
` + "```" + `

### Services
Exponen tus aplicaciones al tráfico externo:

- ClusterIP: Acceso interno
- NodePort: Exposición en puerto del nodo
- LoadBalancer: Balanceador de carga externo
- Ingress: Enrutamiento HTTP/HTTPS

## Health Checks

Kubernetes puede verificar la salud de tu aplicación:

` + "```go" + `
http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
    // Verificar dependencias
    if err := checkDatabase(); err != nil {
        w.WriteHeader(http.StatusServiceUnavailable)
        return
    }
    w.WriteHeader(http.StatusOK)
})
` + "```" + `

## Escalado automático

El Horizontal Pod Autoscaler ajusta réplicas según carga.`,
			true,
			1, 0, []int{4, 9}, 720,
		},
		{
			"PostgreSQL: Optimización de Consultas",
			"postgresql-optimizacion",
			"Técnicas avanzadas para optimizar consultas PostgreSQL en aplicaciones de alto rendimiento.",
			`# Optimización de PostgreSQL

## Índices Efectivos

Los índices son cruciales para el rendimiento:

` + "```sql" + `
-- Índice simple
CREATE INDEX idx_users_email ON users(email);

-- Índice compuesto
CREATE INDEX idx_orders_user_date ON orders(user_id, created_at);

-- Índice parcial
CREATE INDEX idx_active_users ON users(email) WHERE active = true;
` + "```" + `

## EXPLAIN ANALYZE

Entiende el plan de ejecución:

` + "```sql" + `
EXPLAIN ANALYZE SELECT * FROM orders WHERE user_id = 123;
` + "```" + `

## Consultas N+1

Problema común en ORMs:

` + "```go" + `
// MALO: N+1 queries
for _, user := range users {
    orders := db.GetOrders(user.ID) // Query por usuario
}

// BUENO: Single query con JOIN
rows, err := db.Query(` + "`" + `
    SELECT u.*, o.* 
    FROM users u 
    LEFT JOIN orders o ON u.id = o.user_id 
    WHERE u.active = true
` + "`" + `)
` + "```" + `

## Connection Pooling

Configura el pool de conexiones adecuadamente:

- max_open_conns: Según capacidad de DB
- max_idle_conns: 25-50% de max_open
- conn_max_lifetime: Menor que timeout de DB`,
			true,
			3, 0, []int{5, 6}, 540,
		},
		{
			"Testing en Go: Unit Tests y Benchmarks",
			"testing-go",
			"Estrategias completas para testing en Go, desde unit tests hasta benchmarks de rendimiento.",
			`# Testing en Go

## Unit Tests

Go tiene testing built-in:

` + "```go" + `
func TestCalculateTotal(t *testing.T) {
    tests := []struct {
        name     string
        items    []Item
        expected float64
    }{
        {
            name:     "empty cart",
            items:    []Item{},
            expected: 0,
        },
        {
            name: "single item",
            items: []Item{{Price: 10, Quantity: 2}},
            expected: 20,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := CalculateTotal(tt.items)
            if result != tt.expected {
                t.Errorf("got %v, want %v", result, tt.expected)
            }
        })
    }
}
` + "```" + `

## Table-Driven Tests

Patrón idiomático en Go para tests concisos.

## Mocks e Interfaces

Usa interfaces para testabilidad:

` + "```go" + `
type DataStore interface {
    GetUser(id int) (*User, error)
    SaveUser(u *User) error
}

type MockStore struct {
    users map[int]*User
}

func (m *MockStore) GetUser(id int) (*User, error) {
    return m.users[id], nil
}
` + "```" + `

## Benchmarks

Mide el rendimiento:

` + "```go" + `
func BenchmarkProcessData(b *testing.B) {
    data := generateTestData()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        ProcessData(data)
    }
}
` + "```" + `

Ejecuta: ` + "`" + `go test -bench=. -benchmem` + "`" + ``,
			true,
			2, 1, []int{0, 8}, 480,
		},
		{
			"API RESTful: Mejores Prácticas",
			"api-restful-mejores-practicas",
			"Diseña APIs RESTful robustas, versionadas y documentadas con OpenAPI/Swagger.",
			`# APIs RESTful: Mejores Prácticas

## Versionado

Incluye la versión en la URL:

- ` + "`/api/v1/users`" + `
- ` + "`/api/v2/users`" + `

## Paginación

Estandariza la respuesta paginada:

` + "```json" + `
{
  "data": [...],
  "meta": {
    "page": 1,
    "per_page": 20,
    "total": 150,
    "total_pages": 8
  },
  "links": {
    "self": "/api/v1/users?page=1",
    "next": "/api/v1/users?page=2",
    "last": "/api/v1/users?page=8"
  }
}
` + "```" + `

## Filtrado y Ordenamiento

Usa query parameters:

- ` + "`/users?role=admin&active=true`" + `
- ` + "`/users?sort=-created_at`" + ` (orden descendente)

## Manejo de Errores

Estructura consistente:

` + "```json" + `
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Invalid input data",
    "details": [
      {"field": "email", "message": "Invalid email format"}
    ]
  }
}
` + "```" + `

## Rate Limiting

Protege tu API:

` + "```go" + `
// Headers informativos
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 99
X-RateLimit-Reset: 1640995200
` + "```" + `

## Documentación

Mantén documentación actualizada con OpenAPI.`,
			true,
			0, 0, []int{0, 6}, 620,
		},
	}

	articleIDs := make([]int64, len(articles))
	for i, art := range articles {
		publishedAt := now
		if !art.published {
			publishedAt = now.Add(-24 * time.Hour)
		}

		res, err := sqlDB.Exec(
			"INSERT INTO articles (created_at, updated_at, title, slug, summary, content, published, published_at, view_count, author_id, category_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			now, now, art.title, art.slug, art.summary, art.content, art.published, publishedAt, art.viewCount,
			authorIDs[art.authorIdx], categoryIDs[art.categoryIdx],
		)
		if err != nil {
			return err
		}
		articleIDs[i], _ = res.LastInsertId()

		// Insert article_tags relationships
		for _, tagIdx := range art.tagIndices {
			_, err = sqlDB.Exec(
				"INSERT INTO article_tags (article_id, tag_id) VALUES (?, ?)",
				articleIDs[i], tagIDs[tagIdx],
			)
			if err != nil {
				return err
			}
		}
	}

	// Insert Comments
	comments := []struct {
		articleIdx             int
		author, email, content string
		approved               bool
	}{
		{0, "Juan Pérez", "juan@example.com", "Excelente artículo, me ayudó mucho a entender GoFrame. Gracias!", true},
		{0, "Laura Gómez", "laura@example.com", "¿Tienen planes para soportar GraphQL en el futuro?", true},
		{1, "Miguel Torres", "miguel@example.com", "Docker ha cambiado completamente mi workflow. Gran guía.", true},
		{2, "Sofia López", "sofia@example.com", "Los ejemplos de código son muy claros. Me encanta Go!", true},
		{3, "Diego Martín", "diego@example.com", "Kubernetes es complejo pero vale la pena aprenderlo.", true},
	}

	for _, c := range comments {
		_, err := sqlDB.Exec(
			"INSERT INTO comments (created_at, updated_at, article_id, author_name, author_email, content, approved) VALUES (?, ?, ?, ?, ?, ?, ?)",
			now, now, articleIDs[c.articleIdx], c.author, c.email, c.content, c.approved,
		)
		if err != nil {
			return err
		}
	}

	// Update counts
	_, err := sqlDB.Exec(`
		UPDATE categories SET article_count = (
			SELECT COUNT(*) FROM articles WHERE category_id = categories.id AND published = 1
		);
		UPDATE tags SET article_count = (
			SELECT COUNT(*) FROM article_tags WHERE tag_id = tags.id
		);
		UPDATE authors SET article_count = (
			SELECT COUNT(*) FROM articles WHERE author_id = authors.id AND published = 1
		);
	`)

	return err
}
