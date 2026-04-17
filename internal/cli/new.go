package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func runNew(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(stderr)

	outDir := fs.String("out", ".", "Parent directory where the project folder will be created")
	modulePath := fs.String("module", "", "Go module path (default: example.com/<project_name>)")
	port := fs.Int("port", 8080, "HTTP port in goframe.yaml")
	force := fs.Bool("force", false, "Overwrite scaffold files if the project directory exists")
	templateName := fs.String("template", "mvc", "Starter template (currently: mvc)")

	projectFirst := ""
	parseArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		projectFirst = strings.TrimSpace(args[0])
		parseArgs = args[1:]
	}

	if err := fs.Parse(parseArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	rest := fs.Args()
	if projectFirst != "" {
		rest = append([]string{projectFirst}, rest...)
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: goframe new <project_name> [--module example.com/name] [--out .] [--port 8080] [--template mvc]")
	}
	if *port <= 0 {
		return fmt.Errorf("port must be greater than 0")
	}
	if strings.TrimSpace(strings.ToLower(*templateName)) != "mvc" {
		return fmt.Errorf("unsupported template %q (supported: mvc)", *templateName)
	}

	projectName := strings.TrimSpace(rest[0])
	if projectName == "" {
		return fmt.Errorf("project name cannot be empty")
	}

	projectDir := filepath.Join(*outDir, projectName)
	if info, err := os.Stat(projectDir); err == nil && !info.IsDir() {
		return fmt.Errorf("target path exists and is not a directory: %s", projectDir)
	} else if err == nil && !*force {
		return fmt.Errorf("project directory already exists: %s (use --force to overwrite scaffold files)", projectDir)
	}
	if err := ensureDir(projectDir); err != nil {
		return err
	}

	module := strings.TrimSpace(*modulePath)
	if module == "" {
		module = defaultModulePath(projectName)
	}

	slug := toSnakeCase(projectName)
	if slug == "" {
		slug = "goframe_app"
	}

	files := []struct {
		relPath string
		body    string
	}{
		{relPath: "go.mod", body: fmt.Sprintf(newGoModTemplate, module)},
		{relPath: "goframe.yaml", body: fmt.Sprintf(newConfigTemplate, *port)},
		{relPath: ".gitignore", body: newGitignoreTemplate},
		{relPath: "README.md", body: fmt.Sprintf(newReadmeTemplate, projectName)},
		{relPath: filepath.Join("cmd", "server", "main.go"), body: fmt.Sprintf(newMainTemplate, module, module, module, module, projectName)},
		{relPath: filepath.Join("cmd", "worker", "main.go"), body: fmt.Sprintf(newWorkerTemplate, module)},
		{relPath: filepath.Join("internal", "models", "article.go"), body: newArticleModelTemplate},
		{relPath: filepath.Join("internal", "controllers", "home_page.go"), body: newHomePageTemplate},
		{relPath: filepath.Join("internal", "controllers", "article_api.go"), body: fmt.Sprintf(newArticleAPITemplate, module)},
		{relPath: filepath.Join("internal", "services", "article_service.go"), body: fmt.Sprintf(newArticleServiceTemplate, module)},
		{relPath: filepath.Join("internal", "repositories", "article_repository.go"), body: newArticleRepositoryTemplate},
		{relPath: filepath.Join("internal", "tasks", "article_events.go"), body: newTaskHandlersTemplate},
		{relPath: filepath.Join("internal", "web", "templates", "home.html"), body: newHomeHTMLTemplate},
		{relPath: filepath.Join("migrations", "000001_create_articles.up.sql"), body: newMigrationUpTemplate},
		{relPath: filepath.Join("migrations", "000001_create_articles.down.sql"), body: newMigrationDownTemplate},
		{relPath: filepath.Join("seeds", "001_articles.sql"), body: newSeedTemplate},
	}

	// Keep the generated project aligned with the documented default layout even
	// when some layers do not contain files yet.
	extraDirs := []string{
		filepath.Join(projectDir, "internal", "services"),
		filepath.Join(projectDir, "internal", "repositories"),
		filepath.Join(projectDir, "internal", "web", "static"),
	}
	for _, dirPath := range extraDirs {
		if err := ensureDir(dirPath); err != nil {
			return err
		}
	}

	for _, f := range files {
		target := filepath.Join(projectDir, f.relPath)
		if err := writeFileIfNotExists(target, strings.TrimSpace(f.body)+"\n", *force); err != nil {
			return err
		}
	}

	fmt.Fprintf(stdout, "Project scaffold created: %s\n", projectDir)
	fmt.Fprintf(stdout, "Next steps:\n")
	fmt.Fprintf(stdout, "  cd %s\n", projectDir)
	fmt.Fprintf(stdout, "  go mod tidy\n")
	fmt.Fprintf(stdout, "  go run ./cmd/server\n")
	fmt.Fprintf(stdout, "  go run ./cmd/worker\n")
	fmt.Fprintf(stdout, "  go run github.com/jcsvwinston/GoFrame/cmd/goframe@latest migrate --config goframe.yaml\n")
	fmt.Fprintf(stdout, "  go run github.com/jcsvwinston/GoFrame/cmd/goframe@latest seed --config goframe.yaml --seeds seeds\n")
	fmt.Fprintf(stdout, "  open http://localhost:%d/admin\n", *port)
	return nil
}

