// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// `nucleus generate module <name>` — the package-per-feature slice
// generator (ADR-022 §4). Where `generate resource` spreads a feature
// across the layer packages (models/controllers/services/repositories/
// modules), this emits ONE self-contained package under internal/<name>/
// that carries everything the feature needs: model+storage, controller,
// mountable module with its own Policies and CSRF exemption, embedded
// migrations applied through rt.ApplyModuleMigrations, and an embedded
// page template. Mounting it is the whole integration — no rbac_policy.csv
// or nucleus.yml edits, no manual migrate step.
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jcsvwinston/nucleus/pkg/model"
)

type moduleScaffoldResult struct {
	PackageDir        string
	StoragePath       string
	ControllerPath    string
	ModulePath        string
	MigrationUpPath   string
	MigrationDownPath string
	TemplatePath      string
}

func generateModuleScaffold(outDir, snake, pascal, system string, force bool) (*moduleScaffoldResult, error) {
	table := pluralizeResource(snake)
	if err := validateSQLIdentifier(table); err != nil {
		return nil, err
	}

	pkgDir := filepath.Join(outDir, "internal", snake)
	for _, dir := range []string{pkgDir, filepath.Join(pkgDir, "migrations"), filepath.Join(pkgDir, "templates")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	storagePath := filepath.Join(pkgDir, snake+".go")
	if err := writeFileIfNotExists(storagePath, moduleSliceStorageSource(snake, pascal, table, system), force); err != nil {
		return nil, err
	}

	controllerPath := filepath.Join(pkgDir, "controller.go")
	if err := writeFileIfNotExists(controllerPath, fmt.Sprintf(moduleSliceControllerTemplate, snake, pascal, table), force); err != nil {
		return nil, err
	}

	modulePath := filepath.Join(pkgDir, "module.go")
	if err := writeFileIfNotExists(modulePath, fmt.Sprintf(moduleSliceModuleTemplate, snake, table, modulePageRoute(snake, table)), force); err != nil {
		return nil, err
	}

	// The migration pair is rendered for the configured dialect and embedded
	// in the package. The 000001_ prefix is deterministic on purpose: the
	// slice is meant to be lifted between applications, and a wall-clock
	// timestamp would make two generations of the same module diverge.
	upSQL, downSQL, err := model.BuildMigrationScaffoldForSystem(system, resourceScaffoldMeta(table, pascal))
	if err != nil {
		return nil, err
	}
	upPath := filepath.Join(pkgDir, "migrations", "000001_create_"+table+".up.sql")
	downPath := filepath.Join(pkgDir, "migrations", "000001_create_"+table+".down.sql")
	if err := writeFileIfNotExists(upPath, upSQL, force); err != nil {
		return nil, err
	}
	if err := writeFileIfNotExists(downPath, downSQL, force); err != nil {
		return nil, err
	}

	templatePath := filepath.Join(pkgDir, "templates", "index.html")
	if err := writeFileIfNotExists(templatePath, fmt.Sprintf(moduleSliceTemplateHTML, pascal, table), force); err != nil {
		return nil, err
	}

	return &moduleScaffoldResult{
		PackageDir:        pkgDir,
		StoragePath:       storagePath,
		ControllerPath:    controllerPath,
		ModulePath:        modulePath,
		MigrationUpPath:   upPath,
		MigrationDownPath: downPath,
		TemplatePath:      templatePath,
	}, nil
}

// modulePageRoute returns the path the scaffolded HTML page mounts at. The
// page normally lives at /<name> next to the /<plural> JSON resource, but
// when the caller passes an already-plural name (products, notes, users…)
// the two paths collide: the resource's Index also answers GET /<plural>,
// and registering the pair panics the router at boot ("pattern GET /products
// conflicts with pattern GET /products"). In that case the page moves to
// /<name>/page — a literal segment, more specific than the resource's
// /<name>/{id}, so both register cleanly.
func modulePageRoute(snake, resourcePath string) string {
	if snake == resourcePath {
		return "/" + snake + "/page"
	}
	return "/" + snake
}

// moduleSliceStorageSource renders the slice's model + storage file with the
// SQL bound to the same dialect the embedded migration was rendered for —
// the same cannot-disagree rule as the resource repository (DX-21).
func moduleSliceStorageSource(snake, pascal, table, system string) string {
	p := func(n int) string { return resourcePlaceholder(system, n) }

	listSQL := fmt.Sprintf("SELECT id, name, created_at, updated_at FROM %s WHERE deleted_at IS NULL ORDER BY id", table)
	getSQL := fmt.Sprintf("SELECT id, name, created_at, updated_at FROM %s WHERE id = %s AND deleted_at IS NULL", table, p(1))
	updateSQL := fmt.Sprintf("UPDATE %s SET name = %s, updated_at = %s WHERE id = %s AND deleted_at IS NULL", table, p(1), p(2), p(3))
	deleteSQL := fmt.Sprintf("UPDATE %s SET deleted_at = %s WHERE id = %s AND deleted_at IS NULL", table, p(1), p(2))

	var createBody string
	switch system {
	case "postgresql":
		insertSQL := fmt.Sprintf("INSERT INTO %s (name, created_at, updated_at) VALUES ($1, $2, $3) RETURNING id", table)
		createBody = fmt.Sprintf("\tif err := s.db.QueryRowContext(ctx, %q, params.Name, now, now).Scan(&id); err != nil {\n\t\treturn Record{}, err\n\t}", insertSQL)
	case "mssql":
		insertSQL := fmt.Sprintf("INSERT INTO %s (name, created_at, updated_at) OUTPUT INSERTED.id VALUES (@p1, @p2, @p3)", table)
		createBody = fmt.Sprintf("\tif err := s.db.QueryRowContext(ctx, %q, params.Name, now, now).Scan(&id); err != nil {\n\t\treturn Record{}, err\n\t}", insertSQL)
	case "oracle":
		insertSQL := fmt.Sprintf("INSERT INTO %s (name, created_at, updated_at) VALUES (:1, :2, :3) RETURNING id INTO :4", table)
		createBody = fmt.Sprintf("\t// Oracle returns the generated identity through an OUT bind parameter.\n\tvar generated int64\n\tif _, err := s.db.ExecContext(ctx, %q, params.Name, now, now, sql.Out{Dest: &generated}); err != nil {\n\t\treturn Record{}, err\n\t}\n\tid = uint(generated)", insertSQL)
	default: // sqlite, mysql
		insertSQL := fmt.Sprintf("INSERT INTO %s (name, created_at, updated_at) VALUES (?, ?, ?)", table)
		createBody = fmt.Sprintf("\tres, err := s.db.ExecContext(ctx, %q, params.Name, now, now)\n\tif err != nil {\n\t\treturn Record{}, err\n\t}\n\tlastID, err := res.LastInsertId()\n\tif err != nil {\n\t\treturn Record{}, err\n\t}\n\tid = uint(lastID)", insertSQL)
	}

	return fmt.Sprintf(moduleSliceStorageTemplate, snake, pascal, table, listSQL, getSQL, updateSQL, deleteSQL, createBody, system)
}

const moduleSliceStorageTemplate = `// Package %[1]s is the %[2]s feature slice, generated by nucleus CLI.
// Everything the feature needs lives in this package: model + storage
// (this file), controller.go, and module.go (the mountable module with
// its embedded migrations, page template, policy rows and CSRF
// exemption). SQL statements were rendered for the %[9]s dialect at
// scaffold time; regenerate with --dialect (or edit the SQL) if the
// application changes engines.
package %[1]s

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ErrNotFound reports a %[3]s row that does not exist (or is soft-deleted).
var ErrNotFound = errors.New("%[3]s record not found")

type Record struct {
	ID        uint      ` + "`json:\"id\"`" + `
	Name      string    ` + "`json:\"name\"`" + `
	CreatedAt time.Time ` + "`json:\"created_at\"`" + `
	UpdatedAt time.Time ` + "`json:\"updated_at\"`" + `
}

type ListParams struct {
	Query string
}

type CreateParams struct {
	Name string
}

type UpdateParams struct {
	Name string
}

// Storage runs real SQL against the framework-managed *sql.DB — injected
// by the module's OnStart hook via rt.DB(). The framework owns the pool's
// lifecycle; Storage never opens or closes connections itself.
type Storage struct {
	db *sql.DB
}

func NewStorage(db *sql.DB) *Storage {
	return &Storage{db: db}
}

func (s *Storage) List(ctx context.Context, params ListParams) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, %[4]q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	query := strings.ToLower(strings.TrimSpace(params.Query))
	records := make([]Record, 0, 16)
	for rows.Next() {
		var record Record
		if err := rows.Scan(&record.ID, &record.Name, &record.CreatedAt, &record.UpdatedAt); err != nil {
			return nil, err
		}
		if query != "" && !strings.Contains(strings.ToLower(record.Name), query) {
			continue
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Storage) Get(ctx context.Context, id uint) (Record, error) {
	var record Record
	err := s.db.QueryRowContext(ctx, %[5]q, id).
		Scan(&record.ID, &record.Name, &record.CreatedAt, &record.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Storage) Create(ctx context.Context, params CreateParams) (Record, error) {
	now := time.Now().UTC()
	var id uint
%[8]s
	return Record{ID: id, Name: params.Name, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Storage) Update(ctx context.Context, id uint, params UpdateParams) (Record, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, %[6]q, params.Name, now, id)
	if err != nil {
		return Record{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Record{}, err
	}
	if affected == 0 {
		return Record{}, ErrNotFound
	}
	return s.Get(ctx, id)
}

func (s *Storage) Delete(ctx context.Context, id uint) error {
	res, err := s.db.ExecContext(ctx, %[7]q, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
`

const moduleSliceControllerTemplate = `package %[1]s

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	nucleusErrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

type payload struct {
	Name string ` + "`json:\"name\"`" + `
}

// Controller implements the nucleus REST Resource sub-interfaces
// (Indexer, Shower, Creator, Updater, Destroyer); module.go registers it
// via r.Resource — no adapter needed.
type Controller struct {
	storage *Storage
}

func NewController(storage *Storage) *Controller {
	return &Controller{storage: storage}
}

// Index handles GET /%[3]s.
func (ctl *Controller) Index(c *nucleus.Context) error {
	records, err := ctl.storage.List(c.Request.Context(), ListParams{
		Query: strings.TrimSpace(c.Query("q")),
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "unable to list %[3]s"})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"data":  records,
		"count": len(records),
	})
}

// Show handles GET /%[3]s/{id}.
func (ctl *Controller) Show(c *nucleus.Context) error {
	id, err := parseID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	record, err := ctl.storage.Get(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "%[2]s " + strconv.FormatUint(uint64(id), 10) + " not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "unable to fetch %[2]s"})
	}
	return c.JSON(http.StatusOK, map[string]any{"data": record})
}

// Create handles POST /%[3]s.
func (ctl *Controller) Create(c *nucleus.Context) error {
	p, err := bindPayload(c)
	if err != nil {
		return c.JSON(payloadStatus(err), map[string]string{"error": err.Error()})
	}
	record, err := ctl.storage.Create(c.Request.Context(), CreateParams{Name: p.Name})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "unable to create %[2]s"})
	}
	return c.JSON(http.StatusCreated, map[string]any{"data": record})
}

// Update handles PUT /%[3]s/{id}.
func (ctl *Controller) Update(c *nucleus.Context) error {
	id, err := parseID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	p, err := bindPayload(c)
	if err != nil {
		return c.JSON(payloadStatus(err), map[string]string{"error": err.Error()})
	}
	record, err := ctl.storage.Update(c.Request.Context(), id, UpdateParams{Name: p.Name})
	if errors.Is(err, ErrNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "%[2]s " + strconv.FormatUint(uint64(id), 10) + " not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "unable to update %[2]s"})
	}
	return c.JSON(http.StatusOK, map[string]any{"data": record})
}

// Destroy handles DELETE /%[3]s/{id}.
func (ctl *Controller) Destroy(c *nucleus.Context) error {
	id, err := parseID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	err = ctl.storage.Delete(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "%[2]s " + strconv.FormatUint(uint64(id), 10) + " not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "unable to delete %[2]s"})
	}
	return c.NoContent()
}

func bindPayload(c *nucleus.Context) (payload, error) {
	var p payload
	if err := c.BindJSON(&p); err != nil {
		// BindJSON already classifies what went wrong — a body over the
		// 1 MiB cap is a 413, not a JSON error — so keep its verdict and
		// only reword the one case it leaves generic.
		var domainErr *nucleusErrors.DomainError
		if errors.As(err, &domainErr) && domainErr.StatusCode != http.StatusBadRequest {
			return p, domainErr
		}
		return p, errors.New("request body must be valid JSON")
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return p, errors.New("name is required")
	}
	return p, nil
}

// payloadStatus maps a bindPayload error to its HTTP status: a classified
// error (413 for an oversized body) keeps its own, anything else is a 400.
func payloadStatus(err error) int {
	var domainErr *nucleusErrors.DomainError
	if errors.As(err, &domainErr) && domainErr.StatusCode > 0 {
		return domainErr.StatusCode
	}
	return http.StatusBadRequest
}

func parseID(c *nucleus.Context) (uint, error) {
	raw := strings.TrimSpace(c.Param("id"))
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

const moduleSliceModuleTemplate = `package %[1]s

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

//go:embed migrations/*.sql
var migrationsDir embed.FS

//go:embed templates/*.html
var templatesDir embed.FS

// Module returns the mountable %[1]s feature slice. Everything travels
// with it — routes, storage, policy rows, CSRF exemption, embedded
// migrations and the page template — so
//
//	nucleus.New().Mount(%[1]s.Module())
//
// is the whole integration: no rbac_policy.csv rows to add, no
// csrf_exempt_paths edit, no manual migrate step.
func Module() nucleus.ModuleSpec {
	var storage *Storage

	return nucleus.Module[struct{}]{
		Name:       "%[1]s",
		Migrations: subFS(migrationsDir, "migrations"),
		Templates:  subFS(templatesDir, "templates"),

		// The slice opens exactly its own routes under the default-deny
		// enforcer. Scope these rows down (or let the host CSV deny them —
		// an operator deny always overrides) when you add authentication.
		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/%[1]s", Action: "read"},
			{Subject: "anonymous", Object: "/%[2]s", Action: "*"},
			{Subject: "anonymous", Object: "/%[2]s/*", Action: "*"},
		},
		// The JSON API takes cookie-less POST/PUT/DELETE; the page route
		// stays under CSRF protection.
		CSRFExempt: []string{"/%[2]s"},

		OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
			// Deliberate schema application at module start: embedded SQL
			// through the module-scoped migration ledger, idempotent across
			// restarts. Delete this call to keep schema changes
			// operator-driven and ship the SQL yourself.
			if err := rt.ApplyModuleMigrations(); err != nil {
				return err
			}
			db := rt.DB()
			if db == nil {
				return fmt.Errorf("%[1]s: no managed database configured (set databases.default.url in nucleus.yml)")
			}
			storage = NewStorage(db)
			return nil
		},

		Routes: func(r nucleus.Router, _ struct{}) {
			// The HTML page and the JSON resource register on distinct
			// paths; when the module name is already plural the page moves
			// to /<name>/page so it cannot collide with the resource Index.
			r.Get("%[3]s", func(c *nucleus.Context) error {
				// Engine-backed render: the embedded template registers
				// under the module-name namespace.
				return c.Render(http.StatusOK, "%[1]s/index.html", map[string]interface{}{"title": "%[1]s"})
			})
			r.Resource("/%[2]s", NewController(storage), nucleus.Methods(
				nucleus.Index,
				nucleus.Show,
				nucleus.Create,
				nucleus.Update,
				nucleus.Destroy,
			))
		},
	}.Build()
}

// subFS re-roots an embedded directory. The paths are constants this file
// declares, so a failure is a broken build of the module itself — fail
// loudly at init rather than serving without migrations or templates.
func subFS(parent embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(parent, dir)
	if err != nil {
		panic(fmt.Sprintf("%[1]s: embedded %%s: %%v", dir, err))
	}
	return sub
}
`

const moduleSliceTemplateHTML = `<!doctype html>
<title>%[1]s</title>
<h1>{{.title}}</h1>
<p>Feature slice scaffold. The JSON API lives at <code>/%[2]s</code>.</p>
`
