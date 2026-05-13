package main

import (
	"context"
	"embed"
	"html/template"
	"log"
	"net/http"

	"github.com/jcsvwinston/nucleus/examples/mvc_api/internal/config"
	"github.com/jcsvwinston/nucleus/examples/mvc_api/internal/controllers"
	"github.com/jcsvwinston/nucleus/examples/mvc_api/internal/models"
	"github.com/jcsvwinston/nucleus/examples/mvc_api/internal/services"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/openapi"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

//go:embed templates/*.html
var templateFS embed.FS

var templatesPath = "templates/*.html"

func main() {
	cfg := config.DefaultConfig()
	a, err := app.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	// The framework mounts a default-deny RBAC middleware per ADR-004.
	// This example exposes a small public API and a couple of marketing
	// pages — none of them need authentication. Grant the anonymous
	// subject access to those surfaces; the framework-owned routes
	// (/healthz, /metrics, /admin/*, /login, …) are already seeded by
	// SeedBootstrapAllowList. Production apps load a real policy file
	// via `admin_rbac_policy_file` instead of these blanket allows.
	for _, path := range []string{
		"/", "/articles", "/contact", "/app/*",
		"/api/*", "/openapi.json",
	} {
		if err := a.Authorizer.AddPolicy("anonymous", path, "*"); err != nil {
			log.Fatalf("seed anonymous allow for %s: %v", path, err)
		}
	}

	svc, err := services.New(a)
	if err != nil {
		log.Fatal(err)
	}

	if err := registerModels(a); err != nil {
		log.Fatal(err)
	}

	tpl, err := template.ParseFS(templateFS, templatesPath)
	if err != nil {
		log.Fatal(err)
	}

	// Web routes
	a.Router.Get("/", router.FromHTTP(controllers.HomePage(tpl, cfg)))
	a.Router.Get("/articles", router.FromHTTP(controllers.PublishedArticlesPage(tpl, svc)))
	a.Router.Get("/contact", router.FromHTTP(controllers.LeadCapturePage(tpl)))
	a.Router.Post("/contact", router.FromHTTP(controllers.LeadCaptureSubmit(a, tpl, svc)))
	a.Router.Get("/app/login", router.FromHTTP(controllers.AppLoginPage(tpl)))
	a.Router.Post("/app/login", router.FromHTTP(controllers.AppLoginPost(a)))
	a.Router.Get("/app/logout", router.FromHTTP(controllers.AppLogout(a)))
	a.Router.Get("/app/dashboard", router.FromHTTP(controllers.AppDashboard(a, tpl, svc)))

	// API routes
	a.Router.Get("/api/health", func(c *router.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"status": "ok", "service": "nucleus-mvc-api"})
	})
	a.Router.Get("/api/articles", router.FromHTTP(controllers.ListArticles(svc)))
	a.Router.Get("/api/articles/live-flag", router.FromHTTP(controllers.ListArticlesLiveFlag(a, svc)))
	a.Router.Post("/api/articles", router.FromHTTP(controllers.CreateArticle(svc)))
	a.Router.Get("/api/leads", router.FromHTTP(controllers.ListLeads(svc)))
	a.Router.Post("/api/leads", router.FromHTTP(controllers.CreateLead(svc)))
	a.Router.Get("/api/demo/runtime", router.FromHTTP(controllers.DemoRuntime(a, svc)))
	a.Router.Post("/api/demo/outbox", router.FromHTTP(controllers.EnqueueOutbox(a, svc)))
	a.Router.Post("/api/demo/outbox/drain", router.FromHTTP(controllers.DrainOutbox(a, svc)))
	a.Router.Post("/api/demo/tasks", router.FromHTTP(controllers.EnqueueTask(a, svc)))

	// OpenAPI
	doc := exampleOpenAPIDocument()
	if err := a.MountOpenAPI("/openapi.json", func() *openapi.Document { return doc }); err != nil {
		log.Fatal(err)
	}

	port := cfg.Port
	log.Println("Nucleus MVC + API Showcase running:")
	log.Printf("  web:     http://localhost:%d/\n", port)
	log.Printf("  api:     http://localhost:%d/api/articles\n", port)
	log.Printf("  leads:   http://localhost:%d/api/leads\n", port)
	log.Printf("  openapi: http://localhost:%d/openapi.json\n", port)
	log.Printf("  admin:   http://localhost:%d/admin\n", port)
	log.Printf("  login:   admin / %s\n", cfg.AdminBootstrapPassword)
	log.Printf("  app auth: %s / %s\n", config.DemoAppUsername, config.DemoAppPassword)

	if err := a.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func registerModels(a *app.App) error {
	if err := a.RegisterModel(&models.Article{}, model.ModelConfig{
		Icon:         "document",
		ListFields:   []string{"ID", "Title", "Published", "CreatedAt"},
		SearchFields: []string{"Title", "Content"},
		Filters:      []string{"Published"},
		OrderBy:      "created_at desc",
	}); err != nil {
		return err
	}
	if err := a.RegisterModel(&models.Lead{}, model.ModelConfig{
		Icon:         "users",
		ListFields:   []string{"ID", "Name", "Email", "Company", "CreatedAt"},
		SearchFields: []string{"Name", "Email", "Company"},
		Filters:      []string{"WantsDemo"},
		OrderBy:      "created_at desc",
	}); err != nil {
		return err
	}
	return nil
}

