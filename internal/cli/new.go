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
	port := fs.Int("port", 8080, "HTTP port in nucleus.yml")
	force := fs.Bool("force", false, "Overwrite scaffold files if the project directory exists")
	templateName := fs.String("template", "mvc", "Starter template (mvc: full-stack, api: lightweight core-only)")

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
		return fmt.Errorf("usage: nucleus new <project_name> [--module example.com/name] [--out .] [--port 8080] [--template mvc]")
	}
	if *port <= 0 {
		return fmt.Errorf("port must be greater than 0")
	}
	tmpl := strings.TrimSpace(strings.ToLower(*templateName))
	if tmpl != "mvc" && tmpl != "api" {
		return fmt.Errorf("unsupported template %q (supported: mvc, api)", *templateName)
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
		slug = "nucleus_app"
	}

	frameworkVersion := resolveFrameworkVersion()

	var files []struct {
		relPath string
		body    string
	}

	switch tmpl {
	case "api":
		files = []struct {
			relPath string
			body    string
		}{
			{relPath: "go.mod", body: fmt.Sprintf(newGoModTemplate, module, frameworkVersion)},
			{relPath: "nucleus.yml", body: fmt.Sprintf(newAPIConfigTemplate, *port)},
			{relPath: ".gitignore", body: newGitignoreTemplate},
			{relPath: "README.md", body: fmt.Sprintf(newReadmeTemplate, projectName)},
			{relPath: filepath.Join("cmd", "server", "main.go"), body: fmt.Sprintf(newAPIMainTemplate, module, module, module, module, projectName)},
			{relPath: filepath.Join("internal", "models", "article.go"), body: newArticleModelTemplate},
			{relPath: filepath.Join("internal", "controllers", "article_api.go"), body: fmt.Sprintf(newArticleAPITemplate, module)},
			{relPath: filepath.Join("internal", "contracts", "contracts.go"), body: fmt.Sprintf(contractsAggregatorTemplate, defaultOpenAPITitle(projectName, module, projectDir))},
			{relPath: filepath.Join("internal", "contracts", "article_contract.go"), body: newArticleContractTemplate},
			{relPath: filepath.Join("internal", "services", "article_service.go"), body: fmt.Sprintf(newArticleServiceTemplate, module)},
			{relPath: filepath.Join("internal", "repositories", "article_repository.go"), body: newArticleRepositoryTemplate},
			{relPath: filepath.Join("migrations", "000001_create_articles.up.sql"), body: newMigrationUpTemplate},
			{relPath: filepath.Join("migrations", "000001_create_articles.down.sql"), body: newMigrationDownTemplate},
		}
	default: // mvc
		files = []struct {
			relPath string
			body    string
		}{
			{relPath: "go.mod", body: fmt.Sprintf(newGoModTemplate, module, frameworkVersion)},
			{relPath: "nucleus.yml", body: fmt.Sprintf(newConfigTemplate, *port)},
			{relPath: ".gitignore", body: newGitignoreTemplate},
			{relPath: "README.md", body: fmt.Sprintf(newReadmeTemplate, projectName)},
			{relPath: filepath.Join("cmd", "server", "main.go"), body: fmt.Sprintf(newMainTemplate, module, module, module, module, module, projectName)},
			{relPath: filepath.Join("cmd", "worker", "main.go"), body: fmt.Sprintf(newWorkerTemplate, module, module, module)},
			{relPath: filepath.Join("internal", "models", "article.go"), body: newArticleModelTemplate},
			{relPath: filepath.Join("internal", "controllers", "home_page.go"), body: newHomePageTemplate},
			{relPath: filepath.Join("internal", "controllers", "article_api.go"), body: fmt.Sprintf(newArticleAPITemplate, module)},
			{relPath: filepath.Join("internal", "contracts", "contracts.go"), body: fmt.Sprintf(contractsAggregatorTemplate, defaultOpenAPITitle(projectName, module, projectDir))},
			{relPath: filepath.Join("internal", "contracts", "article_contract.go"), body: newArticleContractTemplate},
			{relPath: filepath.Join("internal", "services", "article_service.go"), body: fmt.Sprintf(newArticleServiceTemplate, module)},
			{relPath: filepath.Join("internal", "repositories", "article_repository.go"), body: newArticleRepositoryTemplate},
			{relPath: filepath.Join("internal", "tasks", "article_events.go"), body: fmt.Sprintf(newTaskHandlersTemplate, module)},
			{relPath: filepath.Join("internal", "web", "templates", "home.html"), body: newHomeHTMLTemplate},
			{relPath: filepath.Join("migrations", "000001_create_articles.up.sql"), body: newMigrationUpTemplate},
			{relPath: filepath.Join("migrations", "000001_create_articles.down.sql"), body: newMigrationDownTemplate},
			{relPath: filepath.Join("seeds", "001_articles.sql"), body: newSeedTemplate},
		}
	}

	// Keep the generated project aligned with the documented default layout even
	// when some layers do not contain files yet.
	extraDirs := []string{
		filepath.Join(projectDir, "internal", "contracts"),
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

	fmt.Fprintf(stdout, "Project scaffold created: %s (template: %s)\n", projectDir, tmpl)
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "Next steps:\n")
	fmt.Fprintf(stdout, "  cd %s\n", projectDir)
	fmt.Fprintf(stdout, "  go mod tidy\n")
	fmt.Fprintf(stdout, "  go run ./cmd/server\n")
	if tmpl == "mvc" {
		fmt.Fprintf(stdout, "\n")
		fmt.Fprintf(stdout, "Optional (requires Redis):\n")
		fmt.Fprintf(stdout, "  go run ./cmd/worker\n")
	}
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "Maintenance (no local Nucleus source needed):\n")
	fmt.Fprintf(stdout, "  go run github.com/jcsvwinston/nucleus/cmd/nucleus@latest migrate --config nucleus.yml\n")
	fmt.Fprintf(stdout, "  go run github.com/jcsvwinston/nucleus/cmd/nucleus@latest seed --config nucleus.yml --seeds seeds\n")
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "Access:\n")
	if tmpl == "api" {
		fmt.Fprintf(stdout, "  API:     http://localhost:%d/api/articles\n", *port)
		fmt.Fprintf(stdout, "  Health:  http://localhost:%d/health\n", *port)
		fmt.Fprintf(stdout, "  OpenAPI: http://localhost:%d/openapi.json\n", *port)
		fmt.Fprintf(stdout, "\n")
		fmt.Fprintf(stdout, "Note: This is a lightweight API-only scaffold.\n")
		fmt.Fprintf(stdout, "  Admin panel, file storage, and mail are not included.\n")
		fmt.Fprintf(stdout, "  You can add subsystems via app.WithExtensions() in main.go.\n")
	} else {
		fmt.Fprintf(stdout, "  Web:   http://localhost:%d/\n", *port)
		fmt.Fprintf(stdout, "  API:   http://localhost:%d/api/articles\n", *port)
		fmt.Fprintf(stdout, "  Admin: http://localhost:%d/admin\n", *port)
	}
	return nil
}

