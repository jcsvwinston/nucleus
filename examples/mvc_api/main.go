package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jcsvwinston/GoFrame/pkg/app"
	gferrors "github.com/jcsvwinston/GoFrame/pkg/errors"
	"github.com/jcsvwinston/GoFrame/pkg/model"
	"github.com/jcsvwinston/GoFrame/pkg/openapi"
	"github.com/jcsvwinston/GoFrame/pkg/outbox"
	gfrender "github.com/jcsvwinston/GoFrame/pkg/router"
	"github.com/jcsvwinston/GoFrame/pkg/tasks"
	"github.com/jcsvwinston/GoFrame/pkg/validate"
)

//go:embed templates/*.html
var templateFS embed.FS

type Article struct {
	model.BaseModel
	Title     string `db:"column:title;required" validate:"required,min=3" admin:"list,search"`
	Content   string `db:"column:content" admin:"list"`
	Published bool   `db:"column:published" admin:"list,filter"`
}

type Lead struct {
	model.BaseModel
	Name      string `db:"column:name;required" validate:"required,min=2" admin:"list,search"`
	Email     string `db:"column:email;required" validate:"required,email" admin:"list,search"`
	Company   string `db:"column:company" admin:"list,search"`
	WantsDemo bool   `db:"column:wants_demo" admin:"list,filter"`
}

type createArticleInput struct {
	Title     string `json:"title" validate:"required,min=3"`
	Content   string `json:"content"`
	Published bool   `json:"published"`
}

type createLeadInput struct {
	Name      string `json:"name" validate:"required,min=2"`
	Email     string `json:"email" validate:"required,email"`
	Company   string `json:"company"`
	WantsDemo bool   `json:"wants_demo"`
}

type articleDTO struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Published bool      `json:"published"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type leadDTO struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Company   string    `json:"company"`
	WantsDemo bool      `json:"wants_demo"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type leadCapturePageData struct {
	Title       string
	Submitted   bool
	Form        createLeadInput
	FieldErrors map[string]string
}

type demoTaskPayload struct {
	Kind     string `json:"kind"`
	Target   string `json:"target"`
	Source   string `json:"source"`
	QueuedAt string `json:"queued_at"`
}

const (
	demoAppUsername = "demo"
	demoAppPassword = "demo123456"
)

type exampleServices struct {
	sqlDB            *sql.DB
	outboxStore      *outbox.Store
	outboxDispatcher *outbox.Dispatcher
	taskManager      *tasks.Manager
	scheduler        *tasks.Scheduler
}