func defaultModulePath(projectName string) string {
	slug := toSnakeCase(projectName)
	if slug == "" {
		slug = "goframe_app"
	}
	return "example.com/" + slug
}

const newGoModTemplate = `module %s

go 1.25
`

const newConfigTemplate = `database_default: default
databases:
  default:
    url: sqlite://app.db
    max_open: 25
    max_idle: 5
    max_lifetime: 5m
redis_url: redis://127.0.0.1:6379/0
host: 0.0.0.0
port: %d
env: development
log_level: info
log_format: text
otlp_endpoint: ""
rate_limit_requests: 0
rate_limit_window: 1m
rate_limit_burst: 0
rate_limit_by_route: false
rate_limit_by_role: false
admin_prefix: /admin
admin_title: GoFrame Admin
admin_auth_database: default
admin_bootstrap_username: admin
admin_bootstrap_email: admin@example.com
admin_bootstrap_password: ""
admin_trace_url_template: ""
multisite:
  enabled: false
  default_site: default
  sites:
    default:
      database: default
multitenant:
  enabled: false
  resolver: subdomain
  header: X-Tenant-ID
  require_isolated_db: true
  database_alias_template: tenant_%%s
`

const newGitignoreTemplate = `app.db
*.db
`

const newReadmeTemplate = `# %s

Proyecto generado con goframe CLI.

## Arranque rapido

1. go mod tidy
2. go run ./cmd/server
3. go run ./cmd/worker  # opcional (requiere Redis)

Accesos:

- App: http://localhost:8080/
- API: http://localhost:8080/api/articles
- Admin: http://localhost:8080/admin
`

const newMainTemplate = `package main

import (
	"context"
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"time"

	"%s/internal/controllers"
	"%s/internal/models"
	"%s/internal/repositories"
	"%s/internal/services"
	"github.com/jcsvwinston/GoFrame/pkg/app"
	"github.com/jcsvwinston/GoFrame/pkg/model"
)

func main() {
	cfg, err := app.LoadConfig("goframe.yaml")
	if err != nil {
		log.Fatal(err)
	}

	a, err := app.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	sqlDB, err := a.DB.SqlDB()
	if err != nil {
		log.Fatal(err)
	}
	if err := ensureSchema(sqlDB); err != nil {
		log.Fatal(err)
	}
	if err := ensureSeed(sqlDB); err != nil {
		log.Fatal(err)
	}

	if err := a.RegisterModel(&models.Article{}, model.ModelConfig{
		Icon:         "document",
		ListFields:   []string{"ID", "Title", "Published", "CreatedAt"},
		SearchFields: []string{"Title", "Content"},
		Filters:      []string{"Published"},
		OrderBy:      "created_at desc",
	}); err != nil {
		log.Fatal(err)
	}

	tpl, err := template.ParseFiles("internal/web/templates/home.html")
	if err != nil {
		log.Fatal(err)
	}

	articleRepository := repositories.NewArticleRepository(sqlDB)
	articleService := services.NewArticleService(articleRepository)

	a.Router.Get("/", controllers.HomePage(tpl))
	a.Router.Get("/api/health", controllers.Health)
	a.Router.Get("/api/articles", controllers.ListArticles(articleService))
	a.Router.Post("/api/articles", controllers.CreateArticle(articleService))
	a.Router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Println("%s running:")
	log.Printf("  web:   http://localhost:%%d/\n", cfg.Port)
	log.Printf("  api:   http://localhost:%%d/api/articles\n", cfg.Port)
	log.Printf("  admin: http://localhost:%%d/admin\n", cfg.Port)
	log.Fatal(a.Run(context.Background()))
}

func ensureSchema(sqlDB *sql.DB) error {
	_, err := sqlDB.Exec(
		"CREATE TABLE IF NOT EXISTS articles (" +
			"id INTEGER PRIMARY KEY AUTOINCREMENT," +
			"created_at DATETIME," +
			"updated_at DATETIME," +
			"deleted_at DATETIME," +
			"title TEXT NOT NULL," +
			"content TEXT," +
			"published BOOLEAN NOT NULL DEFAULT 0" +
			")",
	)
	return err
}

func ensureSeed(sqlDB *sql.DB) error {
	var count int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM articles").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	now := time.Now().UTC()
	_, err := sqlDB.Exec(
		"INSERT INTO articles (created_at, updated_at, title, content, published) VALUES (?, ?, ?, ?, ?)",
		now, now, "Welcome to GoFrame", "This record is editable from /admin and visible via /api/articles.", true,
	)
	return err
}
`

