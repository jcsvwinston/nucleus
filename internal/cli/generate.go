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
		return fmt.Errorf("usage: goframe generate <model|handler|migration|resource> <name>")
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
	TestPath          string
	MigrationUpPath   string
	MigrationDownPath string
}

func generateModelScaffold(outDir, snake, pascal string, force bool) (string, error) {
	path := filepath.Join(outDir, "models", snake+".go")
	body := fmt.Sprintf(modelTemplate, pascal, pascal)
	if err := writeFileIfNotExists(path, body, force); err != nil {
		return "", err
	}
	return path, nil
}

func generateHandlerScaffold(outDir, snake, pascal string, force bool) (string, error) {
	path := filepath.Join(outDir, "handlers", snake+"_handler.go")
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

func generateResourceScaffold(outDir, migrationsDir, snake, pascal string, force bool) (*resourceScaffoldResult, error) {
	modelPath, err := generateModelScaffold(outDir, snake, pascal, force)
	if err != nil {
		return nil, err
	}

	resourcePath := pluralizeResource(snake)
	handlerPath := filepath.Join(outDir, "handlers", snake+"_handler.go")
	handlerBody := fmt.Sprintf(
		resourceHandlerTemplate,
		pascal,       // 1 comment
		pascal,       // 2 type
		pascal,       // 3 constructor name
		pascal,       // 4 constructor return type
		pascal,       // 5 constructor literal
		pascal,       // 6 mount receiver
		resourcePath, // 7 route path
		pascal,       // 8 list receiver
		pascal,       // 9 list message
		pascal,       // 10 get receiver
		pascal,       // 11 get message
		pascal,       // 12 create receiver
		pascal,       // 13 create message
		pascal,       // 14 update receiver
		pascal,       // 15 update message
		pascal,       // 16 delete receiver
		pascal,       // 17 delete message
	)
	if err := writeFileIfNotExists(handlerPath, handlerBody, force); err != nil {
		return nil, err
	}

	testPath := filepath.Join(outDir, "handlers", snake+"_handler_test.go")
	testBody := fmt.Sprintf(resourceHandlerTestTemplate, pascal, pascal, resourcePath)
	if err := writeFileIfNotExists(testPath, testBody, force); err != nil {
		return nil, err
	}

	table := resourcePath
	if err := validateSQLIdentifier(table); err != nil {
		return nil, err
	}

	migrationName := "create_" + table + "_table"
	upSQL := fmt.Sprintf(resourceMigrationUpTemplate, table)
	downSQL := fmt.Sprintf(resourceMigrationDownTemplate, table)
	upPath, downPath, err := createMigrationPair(migrationsDir, migrationName, upSQL, downSQL)
	if err != nil {
		return nil, err
	}

	return &resourceScaffoldResult{
		ModelPath:         modelPath,
		HandlerPath:       handlerPath,
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

	Name string ` + "`db:\"name\" validate:\"required\" admin:\"list,search\"`" + `
}
`

const handlerTemplate = `package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// %sHandler is a scaffold generated by goframe CLI.
type %sHandler struct{}

func New%sHandler() *%sHandler {
	return &%sHandler{}
}

func (h *%sHandler) Mount(r chi.Router) {
	r.Get("/%s", h.List)
}

func (h *%sHandler) List(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": "%s handler scaffold ready",
	})
}
`

const resourceHandlerTemplate = `package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// %sHandler is a CRUD scaffold generated by goframe CLI.
type %sHandler struct{}

func New%sHandler() *%sHandler {
	return &%sHandler{}
}

func (h *%sHandler) Mount(r chi.Router) {
	r.Route("/%s", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Get("/{id}", h.Get)
		r.Put("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
	})
}

func (h *%sHandler) List(w http.ResponseWriter, _ *http.Request) {
	respondNotImplemented(w, "%s list not implemented")
}

func (h *%sHandler) Get(w http.ResponseWriter, _ *http.Request) {
	respondNotImplemented(w, "%s get not implemented")
}

func (h *%sHandler) Create(w http.ResponseWriter, _ *http.Request) {
	respondNotImplemented(w, "%s create not implemented")
}

func (h *%sHandler) Update(w http.ResponseWriter, _ *http.Request) {
	respondNotImplemented(w, "%s update not implemented")
}

func (h *%sHandler) Delete(w http.ResponseWriter, _ *http.Request) {
	respondNotImplemented(w, "%s delete not implemented")
}

func respondNotImplemented(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": message})
}
`

const resourceHandlerTestTemplate = `package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func Test%sHandler_List(t *testing.T) {
	h := New%sHandler()
	r := chi.NewRouter()
	h.Mount(r)

	req := httptest.NewRequest(http.MethodGet, "/%s/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected status %%d, got %%d", http.StatusNotImplemented, rec.Code)
	}
}
`

const resourceMigrationUpTemplate = `CREATE TABLE IF NOT EXISTS %s (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME,
	updated_at DATETIME,
	deleted_at DATETIME,
	name TEXT NOT NULL
);`

const resourceMigrationDownTemplate = `DROP TABLE IF EXISTS %s;`