func exampleOpenAPIDocument() *openapi.Document {
	doc := openapi.NewDocument("Nucleus MVC + API Showcase", "0.1.0")
	doc.Info.Description = "End-to-end showcase for MVC pages, JSON API endpoints, OpenAPI export, admin, outbox, and optional task runtime."

	doc.AddSchema("ArticleRecord", openapi.ObjectSchema(map[string]openapi.Schema{
		"id":         openapi.IDSchema(),
		"title":      {Type: "string"},
		"content":    {Type: "string"},
		"published":  {Type: "boolean"},
		"created_at": {Type: "string", Format: "date-time"},
		"updated_at": {Type: "string", Format: "date-time"},
	}, "id", "title", "published", "created_at", "updated_at"))

	doc.AddSchema("ArticleCreateInput", openapi.ObjectSchema(map[string]openapi.Schema{
		"title":     {Type: "string"},
		"content":   {Type: "string"},
		"published": {Type: "boolean"},
	}, "title"))

	doc.AddSchema("LeadRecord", openapi.ObjectSchema(map[string]openapi.Schema{
		"id":         openapi.IDSchema(),
		"name":       {Type: "string"},
		"email":      {Type: "string"},
		"company":    {Type: "string"},
		"wants_demo": {Type: "boolean"},
		"created_at": {Type: "string", Format: "date-time"},
		"updated_at": {Type: "string", Format: "date-time"},
	}, "id", "name", "email", "wants_demo", "created_at", "updated_at"))

	doc.AddSchema("LeadCreateInput", openapi.ObjectSchema(map[string]openapi.Schema{
		"name":       {Type: "string"},
		"email":      {Type: "string"},
		"company":    {Type: "string"},
		"wants_demo": {Type: "boolean"},
	}, "name", "email"))

	doc.Paths["/api/health"] = openapi.PathItem{
		Get: &openapi.Operation{
			OperationID: "healthCheck",
			Summary:     "Health check",
			Tags:        []string{"System"},
			Responses: map[string]openapi.Response{
				"200": openapi.JSONResponse("Healthy service", openapi.ObjectSchema(map[string]openapi.Schema{
					"status":  {Type: "string"},
					"service": {Type: "string"},
				}, "status", "service")),
			},
		},
	}

	doc.Paths["/api/articles"] = openapi.PathItem{
		Get: &openapi.Operation{
			OperationID: "listArticles",
			Summary:     "List articles",
			Tags:        []string{"Articles"},
			Responses: map[string]openapi.Response{
				"200": openapi.JSONResponse("Article collection", openapi.CollectionEnvelopeSchema(openapi.RefSchema("ArticleRecord"))),
			},
		},
		Post: &openapi.Operation{
			OperationID: "createArticle",
			Summary:     "Create article",
			Tags:        []string{"Articles"},
			RequestBody: openapi.JSONRequestBody(openapi.RefSchema("ArticleCreateInput"), true),
			Responses: map[string]openapi.Response{
				"201": openapi.JSONResponse("Created article", openapi.DataEnvelopeSchema(openapi.RefSchema("ArticleRecord"))),
				"400": openapi.ErrorResponse("Validation error"),
			},
		},
	}

	doc.Paths["/api/articles/live-flag"] = openapi.PathItem{
		Get: &openapi.Operation{
			OperationID: "listArticlesLiveFlag",
			Summary:     "List articles using the admin feature flag",
			Tags:        []string{"Articles"},
			Responses: map[string]openapi.Response{
				"200": openapi.JSONResponse("Article preview collection", openapi.ObjectSchema(map[string]openapi.Schema{
					"feature_flag": {Type: "string"},
					"enabled":      {Type: "boolean"},
					"mode":         {Type: "string"},
					"data":         openapi.ArraySchema(openapi.RefSchema("ArticleRecord")),
					"count":        {Type: "integer"},
				}, "feature_flag", "enabled", "mode", "data", "count")),
			},
		},
	}

	doc.Paths["/api/leads"] = openapi.PathItem{
		Get: &openapi.Operation{
			OperationID: "listLeads",
			Summary:     "List inbound leads",
			Tags:        []string{"Leads"},
			Responses: map[string]openapi.Response{
				"200": openapi.JSONResponse("Lead collection", openapi.CollectionEnvelopeSchema(openapi.RefSchema("LeadRecord"))),
			},
		},
		Post: &openapi.Operation{
			OperationID: "createLead",
			Summary:     "Create inbound lead",
			Tags:        []string{"Leads"},
			RequestBody: openapi.JSONRequestBody(openapi.RefSchema("LeadCreateInput"), true),
			Responses: map[string]openapi.Response{
				"201": openapi.JSONResponse("Created lead", openapi.DataEnvelopeSchema(openapi.RefSchema("LeadRecord"))),
				"400": openapi.ErrorResponse("Validation error"),
			},
		},
	}

	doc.Paths["/api/demo/runtime"] = openapi.PathItem{
		Get: &openapi.Operation{
			OperationID: "showDemoRuntime",
			Summary:     "Inspect showcase runtime",
			Tags:        []string{"Demo"},
			Responses: map[string]openapi.Response{
				"200": openapi.JSONResponse("Demo runtime snapshot", openapi.ObjectSchema(map[string]openapi.Schema{
					"name":             {Type: "string"},
					"admin_prefix":     {Type: "string"},
					"openapi_path":     {Type: "string"},
					"redis_configured": {Type: "boolean"},
				}, "name", "admin_prefix", "openapi_path", "redis_configured")),
			},
		},
	}

	doc.Paths["/api/demo/outbox"] = openapi.PathItem{
		Post: &openapi.Operation{
			OperationID: "enqueueDemoOutboxMessage",
			Summary:     "Enqueue a demo outbox message",
			Tags:        []string{"Demo"},
			Responses: map[string]openapi.Response{
				"201": openapi.JSONResponse("Created outbox message", openapi.ObjectSchema(map[string]openapi.Schema{
					"data":     {Type: "object"},
					"snapshot": {Type: "object"},
				}, "data", "snapshot")),
			},
		},
	}

	doc.Paths["/api/demo/outbox/drain"] = openapi.PathItem{
		Post: &openapi.Operation{
			OperationID: "drainDemoOutbox",
			Summary:     "Deliver one outbox batch",
			Tags:        []string{"Demo"},
			Responses: map[string]openapi.Response{
				"200": openapi.JSONResponse("Outbox drained", openapi.ObjectSchema(map[string]openapi.Schema{
					"result":   {Type: "object"},
					"snapshot": {Type: "object"},
				}, "result", "snapshot")),
			},
		},
	}

	doc.Paths["/api/demo/tasks"] = openapi.PathItem{
		Post: &openapi.Operation{
			OperationID: "enqueueDemoTask",
			Summary:     "Enqueue one demo background task",
			Tags:        []string{"Demo"},
			Responses: map[string]openapi.Response{
				"201": openapi.JSONResponse("Task enqueued", openapi.ObjectSchema(map[string]openapi.Schema{
					"data":     {Type: "object"},
					"snapshot": {Type: "object"},
				}, "data", "snapshot")),
				"400": openapi.ErrorResponse("Tasks runtime is disabled"),
			},
		},
	}

	return doc
}
