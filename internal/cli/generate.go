package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/db"
	"github.com/jcsvwinston/GoFrame/pkg/model"
)

func runGenerate(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(stderr)

	force := fs.Bool("force", false, "Overwrite existing files")
	outDir := fs.String("out", ".", "Project root output directory")
	migrationsDir := fs.String("migrations", "", "Migrations directory (defaults to <out>/migrations)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	rest := fs.Args()
	if len(rest) < 2 {
		return fmt.Errorf("usage: goframe generate <model|handler|service|repository|migration|resource> <name>")
	}

	kind := strings.ToLower(rest[0])
	name := strings.TrimSpace(rest[1])
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}

	if err := ensureDir(*outDir); err != nil {
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
	path := filepath.Join(outDir, "internal", "controllers", snake+"_handler.go")
	body := fmt.Sprintf(
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
	if err := writeFileIfNotExists(path, body, force); err != nil {
		return "", err
	}
	return path, nil
}

func generateServiceScaffold(outDir, snake, pascal string, force bool) (string, error) {
	path := filepath.Join(outDir, "internal", "services", snake+"_service.go")
	body := fmt.Sprintf(serviceTemplate, pascal, pascal, pascal, pascal, pascal, pascal)
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

	repositoryPath, err := generateRepositoryScaffold(outDir, snake, pascal, force)
	if err != nil {
		return nil, err
	}

	servicePath, err := generateServiceScaffold(outDir, snake, pascal, force)
	if err != nil {
		return nil, err
	}

	resourcePath := pluralizeResource(snake)
	handlerPath := filepath.Join(outDir, "internal", "controllers", snake+"_handler.go")
	handlerBody := fmt.Sprintf(resourceHandlerTemplate, pascal, resourcePath)
	if err := writeFileIfNotExists(handlerPath, handlerBody, force); err != nil {
		return nil, err
	}

	testPath := filepath.Join(outDir, "internal", "controllers", snake+"_handler_test.go")
	testBody := fmt.Sprintf(resourceHandlerTestTemplate, pascal, resourcePath)
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
		TestPath:          testPath,
		MigrationUpPath:   upPath,
		MigrationDownPath: downPath,
	}, nil
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
	for _, suffix := range []string{"s", "x", "z", "ch", "sh"} {
		if strings.HasSuffix(name, suffix) {
			return name + "es"
		}
	}
	return name + "s"
}

const modelTemplate = `package models

import "github.com/jcsvwinston/GoFrame/pkg/model"

// %s is a scaffold generated by goframe CLI.
type %s struct {
	model.BaseModel

	Name string ` + "`db:\"column:name;required;index\" validate:\"required\" admin:\"list,search\"`" + `
}
`

const handlerTemplate = `package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/jcsvwinston/GoFrame/pkg/router"
)

// %sHandler is a scaffold generated by goframe CLI.
type %sHandler struct{}

func New%sHandler() *%sHandler {
	return &%sHandler{}
}

func (h *%sHandler) Mount(r *router.Mux) {
	r.Get("/%s", h.List)
}

func (h *%sHandler) List(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": "%s handler scaffold ready",
	})
}
`

const serviceTemplate = `package services

import "context"

// %sService is a scaffold generated by goframe CLI.
type %sService struct{}

func New%sService() *%sService {
	return &%sService{}
}

func (s *%sService) Health(_ context.Context) string {
	return "ok"
}
`

const repositoryTemplate = `package repositories

import "context"

// %sRepository is a scaffold generated by goframe CLI.
type %sRepository struct{}

func New%sRepository() *%sRepository {
	return &%sRepository{}
}

func (r *%sRepository) Ping(_ context.Context) error {
	return nil
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

	"github.com/jcsvwinston/GoFrame/pkg/router"
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

// %[1]sHandler is a CRUD scaffold generated by goframe CLI.
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
	r.Resource("/%[2]s", router.ResourceHandlers{
		List:     h.List,
		Create:   h.Create,
		Retrieve: h.Get,
		Update:   h.Update,
		Delete:   h.Delete,
	})
}

func (h *%[1]sHandler) List(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	records := make([]%[1]sRecord, 0, len(h.items))
	for _, record := range h.items {
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
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	record, ok := h.lookup(id)
	if !ok {
		writeErrorJSON(w, http.StatusNotFound, "%[2]s record not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": record})
}

func (h *%[1]sHandler) Create(w http.ResponseWriter, r *http.Request) {
	payload, err := decode%[1]sPayload(r)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
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
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	payload, err := decode%[1]sPayload(r)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	h.mu.Lock()
	record, ok := h.items[id]
	if !ok {
		h.mu.Unlock()
		writeErrorJSON(w, http.StatusNotFound, "%[2]s record not found")
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
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	h.mu.Lock()
	if _, ok := h.items[id]; !ok {
		h.mu.Unlock()
		writeErrorJSON(w, http.StatusNotFound, "%[2]s record not found")
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

func writeErrorJSON(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
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

	"github.com/jcsvwinston/GoFrame/pkg/router"
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

	listRec := perform%[1]sRequest(t, r, http.MethodGet, "/%[2]s/", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected status %%d, got %%d", http.StatusOK, listRec.Code)
	}
	listBody := decode%[1]sJSON(t, listRec.Body.Bytes())
	if got := int(listBody["count"].(float64)); got != 1 {
		t.Fatalf("expected list count 1, got %%d", got)
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

	deleteRec := perform%[1]sRequest(t, r, http.MethodDelete, resourcePath, nil)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("expected status %%d, got %%d", http.StatusNoContent, deleteRec.Code)
	}

	finalListRec := perform%[1]sRequest(t, r, http.MethodGet, "/%[2]s/", nil)
	finalListBody := decode%[1]sJSON(t, finalListRec.Body.Bytes())
	if got := int(finalListBody["count"].(float64)); got != 0 {
		t.Fatalf("expected list count 0 after delete, got %%d", got)
	}
}

func Test%[1]sHandler_RejectsInvalidPayload(t *testing.T) {
	h := New%[1]sHandler()
	r := router.NewMux()
	h.Mount(r)

	rec := perform%[1]sRequest(t, r, http.MethodPost, "/%[2]s/", map[string]any{"name": "  "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %%d, got %%d", http.StatusBadRequest, rec.Code)
	}

	body := decode%[1]sJSON(t, rec.Body.Bytes())
	if got := body["error"]; got != "name is required" {
		t.Fatalf("expected validation error, got %%v", got)
	}
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
`
