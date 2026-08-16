package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/model"
)

type startAppGeneratedFile struct {
	path string
	body string
}

func runStartApp(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("startapp", flag.ContinueOnError)
	fs.SetOutput(stderr)

	force := fs.Bool("force", false, "Overwrite existing files")
	outDir := fs.String("out", ".", "Project root output directory")
	migrationsDir := fs.String("migrations", "", "Migrations directory (defaults to <out>/migrations)")
	skipMigration := fs.Bool("skip-migration", false, "Skip SQL migration scaffold generation")
	dialect := fs.String("dialect", "", "Migration SQL dialect (sqlite|postgresql|mysql|mssql|oracle); defaults to the configured database")
	configPath := fs.String("config", "", "Path to nucleus config file (defaults to <out>/nucleus.yml)")
	databaseAlias := fs.String("database", "", "Database alias whose dialect the migration targets (defaults to database_default)")

	appNameFirst := ""
	parseArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		appNameFirst = strings.TrimSpace(args[0])
		parseArgs = args[1:]
	}

	if err := fs.Parse(parseArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	rest := fs.Args()
	if appNameFirst != "" {
		rest = append([]string{appNameFirst}, rest...)
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: nucleus startapp <name>")
	}
	name := strings.TrimSpace(rest[0])
	if name == "" {
		return fmt.Errorf("app name cannot be empty")
	}

	snake := toSnakeCase(name)
	pascal := toPascalCase(name)
	if snake == "" || pascal == "" {
		return fmt.Errorf("invalid app name %q", name)
	}

	pluralSnake := pluralizeResource(snake)
	pluralPascal := toPascalCase(pluralSnake)
	if err := validateSQLIdentifier(pluralSnake); err != nil {
		return err
	}

	modulePath, hasModule, err := detectModulePath(*outDir)
	if err != nil {
		return err
	}
	if err := ensureContractsAggregator(*outDir, defaultOpenAPITitle("", modulePath, *outDir)); err != nil {
		return err
	}

	// Ensure common architectural directories exist so startapp can be used to
	// grow both freshly generated projects and older trees safely.
	extraDirs := []string{
		filepath.Join(*outDir, "internal", "contracts"),
		filepath.Join(*outDir, "internal", "services"),
		filepath.Join(*outDir, "internal", "repositories"),
		filepath.Join(*outDir, "internal", "web", "static", snake),
	}
	for _, dirPath := range extraDirs {
		if err := ensureDir(dirPath); err != nil {
			return err
		}
	}

	// The repository scaffold embeds dialect-specific SQL (DX-21), so the
	// system is resolved up front. With --skip-migration and no resolvable
	// config the scaffold falls back to SQLite instead of failing: the
	// migration (the artifact where a wrong dialect breaks `migrate up`) is
	// not being generated in that case.
	system, err := resolveScaffoldSystem(*dialect, *configPath, *databaseAlias, *outDir)
	if err != nil {
		if !*skipMigration {
			return err
		}
		system = "sqlite"
	}

	files := []startAppGeneratedFile{
		{
			path: filepath.Join(*outDir, "internal", "models", snake+".go"),
			body: fmt.Sprintf(startAppModelTemplate, pascal),
		},
		{
			path: filepath.Join(*outDir, "internal", "controllers", snake+"_page.go"),
			body: fmt.Sprintf(startAppPageTemplate, pascal, snake),
		},
		{
			path: filepath.Join(*outDir, "internal", "web", "templates", snake, "index.html"),
			body: fmt.Sprintf(startAppHTMLTemplate, pascal, snake, pluralSnake),
		},
	}

	if hasModule {
		// DX-21: the app scaffold emits the same mountable artifacts as
		// `generate resource` — nucleus.Context controller, database-backed
		// repository and a Module() that wires them plus the page route. What
		// startapp used to emit here (router-package handlers nobody could
		// mount, a map repository) was dead code the moment it was written.
		files = append(files,
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "controllers", snake+"_api.go"),
				body: fmt.Sprintf(resourceControllerTemplate, modulePath, pascal, pluralSnake),
			},
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "services", snake+"_service.go"),
				body: fmt.Sprintf(resourceServiceTemplate, modulePath, pascal),
			},
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "repositories", snake+"_repository.go"),
				body: resourceRepositorySource(pascal, pluralSnake, system),
			},
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "contracts", snake+"_contract.go"),
				body: fmt.Sprintf(resourceContractTemplate, pascal, pascal, pascal, pluralSnake, pluralPascal, pluralSnake, pascal, pascal, pluralSnake, pascal),
			},
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "modules", snake+"_module.go"),
				body: fmt.Sprintf(startAppModuleTemplate, modulePath, pascal, pluralSnake, snake),
			},
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "tasks", snake+"_tasks.go"),
				body: fmt.Sprintf(
					startAppTasksTemplate,
					pascal,
					snake,
					pascal,
					pascal,
					pascal,
					pascal,
					pascal,
					pascal,
					snake,
				),
			},
		)
	} else {
		files = append(files,
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "controllers", snake+"_api.go"),
				body: fmt.Sprintf(startAppAPITemplate, pascal, pluralPascal),
			},
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "services", snake+"_service.go"),
				body: fmt.Sprintf(startAppServiceTemplate, pascal),
			},
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "repositories", snake+"_repository.go"),
				body: fmt.Sprintf(startAppRepositoryTemplate, pascal),
			},
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "contracts", snake+"_contract.go"),
				body: fmt.Sprintf(startAppContractTemplate, pascal, pluralSnake, pluralPascal, snake),
			},
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "tasks", snake+"_tasks.go"),
				body: fmt.Sprintf(
					startAppTasksTemplate,
					pascal,
					snake,
					pascal,
					pascal,
					pascal,
					pascal,
					pascal,
					pascal,
					snake,
				),
			},
		)
	}

	for _, f := range files {
		if err := writeFileIfNotExists(f.path, f.body, *force); err != nil {
			return err
		}
	}

	var upPath string
	var downPath string
	if !*skipMigration {
		dir := *migrationsDir
		if dir == "" {
			dir = filepath.Join(*outDir, "migrations")
		}

		migrationName := "create_" + pluralSnake + "_table"
		// Same dialect contract as `generate resource` (QCD-CLI-4): the
		// scaffolded migration targets the database the project is
		// configured against (resolved above, shared with the repository
		// scaffold), not unconditional SQLite.
		upSQL, downSQL, err := model.BuildMigrationScaffoldForSystem(system, startAppScaffoldMeta(pluralSnake, pascal))
		if err != nil {
			return err
		}

		upPath, downPath, err = createMigrationPair(dir, migrationName, upSQL, downSQL)
		if err != nil {
			return err
		}
	}

	fmt.Fprintf(stdout, "App scaffold created: %s\n", pascal)
	for _, f := range files {
		fmt.Fprintf(stdout, "  %s\n", f.path)
	}
	if !*skipMigration {
		fmt.Fprintf(stdout, "  %s\n", upPath)
		fmt.Fprintf(stdout, "  %s\n", downPath)
	}
	if hasModule {
		fmt.Fprintf(stdout, "Mount it in main.go:  nucleus.New().Mount(modules.%sModule())\n", pascal)
		fmt.Fprintln(stdout, "Then apply the migration (nucleus migrate up). With the default-deny authorizer, add rbac_policy.csv rows (and a CSRF exemption for JSON APIs) for the new routes.")
	}
	return nil
}

