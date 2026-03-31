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
		return fmt.Errorf("usage: goframe new <project_name> [--module example.com/name] [--out .] [--port 8080]")
	}
	if *port <= 0 {
		return fmt.Errorf("port must be greater than 0")
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
		{relPath: filepath.Join("cmd", "server", "main.go"), body: fmt.Sprintf(newMainTemplate, module, module, projectName)},
		{relPath: filepath.Join("internal", "models", "article.go"), body: newArticleModelTemplate},
		{relPath: filepath.Join("internal", "controllers", "home_page.go"), body: newHomePageTemplate},
		{relPath: filepath.Join("internal", "controllers", "article_api.go"), body: newArticleAPITemplate},
		{relPath: filepath.Join("internal", "web", "templates", "home.html"), body: newHomeHTMLTemplate},
		{relPath: filepath.Join("migrations", "000001_create_articles.up.sql"), body: newMigrationUpTemplate},
		{relPath: filepath.Join("migrations", "000001_create_articles.down.sql"), body: newMigrationDownTemplate},
		{relPath: filepath.Join("seeds", "001_articles.sql"), body: newSeedTemplate},
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

go 1.23
`

const newConfigTemplate = `database_engine: bun
database_url: sqlite://app.db
host: 0.0.0.0
port: %d
env: development
log_level: info
log_format: text
admin_prefix: /admin
admin_title: GoFrame Admin
`

const newGitignoreTemplate = `app.db
*.db
`

const newReadmeTemplate = `# %s

Proyecto generado con goframe CLI.

## Arranque rapido

1. go mod tidy
2. go run ./cmd/server

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

	a.Router.Get("/", controllers.HomePage(tpl))
	a.Router.Get("/api/health", controllers.Health)
	a.Router.Get("/api/articles", controllers.ListArticles(sqlDB))
	a.Router.Post("/api/articles", controllers.CreateArticle(sqlDB))
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
	"database/sql"
	"net/http"
	"time"

	gfrender "github.com/jcsvwinston/GoFrame/pkg/router"
)

type createArticleInput struct {
	Title     string ` + "`json:\"title\" validate:\"required,min=3\"`" + `
	Content   string ` + "`json:\"content\"`" + `
	Published bool   ` + "`json:\"published\"`" + `
}

type articleDTO struct {
	ID        int64     ` + "`json:\"id\"`" + `
	Title     string    ` + "`json:\"title\"`" + `
	Content   string    ` + "`json:\"content\"`" + `
	Published bool      ` + "`json:\"published\"`" + `
	CreatedAt time.Time ` + "`json:\"created_at\"`" + `
	UpdatedAt time.Time ` + "`json:\"updated_at\"`" + `
}

func Health(w http.ResponseWriter, _ *http.Request) {
	gfrender.JSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func ListArticles(sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := sqlDB.QueryContext(
			r.Context(),
			` + "`SELECT id, title, content, published, created_at, updated_at FROM articles ORDER BY id DESC LIMIT 100`" + `,
		)
		if err != nil {
			gfrender.Error(w, err)
			return
		}
		defer rows.Close()

		items := make([]articleDTO, 0, 16)
		for rows.Next() {
			var it articleDTO
			if err := rows.Scan(&it.ID, &it.Title, &it.Content, &it.Published, &it.CreatedAt, &it.UpdatedAt); err != nil {
				gfrender.Error(w, err)
				return
			}
			items = append(items, it)
		}
		if err := rows.Err(); err != nil {
			gfrender.Error(w, err)
			return
		}

		gfrender.JSON(w, http.StatusOK, map[string]any{
			"items": items,
			"total": len(items),
		})
	}
}

func CreateArticle(sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in createArticleInput
		if err := gfrender.Bind(r, &in); err != nil {
			gfrender.Error(w, err)
			return
		}

		now := time.Now().UTC()
		res, err := sqlDB.ExecContext(
			r.Context(),
			` + "`INSERT INTO articles (created_at, updated_at, title, content, published) VALUES (?, ?, ?, ?, ?)`" + `,
			now, now, in.Title, in.Content, in.Published,
		)
		if err != nil {
			gfrender.Error(w, err)
			return
		}
		id, err := res.LastInsertId()
		if err != nil {
			gfrender.Error(w, err)
			return
		}

		gfrender.Created(w, map[string]any{
			"id":         id,
			"title":      in.Title,
			"content":    in.Content,
			"published":  in.Published,
			"created_at": now,
			"updated_at": now,
		})
	}
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
