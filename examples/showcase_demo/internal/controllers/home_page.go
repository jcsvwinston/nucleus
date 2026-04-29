package controllers

import (
	"database/sql"
	"html/template"
	"net/http"
	"time"

	gfrender "github.com/jcsvwinston/GoFrame/pkg/router"
)

func HomePage(tpl *template.Template, db *sql.DB) gfrender.Handler {
	return func(c *gfrender.Context) error {
		// Get latest 3 articles
		rows, err := db.Query(`
			SELECT a.title, a.slug, a.summary, a.published_at,
					c.name as category_name, c.color as category_color
			FROM articles a
			JOIN categories c ON a.category_id = c.id
			WHERE a.published = 1
			ORDER BY a.published_at DESC
			LIMIT 3
		`)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		defer rows.Close()

		var articles []map[string]interface{}
		for rows.Next() {
			var title, slug, summary, categoryName, categoryColor string
			var publishedAt time.Time
			if err := rows.Scan(&title, &slug, &summary, &publishedAt, &categoryName, &categoryColor); err != nil {
				continue
			}
			articles = append(articles, map[string]interface{}{
				"Title":         title,
				"Slug":          slug,
				"Summary":       summary,
				"PublishedAt":   publishedAt.Format("Jan 2, 2006"),
				"CategoryName":  categoryName,
				"CategoryColor": categoryColor,
			})
		}

		return c.HTML(http.StatusOK, "home.html", map[string]any{
			"Title":    "GoFrame Showcase",
			"Articles": articles,
		})
	}
}