const startAppModelTemplate = `package models

import "github.com/jcsvwinston/nucleus/pkg/model"

type %s struct {
	model.BaseModel
	Name string ` + "`db:\"column:name;required;index\" validate:\"required,min=2\"`" + `
}
`

func startAppScaffoldMeta(table, modelName string) *model.ModelMeta {
	return &model.ModelMeta{
		Name:  modelName,
		Table: table,
		Fields: []model.FieldMeta{
			{Name: "ID", Column: "id", GoType: "uint", IsPK: true},
			{Name: "CreatedAt", Column: "created_at", GoType: "time.Time"},
			{Name: "UpdatedAt", Column: "updated_at", GoType: "time.Time"},
			{Name: "DeletedAt", Column: "deleted_at", GoType: "*time.Time"},
			{Name: "Name", Column: "name", GoType: "string", IsRequired: true},
		},
		PrimaryKey: "ID",
		Indexes: []model.IndexMeta{
			{Name: fmt.Sprintf("idx_%s_name", table), Columns: []string{"name"}},
		},
	}
}

const startAppAPITemplate = `package controllers

import (
	"database/sql"
	"net/http"
	"strings"
	"sync"

	gfrender "github.com/jcsvwinston/nucleus/pkg/router"
)

type create%[1]sInput struct {
	Name string ` + "`json:\"name\" validate:\"required,min=2\"`" + `
}

type %[1]sRecord struct {
	Name string ` + "`json:\"name\"`" + `
}

var (
	startApp%[1]sMu    sync.RWMutex
	startApp%[1]sItems []%[1]sRecord
)

func List%[2]s(_ *sql.DB) gfrender.Handler {
	return func(c *gfrender.Context) error {
		query := strings.ToLower(strings.TrimSpace(c.Query("q")))

		startApp%[1]sMu.RLock()
		items := make([]%[1]sRecord, 0, len(startApp%[1]sItems))
		for _, item := range startApp%[1]sItems {
			if query != "" && !strings.Contains(strings.ToLower(item.Name), query) {
				continue
			}
			items = append(items, item)
		}
		startApp%[1]sMu.RUnlock()

		return c.JSON(http.StatusOK, map[string]any{
			"data":  items,
			"count": len(items),
		})
	}
}

func Create%[1]s(_ *sql.DB) gfrender.Handler {
	return func(c *gfrender.Context) error {
		var in create%[1]sInput
		if err := c.Bind(&in); err != nil {
			return err
		}

		record := %[1]sRecord{Name: strings.TrimSpace(in.Name)}

		startApp%[1]sMu.Lock()
		startApp%[1]sItems = append(startApp%[1]sItems, record)
		startApp%[1]sMu.Unlock()

		return c.JSON(http.StatusCreated, map[string]any{
			"data": record,
		})
	}
}
`