func main() {
	a, err := newExampleApp(nil)
	if err != nil {
		log.Fatal(err)
	}

	port := a.Config.Port
	log.Println("GoFrame showcase running:")
	log.Printf("  web:      http://localhost:%d/\n", port)
	log.Printf("  api:      http://localhost:%d/api/articles\n", port)
	log.Printf("  leads:    http://localhost:%d/api/leads\n", port)
	log.Printf("  openapi:  http://localhost:%d/openapi.json\n", port)
	log.Printf("  admin:    http://localhost:%d/admin\n", port)
	log.Printf("  system:   http://localhost:%d/admin/system\n", port)
	log.Printf("  login:    admin / %s\n", a.Config.AdminBootstrapPassword)
	log.Printf("  app auth: %s / %s\n", demoAppUsername, demoAppPassword)

	if strings.TrimSpace(a.Config.RedisURL) == "" {
		log.Println("  jobs:     disabled (set GOFRAME_EXAMPLE_REDIS_URL to see queues/schedules in /admin)")
	} else {
		log.Println("  jobs:     enabled via Redis demo runtime")
	}

	if err := a.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func defaultExampleConfig() *app.Config {
	cfg := &app.Config{
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
		AdminPrefix:            "/admin",
		AdminTitle:             "GoFrame Showcase Admin",
		AdminBootstrapUsername: "admin",
		AdminBootstrapEmail:    "admin@example.com",
		AdminBootstrapPassword: "supersecret123",
		LogLevel:               "info",
		LogFormat:              "text",
	}
	applyExampleEnvOverrides(cfg)
	return cfg
}

func applyExampleEnvOverrides(cfg *app.Config) {
	if cfg == nil {
		return
	}
	cfg.Port = getenvInt("GOFRAME_EXAMPLE_PORT", cfg.Port)

	if dbURL := strings.TrimSpace(os.Getenv("GOFRAME_EXAMPLE_DB_URL")); dbURL != "" {
		if cfg.Databases == nil {
			cfg.Databases = map[string]app.DatabaseConfig{}
		}
		dbCfg := cfg.Databases["default"]
		dbCfg.URL = dbURL
		cfg.Databases["default"] = dbCfg
	}

	if redisURL := strings.TrimSpace(os.Getenv("GOFRAME_EXAMPLE_REDIS_URL")); redisURL != "" {
		cfg.RedisURL = redisURL
	}

	if sessionStore := strings.TrimSpace(os.Getenv("GOFRAME_EXAMPLE_SESSION_STORE")); sessionStore != "" {
		cfg.SessionStore = strings.ToLower(sessionStore)
	}
	if sessionRedisURL := strings.TrimSpace(os.Getenv("GOFRAME_EXAMPLE_SESSION_REDIS_URL")); sessionRedisURL != "" {
		cfg.SessionRedisURL = sessionRedisURL
	}

	cfg.AdminClusterEnabled = getenvBool("GOFRAME_EXAMPLE_ADMIN_CLUSTER_ENABLED", cfg.AdminClusterEnabled)
	if clusterRedis := strings.TrimSpace(os.Getenv("GOFRAME_EXAMPLE_ADMIN_CLUSTER_REDIS_URL")); clusterRedis != "" {
		cfg.AdminClusterRedisURL = clusterRedis
	}
	if clusterChannel := strings.TrimSpace(os.Getenv("GOFRAME_EXAMPLE_ADMIN_CLUSTER_CHANNEL")); clusterChannel != "" {
		cfg.AdminClusterChannel = clusterChannel
	}
	if clusterNodeID := strings.TrimSpace(os.Getenv("GOFRAME_EXAMPLE_ADMIN_CLUSTER_NODE_ID")); clusterNodeID != "" {
		cfg.AdminClusterNodeID = clusterNodeID
	}
	if clusterToken := strings.TrimSpace(os.Getenv("GOFRAME_EXAMPLE_ADMIN_CLUSTER_TOKEN")); clusterToken != "" {
		cfg.AdminClusterToken = clusterToken
	}
	if traceURLTemplate := strings.TrimSpace(os.Getenv("GOFRAME_EXAMPLE_ADMIN_TRACE_URL_TEMPLATE")); traceURLTemplate != "" {
		cfg.AdminTraceURLTemplate = traceURLTemplate
	}
	if otlpEndpoint := strings.TrimSpace(os.Getenv("GOFRAME_EXAMPLE_OTLP_ENDPOINT")); otlpEndpoint != "" {
		cfg.OTLPEndpoint = otlpEndpoint
	}
	if adminTitle := strings.TrimSpace(os.Getenv("GOFRAME_EXAMPLE_ADMIN_TITLE")); adminTitle != "" {
		cfg.AdminTitle = adminTitle
	}
	if bootstrapPassword := strings.TrimSpace(os.Getenv("GOFRAME_EXAMPLE_ADMIN_BOOTSTRAP_PASSWORD")); bootstrapPassword != "" {
		cfg.AdminBootstrapPassword = bootstrapPassword
	}
}

func getenvInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func getenvBool(name string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
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

	outboxStore, err := outbox.NewStore(sqlDB, outbox.Config{Flavor: outbox.FlavorSQLite})
	if err != nil {
		return nil, fmt.Errorf("new outbox store: %w", err)
	}
	if err := ensureOutboxSeed(outboxStore); err != nil {
		return nil, fmt.Errorf("ensure outbox seed: %w", err)
	}
	outboxDispatcher, err := outbox.NewDispatcher(outboxStore, func(ctx context.Context, msg outbox.Message) error {
		a.Logger.Info("showcase outbox delivered", "topic", msg.Topic, "message_id", msg.ID)
		return nil
	}, outbox.DispatcherConfig{
		LeaseOwner: "mvc-api-showcase",
	})
	if err != nil {
		return nil, fmt.Errorf("new outbox dispatcher: %w", err)
	}

	services := &exampleServices{
		sqlDB:            sqlDB,
		outboxStore:      outboxStore,
		outboxDispatcher: outboxDispatcher,
	}

	if err := registerExampleModels(a); err != nil {
		return nil, err
	}

	if err := bootstrapTaskRuntime(a, services); err != nil {
		return nil, err
	}

	tpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	a.Router.Get("/", homeHandler(tpl, cfg))
	a.Router.Get("/articles", publishedArticlesPageHandler(tpl, sqlDB))
	a.Router.Get("/contact", leadCapturePageHandler(tpl))
	a.Router.Post("/contact", leadCaptureSubmitHandler(a, tpl, services))
	a.Router.Get("/app/login", appLoginPageHandler(tpl))
	a.Router.Post("/app/login", appLoginPostHandler(a))
	a.Router.Post("/app/logout", appLogoutHandler(a))
	a.Router.Get("/app/dashboard", appDashboardHandler(a, tpl, sqlDB))
	a.Router.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		gfrender.JSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"service": "goframe-mvc-api-showcase",
		})
	})
	a.Router.Get("/api/articles", listArticlesHandler(sqlDB))
	a.Router.Post("/api/articles", createArticleHandler(a, services))
	a.Router.Get("/api/articles/live-flag", listArticlesLiveFlagHandler(a, sqlDB))
	a.Router.Get("/api/leads", listLeadsHandler(sqlDB))
	a.Router.Post("/api/leads", createLeadHandler(a, sqlDB))
	a.Router.Get("/api/demo/runtime", demoRuntimeHandler(a, services))
	a.Router.Post("/api/demo/outbox", enqueueOutboxHandler(a, services))
	a.Router.Post("/api/demo/outbox/drain", drainOutboxHandler(a, services))
	a.Router.Post("/api/demo/tasks", enqueueTaskHandler(a, services))

	if err := a.MountOpenAPI("/openapi.json", exampleOpenAPIDocument); err != nil {
		return nil, fmt.Errorf("mount openapi: %w", err)
	}

	a.Admin.SetFeatureFlag("articles_preview_mode", false)
	return a, nil
}