const newWorkerTemplate = `package main

import (
	"context"
	"log"

	projecttasks "%s/internal/tasks"
	"github.com/jcsvwinston/GoFrame/pkg/app"
	gftasks "github.com/jcsvwinston/GoFrame/pkg/tasks"
)

func main() {
	cfg, err := app.LoadConfig("goframe.yaml")
	if err != nil {
		log.Fatal(err)
	}
	if cfg.RedisURL == "" {
		log.Fatal("redis_url is required to run worker")
	}

	manager, err := gftasks.NewManager(gftasks.Config{
		RedisURL:    cfg.RedisURL,
		Concurrency: 10,
		Queues:      map[string]int{"default": 1},
	}, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer manager.Close()

	if err := projecttasks.Register(manager); err != nil {
		log.Fatal(err)
	}

	log.Println("Worker listening for background tasks")
	if err := manager.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
`

const newArticleModelTemplate = `package models

import "github.com/jcsvwinston/GoFrame/pkg/model"

type Article struct {
	model.BaseModel
	Title     string ` + "`db:\"column:title;required\" validate:\"required,min=3\" admin:\"list,search\"`" + `
	Content   string ` + "`db:\"column:content\" admin:\"list\"`" + `
	Published bool   ` + "`db:\"column:published\" admin:\"list,filter\"`" + `
}
`

const newHomePageTemplate = `package controllers

import (
	"html/template"
	"net/http"
)

func HomePage(tpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = tpl.ExecuteTemplate(w, "home.html", map[string]any{
			"Title": "GoFrame Starter",
		})
	}
}
`

const newArticleAPITemplate = `package controllers

import (
	"net/http"

	"%s/internal/services"
	gfrender "github.com/jcsvwinston/GoFrame/pkg/router"
)

type createArticleInput struct {
	Title     string ` + "`json:\"title\" validate:\"required,min=3\"`" + `
	Content   string ` + "`json:\"content\"`" + `
	Published bool   ` + "`json:\"published\"`" + `
}

func Health(w http.ResponseWriter, _ *http.Request) {
	gfrender.JSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func ListArticles(articleService *services.ArticleService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := articleService.List(r.Context())
		if err != nil {
			gfrender.Error(w, err)
			return
		}

		gfrender.JSON(w, http.StatusOK, map[string]any{
			"items": items,
			"total": len(items),
		})
	}
}

func CreateArticle(articleService *services.ArticleService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in createArticleInput
		if err := gfrender.Bind(r, &in); err != nil {
			gfrender.Error(w, err)
			return
		}

		item, err := articleService.Create(r.Context(), services.CreateArticleInput{
			Title:     in.Title,
			Content:   in.Content,
			Published: in.Published,
		})
		if err != nil {
			gfrender.Error(w, err)
			return
		}
		gfrender.Created(w, item)
	}
}
`

const newArticleServiceTemplate = `package services

import (
	"context"
	"time"

	"%s/internal/repositories"
)

type Article struct {
	ID        int64     ` + "`json:\"id\"`" + `
	Title     string    ` + "`json:\"title\"`" + `
	Content   string    ` + "`json:\"content\"`" + `
	Published bool      ` + "`json:\"published\"`" + `
	CreatedAt time.Time ` + "`json:\"created_at\"`" + `
	UpdatedAt time.Time ` + "`json:\"updated_at\"`" + `
}

type CreateArticleInput struct {
	Title     string
	Content   string
	Published bool
}

type ArticleService struct {
	repository *repositories.ArticleRepository
}

func NewArticleService(repository *repositories.ArticleRepository) *ArticleService {
	return &ArticleService{repository: repository}
}

func (s *ArticleService) List(ctx context.Context) ([]repositories.Article, error) {
	return s.repository.List(ctx)
}

func (s *ArticleService) Create(ctx context.Context, in CreateArticleInput) (repositories.Article, error) {
	return s.repository.Create(ctx, repositories.CreateArticleParams{
		Title:     in.Title,
		Content:   in.Content,
		Published: in.Published,
	})
}
`