const startAppServiceTemplate = `package services

import (
	"context"
	"strings"
)

type %[1]sRecord struct {
	Name string ` + "`json:\"name\"`" + `
}

type List%[1]sInput struct {
	Query string
}

type Create%[1]sInput struct {
	Name string ` + "`json:\"name\" validate:\"required,min=2\"`" + `
}

type Record%[1]sCreatedInput struct {
	Name string
}

type %[1]sRepository interface {
	List(ctx context.Context, input List%[1]sInput) ([]%[1]sRecord, error)
	Create(ctx context.Context, input Create%[1]sInput) (%[1]sRecord, error)
}

type %[1]sService struct{}

func New%[1]sService() *%[1]sService {
	return &%[1]sService{}
}

func (s *%[1]sService) List(_ context.Context, input List%[1]sInput) ([]%[1]sRecord, error) {
	_ = strings.TrimSpace(input.Query)
	return []%[1]sRecord{}, nil
}

func (s *%[1]sService) Create(_ context.Context, input Create%[1]sInput) (%[1]sRecord, error) {
	return %[1]sRecord{Name: strings.TrimSpace(input.Name)}, nil
}

func (s *%[1]sService) RecordCreated(_ context.Context, input Record%[1]sCreatedInput) error {
	_ = input
	return nil
}
`


// The record type is named per entity (%[1]sRecord, not a shared
// NameOnlyRecord): every generated file must be self-contained so that
// scaffolding a second app/resource into the same package never redeclares
// a package-level symbol (multi-entity safety).
const startAppRepositoryTemplate = `package repositories

import (
	"context"
	"strings"
	"sync"
)

type %[1]sRecord struct {
	Name string ` + "`json:\"name\"`" + `
}

type List%[1]sParams struct {
	Query string
}

type Create%[1]sParams struct {
	Name string
}

type %[1]sRepository struct {
	mu    sync.RWMutex
	items []%[1]sRecord
}

func New%[1]sRepository() *%[1]sRepository {
	return &%[1]sRepository{}
}

func (r *%[1]sRepository) List(_ context.Context, params List%[1]sParams) ([]%[1]sRecord, error) {
	query := strings.ToLower(strings.TrimSpace(params.Query))

	r.mu.RLock()
	items := make([]%[1]sRecord, 0, len(r.items))
	for _, item := range r.items {
		if query != "" && !strings.Contains(strings.ToLower(item.Name), query) {
			continue
		}
		items = append(items, item)
	}
	r.mu.RUnlock()

	return items, nil
}

func (r *%[1]sRepository) Create(_ context.Context, params Create%[1]sParams) (%[1]sRecord, error) {
	record := %[1]sRecord{Name: strings.TrimSpace(params.Name)}

	r.mu.Lock()
	r.items = append(r.items, record)
	r.mu.Unlock()

	return record, nil
}
`

const startAppPageTemplate = `package controllers

import (
	"html/template"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// %[1]sPage renders the scaffolded server-side page. The template is parsed
// by the generated module's OnStart (internal/modules/%[2]s_module.go) and
// the route is registered there — no manual wiring needed.
func %[1]sPage(tpl *template.Template) nucleus.Handler {
	return func(c *nucleus.Context) error {
		c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		return tpl.ExecuteTemplate(c.Writer, "index.html", map[string]any{})
	}
}
`