func registerExampleModels(a *app.App) error {
	if err := a.RegisterModel(&Article{}, model.ModelConfig{
		Icon:         "document",
		ListFields:   []string{"ID", "Title", "Published", "CreatedAt"},
		SearchFields: []string{"Title", "Content"},
		Filters:      []string{"Published"},
		OrderBy:      "created_at desc",
	}); err != nil {
		return fmt.Errorf("register article model: %w", err)
	}
	if err := a.RegisterModel(&Lead{}, model.ModelConfig{
		Icon:         "users",
		ListFields:   []string{"ID", "Name", "Email", "Company", "WantsDemo", "CreatedAt"},
		SearchFields: []string{"Name", "Email", "Company"},
		Filters:      []string{"WantsDemo"},
		OrderBy:      "created_at desc",
	}); err != nil {
		return fmt.Errorf("register lead model: %w", err)
	}
	return nil
}

func bootstrapTaskRuntime(a *app.App, services *exampleServices) error {
	if a == nil || services == nil || strings.TrimSpace(a.Config.RedisURL) == "" {
		return nil
	}

	manager, err := tasks.NewManager(tasks.Config{
		RedisURL:    a.Config.RedisURL,
		Concurrency: 4,
		Queues: map[string]int{
			"default":  3,
			"critical": 1,
		},
	}, a.Logger)
	if err != nil {
		return fmt.Errorf("new task manager: %w", err)
	}
	if err := manager.HandleFunc("demo.email.send", func(ctx context.Context, task *asynq.Task) error {
		var payload demoTaskPayload
		if err := tasks.DecodeJSONPayload(task, &payload); err != nil {
			return err
		}
		a.Logger.Info("showcase task processed", "kind", payload.Kind, "target", payload.Target, "source", payload.Source)
		return nil
	}); err != nil {
		return fmt.Errorf("register task handler: %w", err)
	}
	if err := manager.HandleFunc("demo.heartbeat", func(ctx context.Context, task *asynq.Task) error {
		var payload demoTaskPayload
		if err := tasks.DecodeJSONPayload(task, &payload); err != nil {
			return err
		}
		a.Logger.Info("showcase scheduler heartbeat", "source", payload.Source, "queued_at", payload.QueuedAt)
		return nil
	}); err != nil {
		return fmt.Errorf("register heartbeat handler: %w", err)
	}

	workerCtx, workerCancel := context.WithCancel(context.Background())
	go func() {
		if err := manager.Run(workerCtx); err != nil {
			a.Logger.Error("showcase worker stopped", "error", err)
		}
	}()
	a.OnShutdown(func(ctx context.Context) error {
		workerCancel()
		return manager.Close()
	})

	scheduler, err := tasks.NewScheduler(tasks.SchedulerConfig{
		RedisURL:          a.Config.RedisURL,
		HeartbeatInterval: 5 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("new scheduler: %w", err)
	}

	policy := tasks.DefaultEnqueuePolicy()
	policy.Queue = "default"
	policy.MaxRetry = 1
	if _, err := scheduler.Register(tasks.PeriodicTask{
		Spec:     "@every 45s",
		TaskType: "demo.heartbeat",
		Payload: demoTaskPayload{
			Kind:     "scheduler",
			Target:   "cluster",
			Source:   "examples/mvc_api",
			QueuedAt: time.Now().UTC().Format(time.RFC3339),
		},
		Policy: policy,
	}); err != nil {
		return fmt.Errorf("register scheduler entry: %w", err)
	}
	if err := scheduler.Start(); err != nil {
		return fmt.Errorf("start scheduler: %w", err)
	}
	a.OnShutdown(func(ctx context.Context) error {
		scheduler.Shutdown()
		return scheduler.Close()
	})

	scheduledPolicy := tasks.DefaultEnqueuePolicy()
	scheduledPolicy.Queue = "critical"
	scheduledPolicy.MaxRetry = 2
	scheduledPolicy.ProcessIn = 30 * time.Second
	if _, err := manager.EnqueueJSONWithPolicy("demo.email.send", demoTaskPayload{
		Kind:     "welcome-seed",
		Target:   "admin@example.com",
		Source:   "examples/mvc_api/startup",
		QueuedAt: time.Now().UTC().Format(time.RFC3339),
	}, scheduledPolicy); err != nil {
		return fmt.Errorf("seed demo task: %w", err)
	}

	services.taskManager = manager
	services.scheduler = scheduler
	return nil
}

func ensureSchema(sqlDB *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS articles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME,
			title TEXT NOT NULL,
			content TEXT,
			published BOOLEAN NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS leads (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME,
			name TEXT NOT NULL,
			email TEXT NOT NULL,
			company TEXT,
			wants_demo BOOLEAN NOT NULL DEFAULT 0
		)`,
	}
	for _, stmt := range stmts {
		if _, err := sqlDB.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func ensureSeed(sqlDB *sql.DB) error {
	var articleCount int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&articleCount); err != nil {
		return err
	}
	if articleCount == 0 {
		now := time.Now().UTC()
		rows := []struct {
			title     string
			content   string
			published bool
		}{
			{
				title:     "Welcome to GoFrame",
				content:   "This record is editable from /admin and visible via /api/articles.",
				published: true,
			},
			{
				title:     "Draft roadmap note",
				content:   "This draft becomes visible from /api/articles/live-flag when the feature flag is enabled in /admin/system.",
				published: false,
			},
		}
		for _, row := range rows {
			if _, err := sqlDB.Exec(
				`INSERT INTO articles (created_at, updated_at, title, content, published) VALUES (?, ?, ?, ?, ?)`,
				now, now, row.title, row.content, row.published,
			); err != nil {
				return err
			}
		}
	}

	var leadCount int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM leads`).Scan(&leadCount); err != nil {
		return err
	}
	if leadCount == 0 {
		now := time.Now().UTC()
		_, err := sqlDB.Exec(
			`INSERT INTO leads (created_at, updated_at, name, email, company, wants_demo) VALUES (?, ?, ?, ?, ?, ?)`,
			now, now, "Ada Lovelace", "ada@example.com", "Analytical Engines Ltd.", true,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func ensureOutboxSeed(store *outbox.Store) error {
	if store == nil {
		return fmt.Errorf("outbox store is nil")
	}
	snapshot := store.Snapshot(context.Background())
	if snapshot.Total > 0 {
		return nil
	}
	_, err := store.Enqueue(context.Background(), outbox.Entry{
		Topic: "demo.welcome.email",
		Payload: map[string]any{
			"template": "welcome",
			"email":    "admin@example.com",
			"source":   "examples/mvc_api/seed",
		},
	})
	return err
}

func homeHandler(tpl *template.Template, cfg *app.Config) http.HandlerFunc {
	adminPassword := ""
	if cfg != nil {
		adminPassword = cfg.AdminBootstrapPassword
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = tpl.ExecuteTemplate(w, "home.html", map[string]any{
			"Title":         "GoFrame MVC + API Showcase",
			"AdminPassword": adminPassword,
			"AppUsername":   demoAppUsername,
			"AppPassword":   demoAppPassword,
		})
	}
}

func publishedArticlesPageHandler(tpl *template.Template, sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		articles, err := listArticleRecords(r.Context(), sqlDB, true, 24)
		if err != nil {
			gfrender.Error(w, err)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = tpl.ExecuteTemplate(w, "articles.html", map[string]any{
			"Title":    "Published Articles",
			"Articles": articles,
		})
	}
}

func leadCapturePageHandler(tpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		renderLeadCapturePage(w, tpl, http.StatusOK, leadCapturePageData{
			Title:     "Request a Demo",
			Submitted: strings.TrimSpace(r.URL.Query().Get("submitted")) == "1",
			Form: createLeadInput{
				WantsDemo: true,
			},
			FieldErrors: map[string]string{},
		})
	}
}

func leadCaptureSubmitHandler(a *app.App, tpl *template.Template, services *exampleServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			renderLeadCapturePage(w, tpl, http.StatusBadRequest, leadCapturePageData{
				Title:       "Request a Demo",
				Form:        createLeadInput{WantsDemo: true},
				FieldErrors: map[string]string{"form": "could not read submitted form"},
			})
			return
		}

		in := createLeadInput{
			Name:      strings.TrimSpace(r.FormValue("name")),
			Email:     strings.TrimSpace(r.FormValue("email")),
			Company:   strings.TrimSpace(r.FormValue("company")),
			WantsDemo: formBool(r.FormValue("wants_demo")),
		}
		if err := validate.Validate(in); err != nil {
			renderLeadCapturePage(w, tpl, validationStatus(err), leadCapturePageData{
				Title:       "Request a Demo",
				Form:        in,
				FieldErrors: validationFields(err),
			})
			return
		}

		lead, err := createLeadRecord(r.Context(), services.sqlDB, in)
		if err != nil {
			gfrender.Error(w, err, a.Logger)
			return
		}
		if services.outboxStore != nil {
			_, _ = services.outboxStore.Enqueue(r.Context(), outbox.Entry{
				Topic: "leads.captured",
				Payload: map[string]any{
					"id":         lead.ID,
					"email":      lead.Email,
					"company":    lead.Company,
					"wants_demo": lead.WantsDemo,
				},
			})
		}

		http.Redirect(w, r, "/contact?submitted=1", http.StatusSeeOther)
	}
}