func defaultModulePath(projectName string) string {
	slug := toSnakeCase(projectName)
	if slug == "" {
		slug = "nucleus_app"
	}
	return "example.com/" + slug
}

// resolveFrameworkVersion returns the module version string to use in generated
// go.mod files. When the CLI was built with a release tag (e.g. "0.5.5" via
// goreleaser ldflags), we use "v" + Version. For development builds ("dev"),
// we use "latest" so `go mod tidy` resolves the newest published tag.
func resolveFrameworkVersion() string {
	v := strings.TrimSpace(Version)
	if v == "" || v == "dev" {
		return "latest"
	}
	// goreleaser sets Version without the "v" prefix (e.g. "0.5.5").
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

const newGoModTemplate = `module %s

go 1.25

require github.com/jcsvwinston/nucleus %s
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
admin_title: Nucleus Admin
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

Proyecto generado con nucleus CLI.

## Arranque rapido

1. go mod tidy
2. go run ./cmd/server
3. go run ./cmd/worker  # opcional (requiere Redis)

Accesos:

- App: http://localhost:8080/
- API: http://localhost:8080/api/articles
- OpenAPI JSON: http://localhost:8080/openapi.json
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
	"%s/internal/contracts"
	"%s/internal/models"
	"%s/internal/repositories"
	"%s/internal/services"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

func main() {
	cfg, err := app.LoadConfig("nucleus.yml")
	if err != nil {
		log.Fatal(err)
	}

	a, err := app.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	// The framework mounts a default-deny RBAC middleware per ADR-004.
	// Grant anonymous access to the public surface of this scaffolded
	// project so unauthenticated callers can hit /, /api/* and the
	// OpenAPI document. Production apps replace these blanket allows
	// with a real policy file via admin_rbac_policy_file.
	for _, path := range []string{"/", "/api/*", "/openapi.json", "/health"} {
		if err := a.Authorizer.AddPolicy("anonymous", path, "*"); err != nil {
			log.Fatalf("seed anonymous allow for %%s: %%v", path, err)
		}
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
	if err := a.MountOpenAPI("/openapi.json", contracts.NewDocument); err != nil {
		log.Fatal(err)
	}
	a.Router.Get("/health", func(c *router.Context) error {
		return c.JSON(http.StatusOK, "ok")
	})

	log.Println("%s running:")
	log.Printf("  web:   http://localhost:%%d/\n", cfg.Port)
	log.Printf("  api:   http://localhost:%%d/api/articles\n", cfg.Port)
	log.Printf("  openapi: http://localhost:%%d/openapi.json\n", cfg.Port)
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
		now, now, "Welcome to Nucleus", "This record is editable from /admin and visible via /api/articles.", true,
	)
	return err
}
`

const newWorkerTemplate = `package main

import (
	"context"
	"log"

	"%s/internal/repositories"
	"%s/internal/services"
	projecttasks "%s/internal/tasks"
	"github.com/jcsvwinston/nucleus/pkg/app"
	gftasks "github.com/jcsvwinston/nucleus/pkg/tasks"
	asynqprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/asynq"
)

func main() {
	cfg, err := app.LoadConfig("nucleus.yml")
	if err != nil {
		log.Fatal(err)
	}
	if cfg.RedisURL == "" {
		log.Fatal("redis_url is required to run worker")
	}

	a, err := app.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	sqlDB, err := a.DB.SqlDB()
	if err != nil {
		log.Fatal(err)
	}

	articleRepository := repositories.NewArticleRepository(sqlDB)
	articleService := services.NewArticleService(articleRepository)

	manager, err := asynqprovider.NewManager(gftasks.Config{
		RedisURL:    cfg.RedisURL,
		Concurrency: 10,
		Queues:      map[string]int{"default": 1},
	}, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer manager.Close()

	if err := projecttasks.Register(manager, articleService); err != nil {
		log.Fatal(err)
	}

	log.Println("Worker listening for background tasks")
	if err := manager.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
`

const newArticleModelTemplate = `package models

import "github.com/jcsvwinston/nucleus/pkg/model"

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

	gfrender "github.com/jcsvwinston/nucleus/pkg/router"
)

func HomePage(tpl *template.Template) gfrender.Handler {
	return func(c *gfrender.Context) error {
		return c.HTML(http.StatusOK, "home.html", map[string]any{
			"Title": "Nucleus Starter",
		})
	}
}
`

const newArticleAPITemplate = `package controllers

import (
	"net/http"

	"%s/internal/services"
	gfrender "github.com/jcsvwinston/nucleus/pkg/router"
)

type createArticleInput struct {
	Title     string ` + "`json:\"title\" validate:\"required,min=3\"`" + `
	Content   string ` + "`json:\"content\"`" + `
	Published bool   ` + "`json:\"published\"`" + `
}

func Health(c *gfrender.Context) error {
	return c.JSON(http.StatusOK, map[string]any{"status": "ok"})
}

func ListArticles(articleService *services.ArticleService) gfrender.Handler {
	return func(c *gfrender.Context) error {
		items, err := articleService.List(c.Request.Context(), services.ListArticleInput{
			Query: c.Query("q"),
		})
		if err != nil {
			return err
		}

		return c.JSON(http.StatusOK, map[string]any{
			"data":  items,
			"count": len(items),
		})
	}
}

func CreateArticle(articleService *services.ArticleService) gfrender.Handler {
	return func(c *gfrender.Context) error {
		var in createArticleInput
		if err := c.Bind(&in); err != nil {
			return err
		}

		item, err := articleService.Create(c.Request.Context(), services.CreateArticleInput{
			Title:     in.Title,
			Content:   in.Content,
			Published: in.Published,
		})
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, map[string]any{
			"data": item,
		})
	}
}
`

const newArticleServiceTemplate = `package services

import (
	"context"
	"strings"
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

type ListArticleInput struct {
	Query string
}

type CreateArticleInput struct {
	Title     string
	Content   string
	Published bool
}

type RecordArticleCreatedInput struct {
	ArticleID int64
	Title     string
}

type ArticleRepository interface {
	List(ctx context.Context, params repositories.ListArticleParams) ([]repositories.Article, error)
	Create(ctx context.Context, params repositories.CreateArticleParams) (repositories.Article, error)
}

type ArticleService struct {
	repository ArticleRepository
}

func NewArticleService(repository ArticleRepository) *ArticleService {
	return &ArticleService{repository: repository}
}

func (s *ArticleService) List(ctx context.Context, input ListArticleInput) ([]Article, error) {
	records, err := s.repository.List(ctx, repositories.ListArticleParams{
		Query: strings.TrimSpace(input.Query),
	})
	if err != nil {
		return nil, err
	}

	items := make([]Article, 0, len(records))
	for _, record := range records {
		items = append(items, articleFromRepository(record))
	}
	return items, nil
}

func (s *ArticleService) Create(ctx context.Context, in CreateArticleInput) (Article, error) {
	record, err := s.repository.Create(ctx, repositories.CreateArticleParams{
		Title:     in.Title,
		Content:   in.Content,
		Published: in.Published,
	})
	if err != nil {
		return Article{}, err
	}

	return articleFromRepository(record), nil
}

func (s *ArticleService) RecordCreated(_ context.Context, input RecordArticleCreatedInput) error {
	_ = input
	return nil
}

func articleFromRepository(record repositories.Article) Article {
	return Article{
		ID:        record.ID,
		Title:     record.Title,
		Content:   record.Content,
		Published: record.Published,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
}
`

const newArticleContractTemplate = `package contracts

import "github.com/jcsvwinston/nucleus/pkg/openapi"

func init() {
	RegisterContract(RegisterArticleContract)
}

func RegisterArticleContract(doc *openapi.Document) {
	doc.AddSchema("ArticleRecord", openapi.ObjectSchema(map[string]openapi.Schema{
		"id":        openapi.IDSchema(),
		"title":     {Type: "string"},
		"content":   {Type: "string"},
		"published": {Type: "boolean"},
	}, "id", "title", "published"))

	doc.AddSchema("CreateArticleInput", openapi.ObjectSchema(map[string]openapi.Schema{
		"title":     {Type: "string"},
		"content":   {Type: "string"},
		"published": {Type: "boolean"},
	}, "title"))

	doc.EnsurePaths()
	doc.Paths["/api/articles"] = openapi.PathItem{
		Get: &openapi.Operation{
			OperationID: "listArticles",
			Summary:     "List articles",
			Description: "Returns the scaffolded article collection.",
			Tags:        []string{"articles"},
			Parameters: []openapi.Parameter{
				openapi.SearchQueryParameter("Filter articles by title or content."),
			},
			Responses: map[string]openapi.Response{
				"200": openapi.JSONResponse("Article collection", openapi.CollectionEnvelopeSchema(openapi.RefSchema("ArticleRecord"))),
				"500": openapi.ErrorResponse("Unexpected error"),
			},
		},
		Post: &openapi.Operation{
			OperationID: "createArticle",
			Summary:     "Create article",
			Description: "Creates a scaffolded article resource.",
			Tags:        []string{"articles"},
			RequestBody: openapi.JSONRequestBody(openapi.RefSchema("CreateArticleInput"), true),
			Responses: map[string]openapi.Response{
				"201": openapi.JSONResponse("Created article", openapi.DataEnvelopeSchema(openapi.RefSchema("ArticleRecord"))),
				"400": openapi.ErrorResponse("Invalid request"),
				"500": openapi.ErrorResponse("Unexpected error"),
			},
		},
	}
}
`

const newArticleRepositoryTemplate = `package repositories

import (
	"context"
	"database/sql"
	"strings"
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

type ListArticleParams struct {
	Query string
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

func (r *ArticleRepository) List(ctx context.Context, params ListArticleParams) ([]Article, error) {
	query := ` + "`SELECT id, title, content, published, created_at, updated_at FROM articles`" + `
	args := make([]any, 0, 2)
	if search := strings.TrimSpace(params.Query); search != "" {
		like := "%" + search + "%"
		query += ` + "` WHERE title LIKE ? OR content LIKE ?`" + `
		args = append(args, like, like)
	}
	query += ` + "` ORDER BY id DESC LIMIT 100`" + `

	rows, err := r.db.QueryContext(ctx, query, args...)
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

	"%s/internal/services"
	gftasks "github.com/jcsvwinston/nucleus/pkg/tasks"
)

const TaskArticleCreated = "articles.created"

type ArticleCreatedPayload struct {
	ArticleID int64  ` + "`json:\"article_id\"`" + `
	Title     string ` + "`json:\"title\"`" + `
}

func Register(manager gftasks.Manager, articleService *services.ArticleService) error {
	return manager.HandleFunc(TaskArticleCreated, func(ctx context.Context, task gftasks.Task) error {
		return handleArticleCreated(ctx, task, articleService)
	})
}

func handleArticleCreated(ctx context.Context, task gftasks.Task, articleService *services.ArticleService) error {
	var payload ArticleCreatedPayload
	if err := gftasks.DecodeJSONPayload(task, &payload); err != nil {
		return err
	}

	return articleService.RecordCreated(ctx, services.RecordArticleCreatedInput{
		ArticleID: payload.ArticleID,
		Title:     payload.Title,
	})
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
      <p>Starter Nucleus generado por CLI.</p>
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
VALUES (CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'Seed Article', 'Seed inserted by nucleus starter', 1);`

const newAPIConfigTemplate = `database_default: default
databases:
  default:
    url: sqlite://app.db
    max_open: 25
    max_idle: 5
    max_lifetime: 5m
host: 0.0.0.0
port: %d
env: development
log_level: info
log_format: text
`

const newAPIMainTemplate = `package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"

	"%s/internal/controllers"
	"%s/internal/contracts"
	"%s/internal/repositories"
	"%s/internal/services"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

func main() {
	cfg, err := app.LoadConfig("nucleus.yml")
	if err != nil {
		log.Fatal(err)
	}

	// WithoutDefaults() creates a lightweight core-only app:
	// config + logger + router + DB + sessions + models.
	// No admin panel, no file storage, no mail, no RBAC.
	a, err := app.New(cfg, app.WithoutDefaults())
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

	articleRepository := repositories.NewArticleRepository(sqlDB)
	articleService := services.NewArticleService(articleRepository)

	a.Router.Get("/api/articles", controllers.ListArticles(articleService))
	a.Router.Post("/api/articles", controllers.CreateArticle(articleService))
	a.Router.Get("/health", func(c *router.Context) error {
		return c.JSON(http.StatusOK, "ok")
	})

	if err := a.MountOpenAPI("/openapi.json", contracts.NewDocument); err != nil {
		log.Fatal(err)
	}

	log.Println("%s API running:")
	log.Printf("  api:     http://localhost:%%d/api/articles\n", cfg.Port)
	log.Printf("  health:  http://localhost:%%d/health\n", cfg.Port)
	log.Printf("  openapi: http://localhost:%%d/openapi.json\n", cfg.Port)
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
`
