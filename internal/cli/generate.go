package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
)

func runGenerate(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(stderr)

	force := fs.Bool("force", false, "Overwrite existing files")
	outDir := fs.String("out", ".", "Project root output directory")
	migrationsDir := fs.String("migrations", "", "Migrations directory (defaults to <out>/migrations)")

	// Allow the <kind> <name> positionals to appear before and/or after any
	// flags. Go's flag package stops at the first non-flag token, which would
	// otherwise silently drop --out/--force/--migrations placed after the
	// positionals (e.g. `nucleus generate resource Widget --out ./proj`).
	leading := make([]string, 0, 2)
	flagStart := 0
	for flagStart < len(args) && !strings.HasPrefix(args[flagStart], "-") {
		leading = append(leading, args[flagStart])
		flagStart++
	}
	if err := fs.Parse(args[flagStart:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	rest := append(leading, fs.Args()...)
	if len(rest) < 2 {
		return fmt.Errorf("usage: nucleus generate <model|handler|service|repository|migration|resource> <name>")
	}

	kind := strings.ToLower(rest[0])
	name := strings.TrimSpace(rest[1])
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}

	if err := ensureDir(*outDir); err != nil {
		return err
	}
	modulePath, _, err := detectModulePath(*outDir)
	if err != nil {
		return err
	}
	if err := ensureContractsAggregator(*outDir, defaultOpenAPITitle("", modulePath, *outDir)); err != nil {
		return err
	}

	snake := toSnakeCase(name)
	pascal := toPascalCase(name)
	if snake == "" || pascal == "" {
		return fmt.Errorf("invalid name %q", name)
	}

	switch kind {
	case "model":
		path, err := generateModelScaffold(*outDir, snake, pascal, *force)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Model scaffold created: %s\n", path)
		return nil

	case "handler":
		path, err := generateHandlerScaffold(*outDir, snake, pascal, *force)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Handler scaffold created: %s\n", path)
		return nil

	case "service":
		path, err := generateServiceScaffold(*outDir, snake, pascal, *force)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Service scaffold created: %s\n", path)
		return nil

	case "repository":
		path, err := generateRepositoryScaffold(*outDir, snake, pascal, *force)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Repository scaffold created: %s\n", path)
		return nil

	case "migration":
		dir := *migrationsDir
		if dir == "" {
			dir = filepath.Join(*outDir, "migrations")
		}
		migrator := db.NewMigrator(nil, dir, newSilentLogger())
		if err := migrator.Create(snake); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Migration scaffold created in: %s\n", dir)
		return nil

	case "resource":
		dir := *migrationsDir
		if dir == "" {
			dir = filepath.Join(*outDir, "migrations")
		}
		result, err := generateResourceScaffold(*outDir, dir, snake, pascal, *force)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Resource scaffold created: %s\n", pascal)
		fmt.Fprintf(stdout, "  model: %s\n", result.ModelPath)
		fmt.Fprintf(stdout, "  handler: %s\n", result.HandlerPath)
		fmt.Fprintf(stdout, "  service: %s\n", result.ServicePath)
		fmt.Fprintf(stdout, "  repository: %s\n", result.RepositoryPath)
		fmt.Fprintf(stdout, "  contract: %s\n", result.ContractPath)
		fmt.Fprintf(stdout, "  test: %s\n", result.TestPath)
		fmt.Fprintf(stdout, "  migration up: %s\n", result.MigrationUpPath)
		fmt.Fprintf(stdout, "  migration down: %s\n", result.MigrationDownPath)
		return nil

	default:
		return fmt.Errorf("unknown generate target %q", kind)
	}
}

type resourceScaffoldResult struct {
	ModelPath         string
	HandlerPath       string
	ServicePath       string
	RepositoryPath    string
	ContractPath      string
	TestPath          string
	MigrationUpPath   string
	MigrationDownPath string
}

func generateModelScaffold(outDir, snake, pascal string, force bool) (string, error) {
	path := filepath.Join(outDir, "internal", "models", snake+".go")
	body := fmt.Sprintf(modelTemplate, pascal, pascal)
	if err := writeFileIfNotExists(path, body, force); err != nil {
		return "", err
	}
	return path, nil
}

func generateHandlerScaffold(outDir, snake, pascal string, force bool) (string, error) {
	modulePath, hasModule, err := detectModulePath(outDir)
	if err != nil {
		return "", err
	}

	var body string
	if hasModule {
		servicePath := filepath.Join(outDir, "internal", "services", snake+"_service.go")
		if _, err := os.Stat(servicePath); errors.Is(err, os.ErrNotExist) {
			if _, err := generateServiceScaffold(outDir, snake, pascal, false); err != nil {
				return "", err
			}
		} else if err != nil {
			return "", fmt.Errorf("stat service scaffold: %w", err)
		}
		body = fmt.Sprintf(handlerWithServiceTemplate, modulePath, pascal, snake)
	} else {
		body = fmt.Sprintf(
			handlerTemplate,
			pascal, // comment
			pascal, // type
			pascal, // constructor name
			pascal, // constructor return type
			pascal, // constructor literal
			pascal, // mount receiver
			snake,  // route path
			pascal, // list receiver
			pascal, // message
		)
	}

	path := filepath.Join(outDir, "internal", "controllers", snake+"_handler.go")
	if err := writeFileIfNotExists(path, body, force); err != nil {
		return "", err
	}
	return path, nil
}

func generateServiceScaffold(outDir, snake, pascal string, force bool) (string, error) {
	path := filepath.Join(outDir, "internal", "services", snake+"_service.go")
	body := fmt.Sprintf(serviceTemplate, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal, pascal)
	if err := writeFileIfNotExists(path, body, force); err != nil {
		return "", err
	}
	return path, nil
}

func generateRepositoryScaffold(outDir, snake, pascal string, force bool) (string, error) {
	path := filepath.Join(outDir, "internal", "repositories", snake+"_repository.go")
	body := fmt.Sprintf(repositoryTemplate, pascal, pascal, pascal, pascal, pascal, pascal)
	if err := writeFileIfNotExists(path, body, force); err != nil {
		return "", err
	}
	return path, nil
}

func generateResourceScaffold(outDir, migrationsDir, snake, pascal string, force bool) (*resourceScaffoldResult, error) {
	modelPath, err := generateModelScaffold(outDir, snake, pascal, force)
	if err != nil {
		return nil, err
	}

	resourcePath := pluralizeResource(snake)
	modulePath, hasModule, err := detectModulePath(outDir)
	if err != nil {
		return nil, err
	}

	var repositoryPath string
	var servicePath string
	var contractPath string
	var handlerBody string
	var testBody string

	contractPath, err = generateResourceContractScaffold(outDir, snake, pascal, resourcePath, force)
	if err != nil {
		return nil, err
	}

	if hasModule {
		repositoryPath, err = generateResourceRepositoryScaffold(outDir, snake, pascal, force)
		if err != nil {
			return nil, err
		}

		servicePath, err = generateResourceServiceScaffold(outDir, snake, pascal, modulePath, force)
		if err != nil {
			return nil, err
		}

		handlerBody = fmt.Sprintf(resourceHandlerWithServiceTemplate, modulePath, pascal, resourcePath)
		testBody = fmt.Sprintf(resourceHandlerWithServiceTestTemplate, modulePath, pascal, resourcePath)
	} else {
		repositoryPath, err = generateRepositoryScaffold(outDir, snake, pascal, force)
		if err != nil {
			return nil, err
		}

		servicePath, err = generateServiceScaffold(outDir, snake, pascal, force)
		if err != nil {
			return nil, err
		}

		handlerBody = fmt.Sprintf(resourceHandlerTemplate, pascal, resourcePath)
		testBody = fmt.Sprintf(resourceHandlerTestTemplate, pascal, resourcePath)
	}

	handlerPath := filepath.Join(outDir, "internal", "controllers", snake+"_handler.go")
	if err := writeFileIfNotExists(handlerPath, handlerBody, force); err != nil {
		return nil, err
	}

	testPath := filepath.Join(outDir, "internal", "controllers", snake+"_handler_test.go")
	if err := writeFileIfNotExists(testPath, testBody, force); err != nil {
		return nil, err
	}

	table := resourcePath
	if err := validateSQLIdentifier(table); err != nil {
		return nil, err
	}

	migrationName := "create_" + table + "_table"
	upSQL, downSQL, err := model.BuildSQLiteMigrationScaffold(resourceScaffoldMeta(table, pascal))
	if err != nil {
		return nil, err
	}
	upPath, downPath, err := createMigrationPair(migrationsDir, migrationName, upSQL, downSQL)
	if err != nil {
		return nil, err
	}

	return &resourceScaffoldResult{
		ModelPath:         modelPath,
		HandlerPath:       handlerPath,
		ServicePath:       servicePath,
		RepositoryPath:    repositoryPath,
		ContractPath:      contractPath,
		TestPath:          testPath,
		MigrationUpPath:   upPath,
		MigrationDownPath: downPath,
	}, nil
}

func generateResourceRepositoryScaffold(outDir, snake, pascal string, force bool) (string, error) {
	path := filepath.Join(outDir, "internal", "repositories", snake+"_repository.go")
	body := fmt.Sprintf(resourceRepositoryTemplate, pascal, pluralizeResource(snake))
	if err := writeFileIfNotExists(path, body, force); err != nil {
		return "", err
	}
	return path, nil
}

func generateResourceServiceScaffold(outDir, snake, pascal, modulePath string, force bool) (string, error) {
	path := filepath.Join(outDir, "internal", "services", snake+"_service.go")
	body := fmt.Sprintf(resourceServiceTemplate, modulePath, pascal)
	if err := writeFileIfNotExists(path, body, force); err != nil {
		return "", err
	}
	return path, nil
}

func generateResourceContractScaffold(outDir, snake, pascal, resourcePath string, force bool) (string, error) {
	path := filepath.Join(outDir, "internal", "contracts", snake+"_contract.go")
	body := fmt.Sprintf(resourceContractTemplate, pascal, pascal, pascal, resourcePath, toPascalCase(resourcePath), resourcePath, pascal, pascal, resourcePath, pascal)
	if err := writeFileIfNotExists(path, body, force); err != nil {
		return "", err
	}
	return path, nil
}

func createMigrationPair(dir, name, upBody, downBody string) (string, string, error) {
	if err := ensureDir(dir); err != nil {
		return "", "", err
	}
	base := fmt.Sprintf("%s_%s", time.Now().UTC().Format("20060102150405"), toSnakeCase(name))
	upPath := filepath.Join(dir, base+".up.sql")
	downPath := filepath.Join(dir, base+".down.sql")
	if err := writeFileIfNotExists(upPath, strings.TrimSpace(upBody)+"\n", false); err != nil {
		return "", "", err
	}
	if err := writeFileIfNotExists(downPath, strings.TrimSpace(downBody)+"\n", false); err != nil {
		return "", "", err
	}
	return upPath, downPath, nil
}

func resourceScaffoldMeta(table, modelName string) *model.ModelMeta {
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

func pluralizeResource(name string) string {
	if name == "" {
		return name
	}
	if strings.HasSuffix(name, "y") && len(name) > 1 {
		prev := name[len(name)-2]
		if !strings.ContainsRune("aeiou", rune(prev)) {
			return name[:len(name)-1] + "ies"
		}
	}
	// A trailing "s" usually means the caller already passed a plural
	// ("fleets", "devices") — return it unchanged instead of producing
	// "fleetses". The "ss"/"us"/"is" endings are genuine singulars
	// (address, status, axis) and fall through to the es-suffix rule.
	if strings.HasSuffix(name, "s") &&
		!strings.HasSuffix(name, "ss") &&
		!strings.HasSuffix(name, "us") &&
		!strings.HasSuffix(name, "is") {
		return name
	}
	for _, suffix := range []string{"s", "x", "z", "ch", "sh"} {
		if strings.HasSuffix(name, suffix) {
			return name + "es"
		}
	}
	return name + "s"
}

const modelTemplate = `package models

import "github.com/jcsvwinston/nucleus/pkg/model"

// %s is a scaffold generated by nucleus CLI.
type %s struct {
	model.BaseModel

	Name string ` + "`db:\"column:name;required;index\" validate:\"required\" admin:\"list,search\"`" + `
}
`

const handlerTemplate = `package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/jcsvwinston/nucleus/pkg/router"
)

// %sHandler is a scaffold generated by nucleus CLI.
type %sHandler struct{}

func New%sHandler() *%sHandler {
	return &%sHandler{}
}

func (h *%sHandler) Mount(r *router.Mux) {
	r.Get("/%s", h.List)
}

func (h *%sHandler) List(c *router.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"message": "%s handler scaffold ready",
	})
}
`

const handlerWithServiceTemplate = `package controllers

import (
	"net/http"

	"%[1]s/internal/services"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// %[2]sHandler is a scaffold generated by nucleus CLI.
type %[2]sHandler struct {
	service *services.%[2]sService
}

func New%[2]sHandler(service *services.%[2]sService) *%[2]sHandler {
	return &%[2]sHandler{service: service}
}

func (h *%[2]sHandler) Mount(r *router.Mux) {
	r.Get("/%[3]s", h.List)
}

func (h *%[2]sHandler) List(c *router.Context) error {
	result, err := h.service.Health(c.Request.Context(), services.%[2]sHealthInput{})
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]any{
		"message": "%[2]s handler scaffold ready",
		"data":    result,
	})
}
`

const serviceTemplate = `package services

import "context"

type %sResult struct {
	Status string ` + "`json:\"status\"`" + `
}

type %sHealthInput struct{}

// %sService is a scaffold generated by nucleus CLI.
type %sService struct{}

func New%sService() *%sService {
	return &%sService{}
}

func (s *%sService) Health(_ context.Context, _ %sHealthInput) (%sResult, error) {
	return %sResult{Status: "ok"}, nil
}
`

const repositoryTemplate = `package repositories

import "context"

// %sRepository is a scaffold generated by nucleus CLI.
type %sRepository struct{}

func New%sRepository() *%sRepository {
	return &%sRepository{}
}

func (r *%sRepository) Ping(_ context.Context) error {
	return nil
}
`

const resourceRepositoryTemplate = `package repositories

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var Err%[1]sNotFound = errors.New("%[2]s record not found")

type %[1]sRecord struct {
	ID        uint      ` + "`json:\"id\"`" + `
	Name      string    ` + "`json:\"name\"`" + `
	CreatedAt time.Time ` + "`json:\"created_at\"`" + `
	UpdatedAt time.Time ` + "`json:\"updated_at\"`" + `
}

type List%[1]sParams struct {
	Query string
}

type Create%[1]sParams struct {
	Name string
}

type Update%[1]sParams struct {
	Name string
}

type %[1]sRepository struct {
	mu     sync.RWMutex
	nextID uint
	items  map[uint]%[1]sRecord
}

func New%[1]sRepository() *%[1]sRepository {
	return &%[1]sRepository{
		nextID: 1,
		items:  make(map[uint]%[1]sRecord),
	}
}

func (r *%[1]sRepository) List(_ context.Context, params List%[1]sParams) ([]%[1]sRecord, error) {
	r.mu.RLock()
	records := make([]%[1]sRecord, 0, len(r.items))
	query := strings.ToLower(strings.TrimSpace(params.Query))
	for _, record := range r.items {
		if query != "" && !strings.Contains(strings.ToLower(record.Name), query) {
			continue
		}
		records = append(records, record)
	}
	r.mu.RUnlock()

	sort.Slice(records, func(i, j int) bool {
		return records[i].ID < records[j].ID
	})
	return records, nil
}

func (r *%[1]sRepository) Get(_ context.Context, id uint) (%[1]sRecord, error) {
	r.mu.RLock()
	record, ok := r.items[id]
	r.mu.RUnlock()
	if !ok {
		return %[1]sRecord{}, Err%[1]sNotFound
	}
	return record, nil
}

func (r *%[1]sRepository) Create(_ context.Context, params Create%[1]sParams) (%[1]sRecord, error) {
	now := time.Now().UTC()

	r.mu.Lock()
	id := r.nextID
	r.nextID++
	record := %[1]sRecord{
		ID:        id,
		Name:      params.Name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	r.items[id] = record
	r.mu.Unlock()

	return record, nil
}

func (r *%[1]sRepository) Update(_ context.Context, id uint, params Update%[1]sParams) (%[1]sRecord, error) {
	r.mu.Lock()
	record, ok := r.items[id]
	if !ok {
		r.mu.Unlock()
		return %[1]sRecord{}, Err%[1]sNotFound
	}

	record.Name = params.Name
	record.UpdatedAt = time.Now().UTC()
	r.items[id] = record
	r.mu.Unlock()

	return record, nil
}

func (r *%[1]sRepository) Delete(_ context.Context, id uint) error {
	r.mu.Lock()
	if _, ok := r.items[id]; !ok {
		r.mu.Unlock()
		return Err%[1]sNotFound
	}
	delete(r.items, id)
	r.mu.Unlock()
	return nil
}
`

const resourceServiceTemplate = `package services

import (
	"context"
	"strings"

	"%[1]s/internal/repositories"
)

type %[2]sRecord struct {
	ID   uint   ` + "`json:\"id\"`" + `
	Name string ` + "`json:\"name\"`" + `
}

type List%[2]sInput struct {
	Query string
}

type Create%[2]sInput struct {
	Name string ` + "`json:\"name\" validate:\"required\"`" + `
}

type Update%[2]sInput struct {
	Name string ` + "`json:\"name\" validate:\"required\"`" + `
}

type %[2]sRepository interface {
	List(ctx context.Context, params repositories.List%[2]sParams) ([]repositories.%[2]sRecord, error)
	Get(ctx context.Context, id uint) (repositories.%[2]sRecord, error)
	Create(ctx context.Context, params repositories.Create%[2]sParams) (repositories.%[2]sRecord, error)
	Update(ctx context.Context, id uint, params repositories.Update%[2]sParams) (repositories.%[2]sRecord, error)
	Delete(ctx context.Context, id uint) error
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

func (s *%[2]sService) Get(ctx context.Context, id uint) (%[2]sRecord, error) {
	record, err := s.repository.Get(ctx, id)
	if err != nil {
		return %[2]sRecord{}, err
	}
	return map%[2]sRecord(record), nil
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

func (s *%[2]sService) Update(ctx context.Context, id uint, input Update%[2]sInput) (%[2]sRecord, error) {
	record, err := s.repository.Update(ctx, id, repositories.Update%[2]sParams{
		Name: strings.TrimSpace(input.Name),
	})
	if err != nil {
		return %[2]sRecord{}, err
	}
	return map%[2]sRecord(record), nil
}

func (s *%[2]sService) Delete(ctx context.Context, id uint) error {
	return s.repository.Delete(ctx, id)
}

func map%[2]sRecord(record repositories.%[2]sRecord) %[2]sRecord {
	return %[2]sRecord{
		ID:   record.ID,
		Name: record.Name,
	}
}
`

const resourceContractTemplate = `package contracts

import "github.com/jcsvwinston/nucleus/pkg/openapi"

func init() {
	RegisterContract(Register%[1]sContract)
}

func Register%[1]sContract(doc *openapi.Document) {
	doc.AddSchema("%[2]sRecord", openapi.ObjectSchema(map[string]openapi.Schema{
		"id":   openapi.IDSchema(),
		"name": {Type: "string"},
	}, "id", "name"))

	doc.AddSchema("Create%[3]sInput", openapi.ObjectSchema(map[string]openapi.Schema{
		"name": {Type: "string"},
	}, "name"))

	doc.AddSchema("Update%[3]sInput", openapi.ObjectSchema(map[string]openapi.Schema{
		"name": {Type: "string"},
	}, "name"))

	doc.EnsurePaths()
	doc.Paths["/%[4]s"] = openapi.PathItem{
		Get: &openapi.Operation{
			OperationID: "list%[5]s",
			Summary:     "List %[5]s",
			Description: "Returns the scaffolded %[4]s collection.",
			Tags:        []string{"%[4]s"},
			Parameters: []openapi.Parameter{
				openapi.SearchQueryParameter("Filter %[4]s by name."),
			},
			Responses: map[string]openapi.Response{
				"200": openapi.JSONResponse("Resource collection", openapi.CollectionEnvelopeSchema(openapi.RefSchema("%[2]sRecord"))),
				"500": openapi.ErrorResponse("Unexpected error"),
			},
		},
		Post: &openapi.Operation{
			OperationID: "create%[7]s",
			Summary:     "Create %[8]s",
			Description: "Creates a scaffolded %[9]s resource.",
			Tags:        []string{"%[4]s"},
			RequestBody: openapi.JSONRequestBody(openapi.RefSchema("Create%[3]sInput"), true),
			Responses: map[string]openapi.Response{
				"201": openapi.JSONResponse("Created resource", openapi.DataEnvelopeSchema(openapi.RefSchema("%[2]sRecord"))),
				"400": openapi.ErrorResponse("Invalid request"),
			},
		},
	}

	doc.Paths["/%[4]s/{id}"] = openapi.PathItem{
		Get: &openapi.Operation{
			OperationID: "get%[10]s",
			Summary:     "Get %[8]s",
			Description: "Returns one scaffolded %[8]s resource by id.",
			Tags:        []string{"%[4]s"},
			Parameters: []openapi.Parameter{
				openapi.PathParameter("id", openapi.IDSchema(), "%[8]s identifier"),
			},
			Responses: map[string]openapi.Response{
				"200": openapi.JSONResponse("Single resource", openapi.DataEnvelopeSchema(openapi.RefSchema("%[2]sRecord"))),
				"400": openapi.ErrorResponse("Invalid request"),
				"404": openapi.ErrorResponse("Resource not found"),
			},
		},
		Put: &openapi.Operation{
			OperationID: "update%[10]s",
			Summary:     "Update %[8]s",
			Description: "Updates one scaffolded %[8]s resource by id.",
			Tags:        []string{"%[4]s"},
			Parameters: []openapi.Parameter{
				openapi.PathParameter("id", openapi.IDSchema(), "%[8]s identifier"),
			},
			RequestBody: openapi.JSONRequestBody(openapi.RefSchema("Update%[3]sInput"), true),
			Responses: map[string]openapi.Response{
				"200": openapi.JSONResponse("Updated resource", openapi.DataEnvelopeSchema(openapi.RefSchema("%[2]sRecord"))),
				"400": openapi.ErrorResponse("Invalid request"),
				"404": openapi.ErrorResponse("Resource not found"),
			},
		},
		Delete: &openapi.Operation{
			OperationID: "delete%[10]s",
			Summary:     "Delete %[8]s",
			Description: "Deletes one scaffolded %[8]s resource by id.",
			Tags:        []string{"%[4]s"},
			Parameters: []openapi.Parameter{
				openapi.PathParameter("id", openapi.IDSchema(), "%[8]s identifier"),
			},
			Responses: map[string]openapi.Response{
				"204": openapi.EmptyResponse("Resource deleted"),
				"400": openapi.ErrorResponse("Invalid request"),
				"404": openapi.ErrorResponse("Resource not found"),
			},
		},
	}
}
`

const resourceHandlerWithServiceTemplate = `package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"%[1]s/internal/services"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

type %[2]sPayload struct {
	Name string ` + "`json:\"name\"`" + `
}

type %[2]sHandler struct {
	service *services.%[2]sService
}

func New%[2]sHandler(service *services.%[2]sService) *%[2]sHandler {
	return &%[2]sHandler{service: service}
}

func (h *%[2]sHandler) Mount(r *router.Mux) {
	r.Resource("/%[3]s", router.ResourceHandlers{
		List:     h.List,
		Create:   h.Create,
		Retrieve: h.Get,
		Update:   h.Update,
		Delete:   h.Delete,
	})
}

func (h *%[2]sHandler) List(c *router.Context) error {
	records, err := h.service.List(c.Request.Context(), services.List%[2]sInput{
		Query: strings.TrimSpace(c.Query("q")),
	})
	if err != nil {
		return gferrors.InternalError("unable to list %[3]s")
	}

	return c.JSON(http.StatusOK, map[string]any{
		"data":  records,
		"count": len(records),
	})
}

func (h *%[2]sHandler) Get(c *router.Context) error {
	id, err := parseResourceID(c.Request)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}

	record, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		return gferrors.NotFound("%[2]s", strconv.FormatUint(uint64(id), 10))
	}

	return c.JSON(http.StatusOK, map[string]any{"data": record})
}

func (h *%[2]sHandler) Create(c *router.Context) error {
	payload, err := decode%[2]sPayload(c.Request)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}

	record, err := h.service.Create(c.Request.Context(), services.Create%[2]sInput{Name: payload.Name})
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}

	return c.JSON(http.StatusCreated, map[string]any{"data": record})
}

func (h *%[2]sHandler) Update(c *router.Context) error {
	id, err := parseResourceID(c.Request)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}

	payload, err := decode%[2]sPayload(c.Request)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}

	record, err := h.service.Update(c.Request.Context(), id, services.Update%[2]sInput{Name: payload.Name})
	if err != nil {
		return gferrors.NotFound("%[2]s", strconv.FormatUint(uint64(id), 10))
	}

	return c.JSON(http.StatusOK, map[string]any{"data": record})
}

func (h *%[2]sHandler) Delete(c *router.Context) error {
	id, err := parseResourceID(c.Request)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}

	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		return gferrors.NotFound("%[2]s", strconv.FormatUint(uint64(id), 10))
	}

	return c.NoContent()
}

func decode%[2]sPayload(r *http.Request) (%[2]sPayload, error) {
	defer r.Body.Close()

	var payload %[2]sPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return payload, errors.New("request body must be valid JSON")
	}

	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" {
		return payload, errors.New("name is required")
	}

	return payload, nil
}

func parseResourceID(r *http.Request) (uint, error) {
	raw := strings.TrimSpace(r.PathValue("id"))
	if raw == "" {
		return 0, errors.New("resource id is required")
	}

	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, errors.New("resource id must be a positive integer")
	}

	return uint(id), nil
}
`

const resourceHandlerWithServiceTestTemplate = `package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"%[1]s/internal/repositories"
	"%[1]s/internal/services"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

func Test%[2]sHandler_CRUDLifecycle(t *testing.T) {
	repository := repositories.New%[2]sRepository()
	service := services.New%[2]sService(repository)
	h := New%[2]sHandler(service)
	r := router.NewMux()
	h.Mount(r)

	createRec := perform%[2]sRequest(t, r, http.MethodPost, "/%[3]s/", map[string]any{"name": "Books"})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected status %%d, got %%d", http.StatusCreated, createRec.Code)
	}

	createBody := decode%[2]sJSON(t, createRec.Body.Bytes())
	createData, ok := createBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected create response data object, got %%T", createBody["data"])
	}

	resourceID, ok := createData["id"].(float64)
	if !ok || resourceID <= 0 {
		t.Fatalf("expected created record id, got %%v", createData["id"])
	}
	if got := createData["name"]; got != "Books" {
		t.Fatalf("expected created name %%q, got %%v", "Books", got)
	}

	secondCreateRec := perform%[2]sRequest(t, r, http.MethodPost, "/%[3]s/", map[string]any{"name": "Games"})
	if secondCreateRec.Code != http.StatusCreated {
		t.Fatalf("expected status %%d, got %%d", http.StatusCreated, secondCreateRec.Code)
	}

	listRec := perform%[2]sRequest(t, r, http.MethodGet, "/%[3]s/", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected status %%d, got %%d", http.StatusOK, listRec.Code)
	}
	listBody := decode%[2]sJSON(t, listRec.Body.Bytes())
	if got := int(listBody["count"].(float64)); got != 2 {
		t.Fatalf("expected list count 2, got %%d", got)
	}

	filteredRec := perform%[2]sRequest(t, r, http.MethodGet, "/%[3]s/?q=book", nil)
	if filteredRec.Code != http.StatusOK {
		t.Fatalf("expected status %%d, got %%d", http.StatusOK, filteredRec.Code)
	}
	filteredBody := decode%[2]sJSON(t, filteredRec.Body.Bytes())
	if got := int(filteredBody["count"].(float64)); got != 1 {
		t.Fatalf("expected filtered count 1, got %%d", got)
	}

	resourcePath := fmt.Sprintf("/%[3]s/%%d", int(resourceID))
	getRec := perform%[2]sRequest(t, r, http.MethodGet, resourcePath, nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected status %%d, got %%d", http.StatusOK, getRec.Code)
	}

	updateRec := perform%[2]sRequest(t, r, http.MethodPut, resourcePath, map[string]any{"name": "Novels"})
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected status %%d, got %%d", http.StatusOK, updateRec.Code)
	}
	updateBody := decode%[2]sJSON(t, updateRec.Body.Bytes())
	updateData, ok := updateBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected update response data object, got %%T", updateBody["data"])
	}
	if got := updateData["name"]; got != "Novels" {
		t.Fatalf("expected updated name %%q, got %%v", "Novels", got)
	}

	updatedFilteredRec := perform%[2]sRequest(t, r, http.MethodGet, "/%[3]s/?q=nov", nil)
	if updatedFilteredRec.Code != http.StatusOK {
		t.Fatalf("expected status %%d, got %%d", http.StatusOK, updatedFilteredRec.Code)
	}
	updatedFilteredBody := decode%[2]sJSON(t, updatedFilteredRec.Body.Bytes())
	if got := int(updatedFilteredBody["count"].(float64)); got != 1 {
		t.Fatalf("expected filtered count 1 after update, got %%d", got)
	}

	deleteRec := perform%[2]sRequest(t, r, http.MethodDelete, resourcePath, nil)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("expected status %%d, got %%d", http.StatusNoContent, deleteRec.Code)
	}

	finalListRec := perform%[2]sRequest(t, r, http.MethodGet, "/%[3]s/", nil)
	finalListBody := decode%[2]sJSON(t, finalListRec.Body.Bytes())
	if got := int(finalListBody["count"].(float64)); got != 1 {
		t.Fatalf("expected list count 1 after delete, got %%d", got)
	}

	badIDRec := perform%[2]sRequest(t, r, http.MethodGet, "/%[3]s/not-a-number", nil)
	assertStructuredErrorResponse(t, badIDRec, http.StatusBadRequest, "BAD_REQUEST")

	missingRec := perform%[2]sRequest(t, r, http.MethodGet, resourcePath, nil)
	assertStructuredErrorResponse(t, missingRec, http.StatusNotFound, "NOT_FOUND")
}

func Test%[2]sHandler_RejectsInvalidPayload(t *testing.T) {
	repository := repositories.New%[2]sRepository()
	service := services.New%[2]sService(repository)
	h := New%[2]sHandler(service)
	r := router.NewMux()
	h.Mount(r)

	rec := perform%[2]sRequest(t, r, http.MethodPost, "/%[3]s/", map[string]any{"name": "  "})
	assertStructuredErrorResponse(t, rec, http.StatusBadRequest, "BAD_REQUEST")
}

func perform%[2]sRequest(t *testing.T, handler http.Handler, method, path string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatalf("encode request body failed: %%v", err)
		}
	}

	req := httptest.NewRequest(method, path, &body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decode%[2]sJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode response failed: %%v raw=%%s", err, string(raw))
	}
	return payload
}

func assertStructuredErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("expected status %%d, got %%d body=%%s", status, rec.Code, rec.Body.String())
	}

	body := decode%[2]sJSON(t, rec.Body.Bytes())
	errorBody, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured error body, got %%#v", body)
	}
	if got := errorBody["code"]; got != code {
		t.Fatalf("expected error code %%q, got %%v", code, got)
	}
	if message, ok := errorBody["message"].(string); !ok || message == "" {
		t.Fatalf("expected non-empty error message, got %%#v", errorBody)
	}
}
`

const resourceHandlerTemplate = `package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// %[1]sRecord is the default API representation returned by the scaffold.
type %[1]sRecord struct {
	ID        uint      ` + "`json:\"id\"`" + `
	Name      string    ` + "`json:\"name\"`" + `
	CreatedAt time.Time ` + "`json:\"created_at\"`" + `
	UpdatedAt time.Time ` + "`json:\"updated_at\"`" + `
}

type %[1]sPayload struct {
	Name string ` + "`json:\"name\"`" + `
}

// %[1]sHandler is a CRUD scaffold generated by nucleus CLI.
// It keeps data in memory so generated routes are usable before a repository
// or service layer is wired in.
type %[1]sHandler struct {
	mu     sync.RWMutex
	nextID uint
	items  map[uint]%[1]sRecord
}

func New%[1]sHandler() *%[1]sHandler {
	return &%[1]sHandler{
		nextID: 1,
		items:  make(map[uint]%[1]sRecord),
	}
}

func (h *%[1]sHandler) Mount(r *router.Mux) {
	// These handlers use the net/http (w, r) signature, so adapt each to a
	// router.Handler with router.FromHTTP before registering.
	r.Resource("/%[2]s", router.ResourceHandlers{
		List:     router.FromHTTP(h.List),
		Create:   router.FromHTTP(h.Create),
		Retrieve: router.FromHTTP(h.Get),
		Update:   router.FromHTTP(h.Update),
		Delete:   router.FromHTTP(h.Delete),
	})
}

func (h *%[1]sHandler) List(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	records := make([]%[1]sRecord, 0, len(h.items))
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	for _, record := range h.items {
		if query != "" && !strings.Contains(strings.ToLower(record.Name), query) {
			continue
		}
		records = append(records, record)
	}
	h.mu.RUnlock()

	sort.Slice(records, func(i, j int) bool {
		return records[i].ID < records[j].ID
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  records,
		"count": len(records),
	})
}

func (h *%[1]sHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseResourceID(r)
	if err != nil {
		writeError(w, r, gferrors.BadRequest(err.Error()))
		return
	}

	record, ok := h.lookup(id)
	if !ok {
		writeError(w, r, gferrors.NotFound("%[1]s", strconv.FormatUint(uint64(id), 10)))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": record})
}

func (h *%[1]sHandler) Create(w http.ResponseWriter, r *http.Request) {
	payload, err := decode%[1]sPayload(r)
	if err != nil {
		writeError(w, r, gferrors.BadRequest(err.Error()))
		return
	}

	now := time.Now().UTC()

	h.mu.Lock()
	id := h.nextID
	h.nextID++
	record := %[1]sRecord{
		ID:        id,
		Name:      payload.Name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	h.items[id] = record
	h.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{"data": record})
}

func (h *%[1]sHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseResourceID(r)
	if err != nil {
		writeError(w, r, gferrors.BadRequest(err.Error()))
		return
	}

	payload, err := decode%[1]sPayload(r)
	if err != nil {
		writeError(w, r, gferrors.BadRequest(err.Error()))
		return
	}

	h.mu.Lock()
	record, ok := h.items[id]
	if !ok {
		h.mu.Unlock()
		writeError(w, r, gferrors.NotFound("%[1]s", strconv.FormatUint(uint64(id), 10)))
		return
	}

	record.Name = payload.Name
	record.UpdatedAt = time.Now().UTC()
	h.items[id] = record
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"data": record})
}

func (h *%[1]sHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseResourceID(r)
	if err != nil {
		writeError(w, r, gferrors.BadRequest(err.Error()))
		return
	}

	h.mu.Lock()
	if _, ok := h.items[id]; !ok {
		h.mu.Unlock()
		writeError(w, r, gferrors.NotFound("%[1]s", strconv.FormatUint(uint64(id), 10)))
		return
	}
	delete(h.items, id)
	h.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

func (h *%[1]sHandler) lookup(id uint) (%[1]sRecord, bool) {
	h.mu.RLock()
	record, ok := h.items[id]
	h.mu.RUnlock()
	return record, ok
}

func decode%[1]sPayload(r *http.Request) (%[1]sPayload, error) {
	defer r.Body.Close()

	var payload %[1]sPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return payload, errors.New("request body must be valid JSON")
	}

	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" {
		return payload, errors.New("name is required")
	}

	return payload, nil
}

func parseResourceID(r *http.Request) (uint, error) {
	raw := strings.TrimSpace(r.PathValue("id"))
	if raw == "" {
		return 0, errors.New("resource id is required")
	}

	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, errors.New("resource id must be a positive integer")
	}

	return uint(id), nil
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	router.Error(w, r, err)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
`

const resourceHandlerTestTemplate = `package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/router"
)

func Test%[1]sHandler_CRUDLifecycle(t *testing.T) {
	h := New%[1]sHandler()
	r := router.NewMux()
	h.Mount(r)

	createRec := perform%[1]sRequest(t, r, http.MethodPost, "/%[2]s/", map[string]any{"name": "Books"})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected status %%d, got %%d", http.StatusCreated, createRec.Code)
	}

	createBody := decode%[1]sJSON(t, createRec.Body.Bytes())
	createData, ok := createBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected create response data object, got %%T", createBody["data"])
	}

	resourceID, ok := createData["id"].(float64)
	if !ok || resourceID <= 0 {
		t.Fatalf("expected created record id, got %%v", createData["id"])
	}
	if got := createData["name"]; got != "Books" {
		t.Fatalf("expected created name %%q, got %%v", "Books", got)
	}

	secondCreateRec := perform%[1]sRequest(t, r, http.MethodPost, "/%[2]s/", map[string]any{"name": "Games"})
	if secondCreateRec.Code != http.StatusCreated {
		t.Fatalf("expected status %%d, got %%d", http.StatusCreated, secondCreateRec.Code)
	}

	listRec := perform%[1]sRequest(t, r, http.MethodGet, "/%[2]s/", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected status %%d, got %%d", http.StatusOK, listRec.Code)
	}
	listBody := decode%[1]sJSON(t, listRec.Body.Bytes())
	if got := int(listBody["count"].(float64)); got != 2 {
		t.Fatalf("expected list count 2, got %%d", got)
	}

	filteredRec := perform%[1]sRequest(t, r, http.MethodGet, "/%[2]s/?q=book", nil)
	if filteredRec.Code != http.StatusOK {
		t.Fatalf("expected status %%d, got %%d", http.StatusOK, filteredRec.Code)
	}
	filteredBody := decode%[1]sJSON(t, filteredRec.Body.Bytes())
	if got := int(filteredBody["count"].(float64)); got != 1 {
		t.Fatalf("expected filtered count 1, got %%d", got)
	}

	resourcePath := fmt.Sprintf("/%[2]s/%%d", int(resourceID))
	getRec := perform%[1]sRequest(t, r, http.MethodGet, resourcePath, nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected status %%d, got %%d", http.StatusOK, getRec.Code)
	}

	updateRec := perform%[1]sRequest(t, r, http.MethodPut, resourcePath, map[string]any{"name": "Novels"})
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected status %%d, got %%d", http.StatusOK, updateRec.Code)
	}
	updateBody := decode%[1]sJSON(t, updateRec.Body.Bytes())
	updateData, ok := updateBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected update response data object, got %%T", updateBody["data"])
	}
	if got := updateData["name"]; got != "Novels" {
		t.Fatalf("expected updated name %%q, got %%v", "Novels", got)
	}

	updatedFilteredRec := perform%[1]sRequest(t, r, http.MethodGet, "/%[2]s/?q=nov", nil)
	if updatedFilteredRec.Code != http.StatusOK {
		t.Fatalf("expected status %%d, got %%d", http.StatusOK, updatedFilteredRec.Code)
	}
	updatedFilteredBody := decode%[1]sJSON(t, updatedFilteredRec.Body.Bytes())
	if got := int(updatedFilteredBody["count"].(float64)); got != 1 {
		t.Fatalf("expected filtered count 1 after update, got %%d", got)
	}

	deleteRec := perform%[1]sRequest(t, r, http.MethodDelete, resourcePath, nil)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("expected status %%d, got %%d", http.StatusNoContent, deleteRec.Code)
	}

	finalListRec := perform%[1]sRequest(t, r, http.MethodGet, "/%[2]s/", nil)
	finalListBody := decode%[1]sJSON(t, finalListRec.Body.Bytes())
	if got := int(finalListBody["count"].(float64)); got != 1 {
		t.Fatalf("expected list count 1 after delete, got %%d", got)
	}

	badIDRec := perform%[1]sRequest(t, r, http.MethodGet, "/%[2]s/not-a-number", nil)
	assertStructuredErrorResponse(t, badIDRec, http.StatusBadRequest, "BAD_REQUEST")

	missingRec := perform%[1]sRequest(t, r, http.MethodGet, resourcePath, nil)
	assertStructuredErrorResponse(t, missingRec, http.StatusNotFound, "NOT_FOUND")
}

func Test%[1]sHandler_RejectsInvalidPayload(t *testing.T) {
	h := New%[1]sHandler()
	r := router.NewMux()
	h.Mount(r)

	rec := perform%[1]sRequest(t, r, http.MethodPost, "/%[2]s/", map[string]any{"name": "  "})
	assertStructuredErrorResponse(t, rec, http.StatusBadRequest, "BAD_REQUEST")
}

func perform%[1]sRequest(t *testing.T, handler http.Handler, method, path string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatalf("encode request body failed: %%v", err)
		}
	}

	req := httptest.NewRequest(method, path, &body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decode%[1]sJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode response failed: %%v raw=%%s", err, string(raw))
	}
	return payload
}

func assertStructuredErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("expected status %%d, got %%d body=%%s", status, rec.Code, rec.Body.String())
	}

	body := decode%[1]sJSON(t, rec.Body.Bytes())
	errorBody, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured error body, got %%#v", body)
	}
	if got := errorBody["code"]; got != code {
		t.Fatalf("expected error code %%q, got %%v", code, got)
	}
	if message, ok := errorBody["message"].(string); !ok || message == "" {
		t.Fatalf("expected non-empty error message, got %%#v", errorBody)
	}
}
`