func renderLeadCapturePage(w http.ResponseWriter, tpl *template.Template, status int, data leadCapturePageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = tpl.ExecuteTemplate(w, "contact.html", data)
}

func appLoginPageHandler(tpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = tpl.ExecuteTemplate(w, "app_login.html", map[string]any{
			"Title":       "Showcase Login",
			"AppUsername": demoAppUsername,
			"AppPassword": demoAppPassword,
			"Error":       strings.TrimSpace(r.URL.Query().Get("error")),
		})
	}
}

func appLoginPostHandler(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/app/login?error=invalid_form", http.StatusSeeOther)
			return
		}
		username := strings.TrimSpace(r.FormValue("username"))
		password := strings.TrimSpace(r.FormValue("password"))
		if username != demoAppUsername || password != demoAppPassword {
			http.Redirect(w, r, "/app/login?error=invalid_credentials", http.StatusSeeOther)
			return
		}
		if a.Session != nil {
			_ = a.Session.RenewToken(r.Context())
			a.Session.Put(r.Context(), "app_user", username)
		}
		http.Redirect(w, r, "/app/dashboard", http.StatusSeeOther)
	}
}

func appLogoutHandler(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.Session != nil {
			_ = a.Session.Destroy(r.Context())
		}
		http.Redirect(w, r, "/app/login", http.StatusSeeOther)
	}
}