const newArticleRepositoryTemplate = `package repositories

import (
	"context"
	"database/sql"
	"time"
)

type Article struct {
	ID        int64     ` + "`json:\"id\"`" + `
	Title     string    ` + "`json:\"title\"`" + `
	Content   string    ` + "`json:\"content\"`" + `
	Published bool      ` + "`json:\"published\"`" + `
	CreatedAt time.Time ` + "`json:\"created_at\"`" + `
	UpdatedAt time.Time ` + "`json:\"updated_at\"`" + `
}

type CreateArticleParams struct {
	Title     string
	Content   string
	Published bool
}

type ArticleRepository struct {
	db *sql.DB
}

func NewArticleRepository(db *sql.DB) *ArticleRepository {
	return &ArticleRepository{db: db}
}

func (r *ArticleRepository) List(ctx context.Context) ([]Article, error) {
	rows, err := r.db.QueryContext(
		ctx,
		` + "`SELECT id, title, content, published, created_at, updated_at FROM articles ORDER BY id DESC LIMIT 100`" + `,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Article, 0, 16)
	for rows.Next() {
		var it Article
		if err := rows.Scan(&it.ID, &it.Title, &it.Content, &it.Published, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ArticleRepository) Create(ctx context.Context, params CreateArticleParams) (Article, error) {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(
		ctx,
		` + "`INSERT INTO articles (created_at, updated_at, title, content, published) VALUES (?, ?, ?, ?, ?)`" + `,
		now, now, params.Title, params.Content, params.Published,
	)
	if err != nil {
		return Article{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Article{}, err
	}

	return Article{
		ID:        id,
		Title:     params.Title,
		Content:   params.Content,
		Published: params.Published,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}
`

const newTaskHandlersTemplate = `package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/hibiken/asynq"
	gftasks "github.com/jcsvwinston/GoFrame/pkg/tasks"
)

const TaskArticleCreated = "articles.created"

type ArticleCreatedPayload struct {
	ArticleID int64  ` + "`json:\"article_id\"`" + `
	Title     string ` + "`json:\"title\"`" + `
}

func Register(manager *gftasks.Manager) error {
	return manager.HandleFunc(TaskArticleCreated, handleArticleCreated)
}

func handleArticleCreated(_ context.Context, task *asynq.Task) error {
	var payload ArticleCreatedPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}

	log.Printf("article created event processed: id=%d title=%q", payload.ArticleID, payload.Title)
	return nil
}
`

const newHomeHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{ .Title }}</title>
  <style>
    body {
      margin: 0;
      font-family: "IBM Plex Sans", "Segoe UI", sans-serif;
      color: #132032;
      background: radial-gradient(circle at 12% 18%, #dffaf5 0%, transparent 28%),
        radial-gradient(circle at 86% 84%, #ffe8cf 0%, transparent 32%), #f7f7f2;
    }
    .wrap {
      max-width: 860px;
      margin: 32px auto;
      padding: 0 18px;
    }
    .card {
      background: #fff;
      border: 1px solid #dbe4ec;
      border-radius: 16px;
      box-shadow: 0 14px 42px rgba(20, 35, 53, 0.08);
      padding: 24px;
    }
    h1 {
      margin-top: 0;
      font-size: 28px;
    }
    p {
      color: #4b5d70;
    }
    .links {
      display: grid;
      gap: 10px;
      margin-top: 16px;
    }
    a {
      display: inline-block;
      text-decoration: none;
      font-weight: 600;
      color: #0f766e;
      border: 1px solid #cfe7e4;
      border-radius: 10px;
      background: #f3fbfa;
      padding: 10px 12px;
      width: fit-content;
    }
  </style>
</head>
<body>
  <main class="wrap">
    <section class="card">
      <h1>{{ .Title }}</h1>
      <p>Starter GoFrame generado por CLI.</p>
      <div class="links">
        <a href="/admin">Abrir Admin</a>
        <a href="/api/articles">GET /api/articles</a>
      </div>
    </section>
  </main>
</body>
</html>
`

const newMigrationUpTemplate = `CREATE TABLE IF NOT EXISTS articles (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME,
	updated_at DATETIME,
	deleted_at DATETIME,
	title TEXT NOT NULL,
	content TEXT,
	published BOOLEAN NOT NULL DEFAULT 0
);`

const newMigrationDownTemplate = `DROP TABLE IF EXISTS articles;`

const newSeedTemplate = `INSERT INTO articles (created_at, updated_at, title, content, published)
VALUES (CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'Seed Article', 'Seed inserted by goframe starter', 1);`
