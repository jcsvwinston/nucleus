package main

import (
	"fmt"

	"example.com/showcase_clean/internal/controllers"
	"example.com/showcase_clean/internal/db"
	"example.com/showcase_clean/internal/models"
	"github.com/jcsvwinston/GoFrame/pkg/app"
	"github.com/jcsvwinston/GoFrame/pkg/model"
)

func main() {
	// QuickStart handles configuration, signal handling, and graceful shutdown.
	// It demonstrates how GoFrame can be as simple as Fiber/Gin while remaining Enterprise.
	app.QuickStart(func(a *app.App) error {
		// 1. Automatic Schema Management
		// Extracts metadata from structs and ensures tables exist.
		if err := a.AutoMigrate(
			&models.Author{},
			&models.Category{},
			&models.Tag{},
			&models.Article{},
			&models.Comment{},
		); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		// 2. Data Seeding (if empty)
		if err := db.Seed(a.DefaultDB()); err != nil {
			a.Logger.Error("seeding failed", "error", err)
		}

		// 3. Declarative Admin Registration
		// Icons and UI hints are part of the model configuration.
		a.RegisterModel(&models.Article{}, model.ModelConfig{Icon: "📄", ListFields: []string{"Title", "Slug", "Published"}})
		a.RegisterModel(&models.Category{}, model.ModelConfig{Icon: "📁"})
		a.RegisterModel(&models.Author{}, model.ModelConfig{Icon: "👤"})
		a.RegisterModel(&models.Tag{}, model.ModelConfig{Icon: "🏷️"})
		a.RegisterModel(&models.Comment{}, model.ModelConfig{Icon: "💬"})

		// 4. Centralized Route Definition
		setupRoutes(a)

		a.Logger.Info("Showcase Demo is ready!", "url", "http://localhost:8080")
		return nil
	})
}

func setupRoutes(a *app.App) {
	db := a.DefaultDB()
	tpl := a.Templates

	// Static Files
	a.Router.Static("/static", "./internal/web/static")

	// Public Web Pages
	a.Router.Get("/", controllers.HomePage(tpl, db))
	a.Router.Get("/blog", controllers.BlogPage(tpl, db))
	a.Router.Get("/articles/{id}", controllers.ArticlePage(tpl, db))
	
	// API Endpoints
	a.Router.Get("/api/health", controllers.Health)
	a.Router.Get("/api/stats", controllers.GetStatsAPI(db))
	a.Router.Get("/api/categories", controllers.ListCategoriesAPI(db))
	a.Router.Get("/api/authors", controllers.ListAuthorsAPI(db))

	a.Logger.Info("Routes registered", "count", 7)
}