func appDashboardHandler(a *app.App, tpl *template.Template, sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := ""
		if a.Session != nil {
			user = strings.TrimSpace(a.Session.GetString(r.Context(), "app_user"))
		}
		if user == "" {
			http.Redirect(w, r, "/app/login", http.StatusSeeOther)
			return
		}

		articleCount := countRows(r.Context(), sqlDB, "articles")
		leadCount := countRows(r.Context(), sqlDB, "leads")
		recentArticles, err := listArticleRecords(r.Context(), sqlDB, false, 5)
		if err != nil {
			gfrender.Error(w, err, a.Logger)
			return
		}
		recentLeads, err := listLeadRecords(r.Context(), sqlDB, 5)
		if err != nil {
			gfrender.Error(w, err, a.Logger)
			return
		}
		outboxSnapshot := outbox.InspectRuntime(sqlDB, outbox.Config{Flavor: outbox.FlavorSQLite})

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = tpl.ExecuteTemplate(w, "app_dashboard.html", map[string]any{
			"Title":          "Showcase Dashboard",
			"AppUser":        user,
			"ArticleCount":   articleCount,
			"LeadCount":      leadCount,
			"OutboxPending":  outboxSnapshot.Pending,
			"RecentArticles": recentArticles,
			"RecentLeads":    recentLeads,
		})
	}
}