// startAppModuleTemplate is the mountable module the app scaffold emits
// (DX-21): the REST resource plus the server-rendered page, wired to the
// framework-managed database. nucleus.New().Mount(modules.<Name>Module()) is
// the whole integration.
const startAppModuleTemplate = `package modules

import (
	"context"
	"fmt"
	"html/template"

	"%[1]s/internal/controllers"
	"%[1]s/internal/repositories"
	"%[1]s/internal/services"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// %[2]sModule returns the mountable module for the %[4]s app scaffold.
// Register it in main.go:
//
//	nucleus.New().Mount(modules.%[2]sModule())
//
// Lifecycle: OnStart captures the framework-managed *sql.DB (rt.DB()),
// builds the repository/service pair and parses the page template; Routes
// runs after OnStart and registers the page plus the REST resource. The
// framework owns the database pool — the module never opens or closes it.
func %[2]sModule() nucleus.ModuleSpec {
	var (
		service *services.%[2]sService
		page    *template.Template
	)

	return nucleus.Module[struct{}]{
		Name: "%[4]s",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			db := rt.DB()
			if db == nil {
				return fmt.Errorf("%[4]s: no managed database configured (set databases.default.url in nucleus.yml)")
			}
			service = services.New%[2]sService(repositories.New%[2]sRepository(db))

			tpl, err := template.ParseFiles("internal/web/templates/%[4]s/index.html")
			if err != nil {
				return fmt.Errorf("%[4]s: parse page template: %%w", err)
			}
			page = tpl
			return nil
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/%[4]s", controllers.%[2]sPage(page))
			r.Resource("/%[3]s", controllers.New%[2]sController(service), nucleus.Methods(
				nucleus.Index,
				nucleus.Show,
				nucleus.Create,
				nucleus.Update,
				nucleus.Destroy,
			))
		},
	}.Build()
}
`

const startAppTasksTemplate = `package tasks

import (
	"context"
	"log"

	gftasks "github.com/jcsvwinston/nucleus/pkg/tasks"
)

const Task%sCreated = "%s.created"

type %sCreatedPayload struct {
	Name string ` + "`json:\"name\"`" + `
}

func Register%sTasks(manager gftasks.Manager) error {
	return manager.HandleFunc(Task%sCreated, handle%sCreated)
}

func handle%sCreated(_ context.Context, task gftasks.Task) error {
	var payload %sCreatedPayload
	if err := gftasks.DecodeJSONPayload(task, &payload); err != nil {
		return err
	}

	log.Printf("task processed: %s created => %%s", payload.Name)
	return nil
}
`


const startAppContractTemplate = `package contracts

import "github.com/jcsvwinston/nucleus/pkg/openapi"

func init() {
	RegisterContract(Register%[1]sContract)
}

func Register%[1]sContract(doc *openapi.Document) {
	doc.AddSchema("%[1]sRecord", openapi.ObjectSchema(map[string]openapi.Schema{
		"name": {Type: "string"},
	}, "name"))

	doc.AddSchema("Create%[1]sInput", openapi.ObjectSchema(map[string]openapi.Schema{
		"name": {Type: "string"},
	}, "name"))

	doc.EnsurePaths()
	doc.Paths["/%[2]s"] = openapi.PathItem{
		Get: &openapi.Operation{
			OperationID: "list%[3]s",
			Summary:     "List %[3]s",
			Description: "Returns the scaffolded %[4]s collection.",
			Tags:        []string{"%[2]s"},
			Parameters: []openapi.Parameter{
				openapi.SearchQueryParameter("Filter %[2]s by name."),
			},
			Responses: map[string]openapi.Response{
				"200": openapi.JSONResponse("Resource collection", openapi.CollectionEnvelopeSchema(openapi.RefSchema("%[1]sRecord"))),
				"500": openapi.ErrorResponse("Unexpected error"),
			},
		},
		Post: &openapi.Operation{
			OperationID: "create%[1]s",
			Summary:     "Create %[1]s",
			Description: "Creates a scaffolded %[4]s resource.",
			Tags:        []string{"%[2]s"},
			RequestBody: openapi.JSONRequestBody(openapi.RefSchema("Create%[1]sInput"), true),
			Responses: map[string]openapi.Response{
				"201": openapi.JSONResponse("Created resource", openapi.DataEnvelopeSchema(openapi.RefSchema("%[1]sRecord"))),
				"400": openapi.ErrorResponse("Invalid request"),
				"500": openapi.ErrorResponse("Unexpected error"),
			},
		},
	}
}
`

const startAppHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
  <style>
    body {
      margin: 0;
      font-family: "IBM Plex Sans", "Segoe UI", sans-serif;
      color: #132032;
      background: radial-gradient(circle at 12%% 18%%, #dffaf5 0%%, transparent 28%%),
        radial-gradient(circle at 86%% 84%%, #ffe8cf 0%%, transparent 32%%), #f7f7f2;
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
  </style>
</head>
<body>
  <main class="wrap">
    <section class="card">
      <h1>%s app scaffold listo</h1>
      <p>Punto de entrada sugerido para plantilla MVC del modulo <strong>%s</strong>.</p>
    </section>
  </main>
</body>
</html>
`
