package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/jcsvwinston/GoFrame/pkg/model"
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
		return fmt.Errorf("usage: goframe startapp <name>")
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
				body: fmt.Sprintf(startAppAPIWithServiceTemplate, modulePath, pluralPascal, pascal, pascal, pascal, pascal),
			},
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "services", snake+"_service.go"),
				body: fmt.Sprintf(startAppServiceWithRepositoryTemplate, modulePath, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal),
			},
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "repositories", snake+"_repository.go"),
				body: fmt.Sprintf(startAppRepositoryTemplate, pascal, pascal, pascal, pascal, pascal, pascal),
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
				body: fmt.Sprintf(startAppAPITemplate, pascal, pluralPascal, pascal, pascal),
			},
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "services", snake+"_service.go"),
				body: fmt.Sprintf(startAppServiceTemplate, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal),
			},
			startAppGeneratedFile{
				path: filepath.Join(*outDir, "internal", "repositories", snake+"_repository.go"),
				body: fmt.Sprintf(startAppRepositoryTemplate, pascal, pascal, pascal, pascal, pascal, pascal),
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

import "github.com/jcsvwinston/GoFrame/pkg/model"

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
	gfrender "github.com/jcsvwinston/GoFrame/pkg/router"
)

type create%sInput struct {
	Name string ` + "`json:\"name\" validate:\"required,min=2\"`" + `
}

func List%s(_ *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		gfrender.JSON(w, http.StatusOK, map[string]any{
			"data":  []any{},
			"count": 0,
		})
	}
}

func Create%s(_ *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in create%sInput
		if err := gfrender.Bind(r, &in); err != nil {
			gfrender.Error(w, err)
			return
		}
		gfrender.Created(w, map[string]any{
			"data": map[string]any{
				"name": in.Name,
			},
		})
	}
}
`

const startAppAPIWithServiceTemplate = `package controllers

import (
	"net/http"

	"%s/internal/services"
	gfrender "github.com/jcsvwinston/GoFrame/pkg/router"
)

func List%s(service *services.%sService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := service.List(r.Context())
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

func Create%s(service *services.%sService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input services.Create%sInput
		if err := gfrender.Bind(r, &input); err != nil {
			gfrender.Error(w, err)
			return
		}

		item, err := service.Create(r.Context(), input)
		if err != nil {
			gfrender.Error(w, err)
			return
		}

		gfrender.Created(w, map[string]any{
			"data": item,
		})
	}
}
`

const startAppServiceTemplate = `package services

import "context"

type NameOnlyRecord struct {
	Name string ` + "`json:\"name\"`" + `
}

type Create%sInput struct {
	Name string ` + "`json:\"name\" validate:\"required,min=2\"`" + `
}

type Record%sCreatedInput struct {
	Name string
}

type %sRepository interface {
	List(ctx context.Context) ([]NameOnlyRecord, error)
	Create(ctx context.Context, name string) (NameOnlyRecord, error)
}

type %sService struct{}

func New%sService() *%sService {
	return &%sService{}
}

func (s *%sService) List(_ context.Context) ([]NameOnlyRecord, error) {
	return []NameOnlyRecord{}, nil
}

func (s *%sService) Create(_ context.Context, input Create%sInput) (NameOnlyRecord, error) {
	return NameOnlyRecord{Name: input.Name}, nil
}

func (s *%sService) RecordCreated(_ context.Context, input Record%sCreatedInput) error {
	_ = input
	return nil
}
`

const startAppServiceWithRepositoryTemplate = `package services

import (
	"context"

	"%s/internal/repositories"
)

type %sRecord struct {
	Name string ` + "`json:\"name\"`" + `
}

type Create%sInput struct {
	Name string ` + "`json:\"name\" validate:\"required,min=2\"`" + `
}

type Record%sCreatedInput struct {
	Name string
}

type %sRepository interface {
	List(ctx context.Context) ([]repositories.NameOnlyRecord, error)
	Create(ctx context.Context, name string) (repositories.NameOnlyRecord, error)
}

type %sService struct {
	repository %sRepository
}

func New%sService(repository %sRepository) *%sService {
	return &%sService{repository: repository}
}

func (s *%sService) List(ctx context.Context) ([]%sRecord, error) {
	records, err := s.repository.List(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]%sRecord, 0, len(records))
	for _, record := range records {
		items = append(items, %sRecord{Name: record.Name})
	}
	return items, nil
}

func (s *%sService) Create(ctx context.Context, input Create%sInput) (%sRecord, error) {
	record, err := s.repository.Create(ctx, input.Name)
	if err != nil {
		return %sRecord{}, err
	}

	return %sRecord{Name: record.Name}, nil
}

func (s *%sService) RecordCreated(_ context.Context, input Record%sCreatedInput) error {
	_ = input
	return nil
}
`

const startAppRepositoryTemplate = `package repositories

import "context"

type NameOnlyRecord struct {
	Name string ` + "`json:\"name\"`" + `
}

type %sRepository struct{}

func New%sRepository() *%sRepository {
	return &%sRepository{}
}

func (r *%sRepository) List(_ context.Context) ([]NameOnlyRecord, error) {
	return []NameOnlyRecord{}, nil
}

func (r *%sRepository) Create(_ context.Context, name string) (NameOnlyRecord, error) {
	return NameOnlyRecord{Name: name}, nil
}
`

const startAppPageTemplate = `package controllers

import (
	"html/template"
	"net/http"
)

func %sPage(tpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = tpl.ExecuteTemplate(w, "%s/index.html", map[string]any{})
	}
}
`

const startAppTasksTemplate = `package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/hibiken/asynq"
	gftasks "github.com/jcsvwinston/GoFrame/pkg/tasks"
)

const Task%sCreated = "%s.created"

type %sCreatedPayload struct {
	Name string ` + "`json:\"name\"`" + `
}

func Register%sTasks(manager *gftasks.Manager) error {
	return manager.HandleFunc(Task%sCreated, handle%sCreated)
}

func handle%sCreated(_ context.Context, task *asynq.Task) error {
	var payload %sCreatedPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %%w", err)
	}

	log.Printf("task processed: %s created => %%s", payload.Name)
	return nil
}
`

const startAppTasksWithServiceTemplate = `package tasks

import (
	"context"
	"encoding/json"
	"fmt"

	"%s/internal/services"
	"github.com/hibiken/asynq"
	gftasks "github.com/jcsvwinston/GoFrame/pkg/tasks"
)

const Task%sCreated = "%s.created"

type %sCreatedPayload struct {
	Name string ` + "`json:\"name\"`" + `
}

func Register%sTasks(manager *gftasks.Manager, service *services.%sService) error {
	return manager.HandleFunc(Task%sCreated, func(ctx context.Context, task *asynq.Task) error {
		return handle%sCreated(ctx, task, service)
	})
}

func handle%sCreated(ctx context.Context, task *asynq.Task, service *services.%sService) error {
	var payload %sCreatedPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %%w", err)
	}

	return service.RecordCreated(ctx, services.Record%sCreatedInput{
		Name: payload.Name,
	})
}
`

const startAppContractTemplate = `package contracts

import "github.com/jcsvwinston/GoFrame/pkg/openapi"

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