func countRows(ctx context.Context, sqlDB *sql.DB, table string) int {
	if sqlDB == nil {
		return 0
	}
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	var count int
	if err := sqlDB.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0
	}
	return count
}

func listArticlesHandler(sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := listArticleRecords(r.Context(), sqlDB, false, 100)
		if err != nil {
			gfrender.Error(w, err)
			return
		}

		gfrender.JSON(w, http.StatusOK, map[string]any{
			"data":  items,
			"count": len(items),
		})
	}
}

func listArticlesLiveFlagHandler(a *app.App, sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		previewMode, _ := a.Admin.FeatureFlag("articles_preview_mode")
		items, err := listArticleRecords(r.Context(), sqlDB, !previewMode, 100)
		if err != nil {
			gfrender.Error(w, err)
			return
		}

		mode := "published_only"
		if previewMode {
			mode = "preview_all"
		}

		gfrender.JSON(w, http.StatusOK, map[string]any{
			"feature_flag": "articles_preview_mode",
			"enabled":      previewMode,
			"mode":         mode,
			"data":         items,
			"count":        len(items),
		})
	}
}

func createArticleHandler(a *app.App, services *exampleServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in createArticleInput
		if err := gfrender.Bind(r, &in); err != nil {
			gfrender.Error(w, err, a.Logger)
			return
		}

		now := time.Now().UTC()
		res, err := services.sqlDB.ExecContext(
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

		if services.outboxStore != nil {
			_, _ = services.outboxStore.Enqueue(r.Context(), outbox.Entry{
				Topic: "articles.created",
				Payload: map[string]any{
					"id":        id,
					"title":     in.Title,
					"published": in.Published,
				},
			})
		}

		gfrender.Created(w, map[string]any{
			"data": map[string]any{
				"id":         id,
				"title":      in.Title,
				"content":    in.Content,
				"published":  in.Published,
				"created_at": now,
				"updated_at": now,
			},
		})
	}
}

