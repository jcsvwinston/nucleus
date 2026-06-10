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
		files = append(files,
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "controllers", snake+"_api.go"),
				body: fmt.Sprintf(startAppAPIWithServiceTemplate, modulePath, pluralPascal, pascal),
			},
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "services", snake+"_service.go"),
				body: fmt.Sprintf(startAppServiceWithRepositoryTemplate, modulePath, pascal),
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
					startAppTasksWithServiceTemplate,
					modulePath,
					pascal,
					snake,
					pascal,
					pascal,
					pascal,
					pascal,
					pascal,
					pascal,
					pascal,
					pascal,
					pascal,
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
		upSQL, downSQL, err := model.BuildSQLiteMigrationScaffold(startAppScaffoldMeta(pluralSnake, pascal))
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
	return nil
}

const startAppModelTemplate = `package models

import "github.com/jcsvwinston/nucleus/pkg/model"

type %s struct {
	model.BaseModel
	Name string ` + "`db:\"column:name;required;index\" validate:\"required,min=2\" admin:\"list,search\"`" + `
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

const startAppAPIWithServiceTemplate = `package controllers

import (
	"net/http"
	"strings"

	"%[1]s/internal/services"
	gfrender "github.com/jcsvwinston/nucleus/pkg/router"
)

func List%[2]s(service *services.%[3]sService) gfrender.Handler {
	return func(c *gfrender.Context) error {
		items, err := service.List(c.Request.Context(), services.List%[3]sInput{
			Query: strings.TrimSpace(c.Query("q")),
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

func Create%[3]s(service *services.%[3]sService) gfrender.Handler {
	return func(c *gfrender.Context) error {
		var input services.Create%[3]sInput
		if err := c.Bind(&input); err != nil {
			return err
		}

		item, err := service.Create(c.Request.Context(), input)
		if err != nil {
			return err
		}

		return c.JSON(http.StatusCreated, map[string]any{
			"data": item,
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

const startAppServiceWithRepositoryTemplate = `package services

import (
	"context"
	"strings"

	"%[1]s/internal/repositories"
)

type %[2]sRecord struct {
	Name string ` + "`json:\"name\"`" + `
}

type List%[2]sInput struct {
	Query string
}

type Create%[2]sInput struct {
	Name string ` + "`json:\"name\" validate:\"required,min=2\"`" + `
}

type Record%[2]sCreatedInput struct {
	Name string
}

type %[2]sRepository interface {
	List(ctx context.Context, params repositories.List%[2]sParams) ([]repositories.%[2]sRecord, error)
	Create(ctx context.Context, params repositories.Create%[2]sParams) (repositories.%[2]sRecord, error)
}

type %[2]sService struct {
	repository %[2]sRepository
}

func New%[2]sService(repository %[2]sRepository) *%[2]sService {
	return &%[2]sService{repository: repository}
}

func (s *%[2]sService) List(ctx context.Context, input List%[2]sInput) ([]%[2]sRecord, error) {
	records, err := s.repository.List(ctx, repositories.List%[2]sParams{
		Query: strings.TrimSpace(input.Query),
	})
	if err != nil {
		return nil, err
	}

	items := make([]%[2]sRecord, 0, len(records))
	for _, record := range records {
		items = append(items, map%[2]sRecord(record))
	}
	return items, nil
}

func (s *%[2]sService) Create(ctx context.Context, input Create%[2]sInput) (%[2]sRecord, error) {
	record, err := s.repository.Create(ctx, repositories.Create%[2]sParams{
		Name: strings.TrimSpace(input.Name),
	})
	if err != nil {
		return %[2]sRecord{}, err
	}

	return map%[2]sRecord(record), nil
}

func (s *%[2]sService) RecordCreated(_ context.Context, input Record%[2]sCreatedInput) error {
	_ = input
	return nil
}

func map%[2]sRecord(record repositories.%[2]sRecord) %[2]sRecord {
	return %[2]sRecord{Name: record.Name}
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

	gfrender "github.com/jcsvwinston/nucleus/pkg/router"
)

func %sPage(tpl *template.Template) gfrender.Handler {
	return func(c *gfrender.Context) error {
		c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		return tpl.ExecuteTemplate(c.Writer, "%s/index.html", map[string]any{})
	}
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

const startAppTasksWithServiceTemplate = `package tasks

import (
	"context"

	"%s/internal/services"
	gftasks "github.com/jcsvwinston/nucleus/pkg/tasks"
)

const Task%sCreated = "%s.created"

type %sCreatedPayload struct {
	Name string ` + "`json:\"name\"`" + `
}

func Register%sTasks(manager gftasks.Manager, service *services.%sService) error {
	return manager.HandleFunc(Task%sCreated, func(ctx context.Context, task gftasks.Task) error {
		return handle%sCreated(ctx, task, service)
	})
}

func handle%sCreated(ctx context.Context, task gftasks.Task, service *services.%sService) error {
	var payload %sCreatedPayload
	if err := gftasks.DecodeJSONPayload(task, &payload); err != nil {
		return err
	}

	return service.RecordCreated(ctx, services.Record%sCreatedInput{
		Name: payload.Name,
	})
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
