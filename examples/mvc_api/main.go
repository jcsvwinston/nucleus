package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/app"
	"github.com/jcsvwinston/GoFrame/pkg/model"
	gfrender "github.com/jcsvwinston/GoFrame/pkg/router"
)

//go:embed templates/*.html
var templateFS embed.FS

type Article struct {
	model.BaseModel
	Title     string `db:"column:title;required" validate:"required,min=3" admin:"list,search"`
	Content   string `db:"column:content" admin:"list"`
	Published bool   `db:"column:published" admin:"list,filter"`
}

type createArticleInput struct {
	Title     string `json:"title" validate:"required,min=3"`
	Content   string `json:"content"`
	Published bool   `json:"published"`
}

type articleDTO struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Published bool      `json:"published"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func main() {
	a, err := newExampleApp(nil)
	if err != nil {
		log.Fatal(err)
	}

	port := a.Config.Port
	log.Println("Example running:")
	log.Printf("  web:   http://localhost:%d/\n", port)
	log.Printf("  api:   http://localhost:%d/api/articles\n", port)
	log.Printf("  admin: http://localhost:%d/admin\n", port)

	if err := a.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func defaultExampleConfig() *app.Config {
	return &app.Config{
		Host:            "0.0.0.0",
		Port:            8090,
		DatabaseDefault: "default",
		Databases: map[string]app.DatabaseConfig{
			"default": {
				URL:         "sqlite://examples_mvc_api.db",
				MaxOpen:     10,
				MaxIdle:     5,
				MaxLifetime: 5 * time.Minute,
			},
		},
		AdminPrefix: "/admin",
		AdminTitle:  "GoFrame Example Admin",
		LogLevel:    "info",
		LogFormat:   "text",
	}
}

func newExampleApp(cfg *app.Config) (*app.App, error) {
	if cfg == nil {
		cfg = defaultExampleConfig()
	}

	a, err := app.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("create app: %w", err)
	}

	sqlDB, err := a.DB.SqlDB()
	if err != nil {
		return nil, fmt.Errorf("sql db: %w", err)
	}
	if err := ensureSchema(sqlDB); err != nil {
		return nil, fmt.Errorf("ensure schema: %w", err)
	}
	if err := ensureSeed(sqlDB); err != nil {
		return nil, fmt.Errorf("ensure seed: %w", err)
	}

	if err := a.RegisterModel(&Article{}, model.ModelConfig{
		Icon:         "document",
		ListFields:   []string{"ID", "Title", "Published", "CreatedAt"},
		SearchFields: []string{"Title", "Content"},
		Filters:      []string{"Published"},
		OrderBy:      "created_at desc",
	}); err != nil {
		return nil, fmt.Errorf("register model: %w", err)
	}

	tpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	a.Router.Get("/", homeHandler(tpl))
	a.Router.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		gfrender.JSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	a.Router.Get("/api/articles", listArticlesHandler(sqlDB))
	a.Router.Post("/api/articles", createArticleHandler(a, sqlDB))

	return a, nil
}

func ensureSchema(sqlDB *sql.DB) error {
	_, err := sqlDB.Exec(`
		CREATE TABLE IF NOT EXISTS articles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME,
			title TEXT NOT NULL,
			content TEXT,
			published BOOLEAN NOT NULL DEFAULT 0
		)
	`)
	return err
}

func ensureSeed(sqlDB *sql.DB) error {
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	now := time.Now().UTC()
	_, err := sqlDB.Exec(
		`INSERT INTO articles (created_at, updated_at, title, content, published) VALUES (?, ?, ?, ?, ?)`,
		now, now, "Welcome to GoFrame", "This record is editable from /admin and visible via /api/articles.", true,
	)
	return err
}

func homeHandler(tpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = tpl.ExecuteTemplate(w, "home.html", map[string]any{
			"Title": "GoFrame MVC + API Example",
		})
	}
}

func listArticlesHandler(sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := sqlDB.QueryContext(
			r.Context(),
			`SELECT id, title, content, published, created_at, updated_at FROM articles ORDER BY id DESC LIMIT 100`,
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

func createArticleHandler(a *app.App, sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in createArticleInput
		if err := gfrender.Bind(r, &in); err != nil {
			gfrender.Error(w, err, a.Logger)
			return
		}

		now := time.Now().UTC()
		res, err := sqlDB.ExecContext(
			r.Context(),
			`INSERT INTO articles (created_at, updated_at, title, content, published) VALUES (?, ?, ?, ?, ?)`,
			now, now, in.Title, in.Content, in.Published,
		)
		if err != nil {
			gfrender.Error(w, err, a.Logger)
			return
		}
		id, err := res.LastInsertId()
		if err != nil {
			gfrender.Error(w, err, a.Logger)
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