func listLeadsHandler(sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := listLeadRecords(r.Context(), sqlDB, 100)
		if err != nil {
			gfrender.Error(w, err)
			return
		}

		gfrender.JSON(w, http.StatusOK, map[string]any{
			"data":  items,
			"count": len(items),
		})
	}
}

func createLeadHandler(a *app.App, sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in createLeadInput
		if err := gfrender.Bind(r, &in); err != nil {
			gfrender.Error(w, err, a.Logger)
			return
		}

		lead, err := createLeadRecord(r.Context(), sqlDB, in)
		if err != nil {
			gfrender.Error(w, err, a.Logger)
			return
		}

		gfrender.Created(w, map[string]any{
			"data": lead,
		})
	}
}

func listArticleRecords(ctx context.Context, sqlDB *sql.DB, publishedOnly bool, limit int) ([]articleDTO, error) {
	query := `SELECT id, title, content, published, created_at, updated_at FROM articles`
	args := make([]any, 0, 1)
	if publishedOnly {
		query += ` WHERE published = 1`
	}
	if limit <= 0 {
		limit = 100
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := sqlDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]articleDTO, 0, min(limit, 16))
	for rows.Next() {
		var it articleDTO
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

func listLeadRecords(ctx context.Context, sqlDB *sql.DB, limit int) ([]leadDTO, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := sqlDB.QueryContext(
		ctx,
		`SELECT id, name, email, company, wants_demo, created_at, updated_at FROM leads ORDER BY id DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]leadDTO, 0, min(limit, 16))
	for rows.Next() {
		var it leadDTO
		if err := rows.Scan(&it.ID, &it.Name, &it.Email, &it.Company, &it.WantsDemo, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func createLeadRecord(ctx context.Context, sqlDB *sql.DB, in createLeadInput) (leadDTO, error) {
	now := time.Now().UTC()
	res, err := sqlDB.ExecContext(
		ctx,
		`INSERT INTO leads (created_at, updated_at, name, email, company, wants_demo) VALUES (?, ?, ?, ?, ?, ?)`,
		now, now, in.Name, in.Email, in.Company, in.WantsDemo,
	)
	if err != nil {
		return leadDTO{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return leadDTO{}, err
	}

	return leadDTO{
		ID:        id,
		Name:      in.Name,
		Email:     in.Email,
		Company:   in.Company,
		WantsDemo: in.WantsDemo,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func validationStatus(err error) int {
	var domErr *gferrors.DomainError
	if errors.As(err, &domErr) && domErr != nil && domErr.StatusCode > 0 {
		return domErr.StatusCode
	}
	return http.StatusBadRequest
}

func validationFields(err error) map[string]string {
	var domErr *gferrors.DomainError
	if errors.As(err, &domErr) && domErr != nil {
		if fields, ok := domErr.Details.(map[string]string); ok && fields != nil {
			return fields
		}
	}
	return map[string]string{
		"form": "please review the highlighted fields",
	}
}

func formBool(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func demoRuntimeHandler(a *app.App, services *exampleServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		outboxSnapshot := outbox.RuntimeSnapshot{Enabled: false, Reason: "outbox is not configured", Table: outbox.DefaultTableName}
		if services.outboxStore != nil {
			outboxSnapshot = services.outboxStore.Snapshot(r.Context())
		}

		jobsSnapshot := tasks.RuntimeSnapshot{Enabled: false, Reason: "redis_url is not configured"}
		if strings.TrimSpace(a.Config.RedisURL) != "" {
			jobsSnapshot = tasks.InspectRuntime(a.Config.RedisURL)
		}

		previewMode, _ := a.Admin.FeatureFlag("articles_preview_mode")
		gfrender.JSON(w, http.StatusOK, map[string]any{
			"name":                  "goframe-mvc-api-showcase",
			"admin_prefix":          a.Config.AdminPrefix,
			"openapi_path":          "/openapi.json",
			"feature_flags":         map[string]bool{"articles_preview_mode": previewMode},
			"outbox":                outboxSnapshot,
			"jobs":                  jobsSnapshot,
			"admin_cluster_enabled": a.Config.AdminClusterEnabled,
			"redis_configured":      strings.TrimSpace(a.Config.RedisURL) != "",
		})
	}
}

func enqueueOutboxHandler(a *app.App, services *exampleServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if services.outboxStore == nil {
			gfrender.JSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]any{
					"code":    "OUTBOX_DISABLED",
					"message": "outbox store is not configured",
				},
			})
			return
		}
		msg, err := services.outboxStore.Enqueue(r.Context(), outbox.Entry{
			Topic: "demo.manual.trigger",
			Payload: map[string]any{
				"path":      r.URL.Path,
				"triggered": time.Now().UTC().Format(time.RFC3339),
			},
		})
		if err != nil {
			gfrender.Error(w, err, a.Logger)
			return
		}
		gfrender.Created(w, map[string]any{
			"data":     msg,
			"snapshot": services.outboxStore.Snapshot(r.Context()),
		})
	}
}

func drainOutboxHandler(a *app.App, services *exampleServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if services.outboxDispatcher == nil || services.outboxStore == nil {
			gfrender.JSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]any{
					"code":    "OUTBOX_DISABLED",
					"message": "outbox runtime is not configured",
				},
			})
			return
		}
		result, err := services.outboxDispatcher.RunOnce(r.Context())
		if err != nil {
			gfrender.Error(w, err, a.Logger)
			return
		}
		gfrender.JSON(w, http.StatusOK, map[string]any{
			"result":   result,
			"snapshot": services.outboxStore.Snapshot(r.Context()),
		})
	}
}

func enqueueTaskHandler(a *app.App, services *exampleServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if services.taskManager == nil {
			gfrender.JSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]any{
					"code":    "TASKS_DISABLED",
					"message": "set GOFRAME_EXAMPLE_REDIS_URL to enable task demo endpoints",
				},
			})
			return
		}

		policy := tasks.DefaultEnqueuePolicy()
		policy.Queue = "critical"
		policy.MaxRetry = 2
		info, err := services.taskManager.EnqueueJSONCtxWithPolicy(r.Context(), "demo.email.send", demoTaskPayload{
			Kind:     "manual",
			Target:   "team@example.com",
			Source:   "/api/demo/tasks",
			QueuedAt: time.Now().UTC().Format(time.RFC3339),
		}, policy)
		if err != nil {
			gfrender.Error(w, err, a.Logger)
			return
		}

		gfrender.Created(w, map[string]any{
			"data": map[string]any{
				"id":    info.ID,
				"queue": info.Queue,
				"type":  info.Type,
			},
			"snapshot": tasks.InspectRuntime(a.Config.RedisURL),
		})
	}
}

func exampleOpenAPIDocument() *openapi.Document {
	doc := openapi.NewDocument("GoFrame MVC + API Showcase", "0.1.0")
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
