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
	"strconv"
	"strings"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
	"github.com/jcsvwinston/nucleus/pkg/model"
)

type moduleScaffoldResult struct {
	PackageDir        string
	StoragePath       string
	ControllerPath    string
	ModulePath        string
	TestPath          string
	MigrationUpPath   string
	MigrationDownPath string
	TemplatePath      string
}

// The data layers `generate module --data` can render the slice's storage
// on: plain database/sql statements bound to the configured dialect (the
// default), or the suite's Quark ORM over the same framework-managed pool.
const (
	moduleDataSQL   = "sql"
	moduleDataQuark = "quark"
)

// quarkModulePath is the `go get` target the Quark variant needs. The
// framework module does not require Quark — the generated project does —
// so the generator prints the line instead of running it.
const quarkModulePath = "github.com/jcsvwinston/quark"

// moduleDataLayer validates the --data flag value.
func moduleDataLayer(raw string) (string, error) {
	switch raw {
	case "", moduleDataSQL:
		return moduleDataSQL, nil
	case moduleDataQuark:
		return moduleDataQuark, nil
	}
	return "", fmt.Errorf("unsupported --data %q (supported: %s, %s)", raw, moduleDataSQL, moduleDataQuark)
}

// driverForSystem maps a migration dialect name (what resolveScaffoldSystem
// returns) to the database driver module the framework publishes for it.
func driverForSystem(system string) (knownproviders.Provider, bool) {
	name := map[string]string{
		"sqlite":     "sqlite",
		"postgresql": "pgx",
		"mysql":      "mysql",
		"mssql":      "sqlserver",
		"oracle":     "oracle",
	}[system]
	if name == "" {
		return knownproviders.Provider{}, false
	}
	return knownproviders.DBDriver(name)
}

// quarkDriverName is the database/sql driver name Quark auto-detects its
// dialect from, per migration dialect.
func quarkDriverName(system string) string {
	switch system {
	case "postgresql":
		return "pgx"
	case "mssql":
		return "sqlserver"
	default: // sqlite, mysql, oracle share the name
		return system
	}
}

// quarkDriverModule is Quark's own driver module for the dialect — the
// piece that teaches Quark the engine's duplicate-key error.
func quarkDriverModule(system string) string {
	dir := map[string]string{
		"sqlite":     "sqlite",
		"postgresql": "postgres",
		"mysql":      "mysql",
		"mssql":      "mssql",
		"oracle":     "oracle",
	}[system]
	if dir == "" {
		return ""
	}
	return quarkModulePath + "/drivers/" + dir
}

// modulePolicyRows renders the RBAC rows the generated module opens under
// the default-deny enforcer. The default (NU-14) lets anonymous callers READ
// the page and the JSON API and nothing else: a write needs an authenticated
// subject, which the application adds as rows for its own roles. The open
// variant — every verb for anonymous, what the scaffold used to emit
// unconditionally — is what --with-policy asks for, for a development spike
// that has no authentication yet.
//
// On an application that builds no enforcer — WithoutDefaults() without
// WithAuthz(), the api starter's contract — the rows are written commented
// out (enforced is false). There is nothing to load them into: the boot
// discards them with an ERROR line and every write of the module answers
// anyone, and from v2.0.0 the application refuses to start with rows it
// cannot enforce (DEP-2026-017) — a fresh starter with its first generated
// module must start, now and then. The rows stay in the file, so that
// adding WithAuthz() to main.go and uncommenting them is the whole change.
// With --with-policy on such a root the rows commented out are the open
// ones: what the file says is what the enforcer would load.
func modulePolicyRows(snake, resource string, open, enforced bool) string {
	literal := policyRowsLiteral(snake, resource, open)
	switch {
	case !enforced:
		// The open variant keeps its warning: uncommented under an enforcer
		// the rows below open every verb, and the file is where a reader
		// learns that before uncommenting them.
		scope := ""
		if open {
			scope = `		// --with-policy: the rows below open EVERY verb to anonymous callers,
		// including writes; scope them down before the slice faces a network.
`
		}
		return `		// This application is built WithoutDefaults() without WithAuthz(), so
		// it has no RBAC enforcer to load policy rows into: rows it cannot
		// enforce are discarded at boot with an ERROR line, and from v2.0.0
		// the application refuses to start with them (DEP-2026-017). The
		// rows this module would declare on the default stack are kept here,
		// commented out: add WithAuthz() beside WithoutDefaults() in main.go,
		// uncomment them, and they are enforced (the module's test boots the
		// same way; add WithAuthz() there too). Until then every route of
		// this module answers anyone.
` + scope + commentOutRows(literal)
	case open:
		return `		// --with-policy: EVERY verb is open to anonymous callers, including
		// writes. This is a development default; scope it down (or let the
		// host CSV deny it — an operator deny always overrides) before the
		// slice faces a network.
` + literal
	default:
		return `		// Anonymous callers can read the page and the JSON API; a write
		// (POST/PUT/DELETE) needs an authenticated subject. Add rows for your
		// roles here — {Subject: "editor", Object: "/` + resource + `/*", Action: "*"} —
		// or regenerate with --with-policy for a development spike with
		// every verb open. An operator deny in the host CSV always overrides.
` + literal
	}
}

// policyRowsLiteral is the Policies field of the generated module, indented
// for the module literal: the open rows, or the read-only ones.
func policyRowsLiteral(snake, resource string, open bool) string {
	if open {
		return fmt.Sprintf(`		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/%[1]s", Action: "read"},
			{Subject: "anonymous", Object: "/%[2]s", Action: "*"},
			{Subject: "anonymous", Object: "/%[2]s/*", Action: "*"},
		},`, snake, resource)
	}
	return fmt.Sprintf(`		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/%[1]s", Action: "read"},
			{Subject: "anonymous", Object: "/%[2]s", Action: "read"},
			{Subject: "anonymous", Object: "/%[2]s/*", Action: "read"},
		},`, snake, resource)
}

// commentOutRows turns the Policies literal into line comments at the same
// indentation, so that uncommenting it is deleting the `// ` on each line.
func commentOutRows(literal string) string {
	lines := strings.Split(literal, "\n")
	for i, line := range lines {
		lines[i] = "\t\t// " + strings.TrimPrefix(line, "\t\t")
	}
	return strings.Join(lines, "\n")
}

// compositionRootEnforcesPolicies reports whether the project's composition
// root builds an RBAC enforcer for a module's policy rows: the default stack
// does, and so does a WithoutDefaults() application that carries WithAuthz().
// One built WithoutDefaults() without it has nowhere to load the rows into —
// they are discarded at boot, and from v2.0.0 refused (DEP-2026-017) — so the
// generator writes them commented out there. The root is the package main
// in outDir — the file --mount edits; --config is not consulted for this —
// read with doctor's reader (readCompositionRoot: every file of the
// package, so an option called from a helper file counts; only calls
// count, in either spelling of the option). A project whose root cannot be
// found or parsed — no package main beside nucleus.yml nor under cmd/*/, a
// hand-written composition root in another layout, or none yet — is taken
// as the default stack: the rows are written as they always were, and the
// boot says what it discards.
func compositionRootEnforcesPolicies(outDir string) bool {
	root, ok := readCompositionRoot(filepath.Join(outDir, "nucleus.yml"))
	return !ok || !root.omits("WithAuthz")
}

// moduleRowsNote qualifies "policy rows" wherever a generated file lists
// what the module carries: on a root without an enforcer they are commented
// out, and a header saying they travel with the module would contradict the
// block, a few lines below, that says they are not in force.
func moduleRowsNote(enforced bool) string {
	if enforced {
		return ""
	}
	return " (commented out: this application builds no enforcer)"
}

func generateModuleScaffold(outDir, snake, pascal, system string, force bool, openPolicy, enforced bool, data string) (*moduleScaffoldResult, error) {
	table := pluralizeResource(snake)
	if err := validateSQLIdentifier(table); err != nil {
		return nil, err
	}
	goModulePath, _, err := detectModulePath(outDir)
	if err != nil {
		return nil, err
	}

	pkgDir := filepath.Join(outDir, "internal", snake)
	for _, dir := range []string{pkgDir, filepath.Join(pkgDir, "migrations"), filepath.Join(pkgDir, "templates")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	storagePath := filepath.Join(pkgDir, snake+".go")
	storageSource := moduleSliceStorageSource(snake, pascal, table, system, enforced)
	if data == moduleDataQuark {
		storageSource = moduleSliceQuarkStorageSource(snake, pascal, table, system, enforced)
	}
	if err := writeFileIfNotExists(storagePath, storageSource, force); err != nil {
		return nil, err
	}

	controllerPath := filepath.Join(pkgDir, "controller.go")
	if err := writeFileIfNotExists(controllerPath, moduleSliceControllerSource(snake, pascal, table), force); err != nil {
		return nil, err
	}

	// The SQL storage is constructed in one line; the Quark one derives a
	// client over the pool and can fail, so the module reports it.
	storageInit := "storage = NewStorage(db)"
	if data == moduleDataQuark {
		storageInit = "var err error\n\t\t\tif storage, err = NewStorage(db); err != nil {\n\t\t\t\treturn fmt.Errorf(\"" + snake + ": quark client over the managed pool: %w\", err)\n\t\t\t}"
	}
	modulePath := filepath.Join(pkgDir, "module.go")
	if err := writeFileIfNotExists(modulePath, fmt.Sprintf(moduleSliceModuleTemplate, snake, table, modulePageRoute(snake, table), modulePolicyRows(snake, table, openPolicy, enforced), storageInit, moduleRowsNote(enforced)), force); err != nil {
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

	// A slice ships with a test that boots it — `go test ./...` in a fresh
	// project used to print "[no test files]", which reads as "nothing to
	// check" when the truth was "nothing checked".
	testPath := filepath.Join(pkgDir, "module_test.go")
	if err := writeFileIfNotExists(testPath, moduleSliceTestSource(goModulePath, snake, table, system, openPolicy, enforced), force); err != nil {
		return nil, err
	}

	return &moduleScaffoldResult{
		PackageDir:        pkgDir,
		StoragePath:       storagePath,
		ControllerPath:    controllerPath,
		ModulePath:        modulePath,
		TestPath:          testPath,
		MigrationUpPath:   upPath,
		MigrationDownPath: downPath,
		TemplatePath:      templatePath,
	}, nil
}

// moduleSliceTestSource renders the slice's test. It mounts the module on
// an in-process application (pkg/nucleustest) and drives the JSON API over
// real HTTP, so the embedded migration, the policy rows and the CSRF
// exemption are all exercised — not a fake repository — and a missing
// record answers in the framework's error shape. The SQL was
// rendered for one dialect, so the test opens that engine: a temporary
// SQLite file when the dialect is sqlite, otherwise the database named by
// NUCLEUS_TEST_DATABASE_URL (skipped when unset, never silently green).
// The POST expectation follows the policy the module was generated with:
// 403 for the read-only default, 201 with --with-policy — and 201 on an
// application that builds no enforcer (enforced is false), where the test
// boots WithoutDefaults() as main.go does and the rows are commented out.
func moduleSliceTestSource(modulePath, snake, table, system string, openPolicy, enforced bool) string {
	driverImport := ""
	if p, ok := driverForSystem(system); ok {
		driverImport = "\n\t// The test boots a real application against a database, so the test\n\t// binary links the driver the way main.go does.\n\t_ " + strconv.Quote(p.Module) + "\n"
	}
	// The database the test boots on. sqlite gets a temporary file from
	// nucleustest; every other dialect gets a named helper (rendered as a
	// top-level function, so the file stays gofmt-clean) that reads
	// NUCLEUS_TEST_DATABASE_URL and skips when it is unset.
	databases := "nucleustest.TempSQLite(t)"
	helper := ""
	extraImports := ""
	appImport := ""
	if system != "sqlite" {
		databases = "testDatabases(t)"
		helper = fmt.Sprintf(`
// testDatabases points the test at a %[1]s database: the slice's SQL was
// rendered for that dialect, so a temporary SQLite file would not prove
// it. The test skips when NUCLEUS_TEST_DATABASE_URL is unset — never
// silently green.
func testDatabases(t *testing.T) map[string]app.DatabaseConfig {
	t.Helper()
	url := os.Getenv("NUCLEUS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set NUCLEUS_TEST_DATABASE_URL to a %[1]s database to run this test")
	}
	return map[string]app.DatabaseConfig{"default": {URL: url}}
}
`, system)
		extraImports = "\t\"os\"\n"
		appImport = "\t\"github.com/jcsvwinston/nucleus/pkg/app\"\n"
	}
	postStatus := "http.StatusForbidden"
	postComment := "// The default policy opens reads only: an anonymous write is refused\n\t// by the default-deny enforcer. Add a role row in module.go (or\n\t// regenerate with --with-policy) and change this expectation."
	// What the test reads back once the write landed: only an application
	// that lets it land — the open policy, or no enforcer at all — decodes
	// the record it created and finds it in the list.
	readBack := ""
	boot := ""
	// What the boot enforces, for the test's doc comment. The CSRF
	// exemption is not claimed: nucleus.New() without a configuration
	// leaves csrf_enabled off, so no CSRF middleware runs on this boot.
	inForce := "the module's own policy rows are in\n// force"
	switch {
	case !enforced:
		postStatus = "http.StatusCreated"
		postComment = "// This application builds no RBAC enforcer (WithoutDefaults() without\n\t// WithAuthz() in main.go), so the module's rows are commented out in\n\t// module.go and the write lands: every route of the module answers\n\t// anyone until WithAuthz() is added (DEP-2026-017). Once main.go\n\t// carries WithAuthz() and the rows are uncommented, add WithAuthz() to\n\t// the boot above, change this expectation to http.StatusForbidden and\n\t// drop the read-back."
		if openPolicy {
			// The rows commented out are the open ones: uncommented under
			// an enforcer they still let the anonymous write land, so the
			// edits the read-only comment asks for would turn the test red.
			postComment = "// This application builds no RBAC enforcer (WithoutDefaults() without\n\t// WithAuthz() in main.go), so the module's rows are commented out in\n\t// module.go and the write lands: every route of the module answers\n\t// anyone until WithAuthz() is added (DEP-2026-017). The rows commented\n\t// out are the open ones (--with-policy): once main.go carries\n\t// WithAuthz() and they are uncommented, add WithAuthz() to the boot\n\t// above and this write still lands. Scope the rows in module.go before\n\t// the slice faces a network; then expect http.StatusForbidden here and\n\t// drop the read-back."
		}
		readBack = fmt.Sprintf(moduleSliceTestReadBack, snake, table)
		boot = "\n\t\t// The same core-only boot as main.go — WithoutDefaults() without\n\t\t// WithAuthz(): no RBAC enforcer. When main.go gains WithAuthz(), add\n\t\t// it here too.\n\t\tWithoutDefaults()."
		inForce = "nothing is enforced on this boot (the policy rows are\n// commented out in module.go: this application builds no enforcer)"
	case openPolicy:
		postStatus = "http.StatusCreated"
		postComment = "// --with-policy opened every verb to anonymous callers, so the write\n\t// lands. Scope the rows in module.go before the slice faces a network\n\t// and change this expectation to http.StatusForbidden."
		readBack = fmt.Sprintf(moduleSliceTestReadBack, snake, table)
	}
	return fmt.Sprintf(moduleSliceTestTemplate, snake, table, modulePath, driverImport, databases, extraImports, appImport, postStatus, postComment, helper, readBack, boot, inForce)
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
func moduleSliceStorageSource(snake, pascal, table, system string, enforced bool) string {
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

	return fmt.Sprintf(moduleSliceStorageTemplate, snake, pascal, table, listSQL, getSQL, updateSQL, deleteSQL, createBody, system, moduleRowsNote(enforced))
}

const moduleSliceStorageTemplate = `// Package %[1]s is the %[2]s feature slice, generated by nucleus CLI.
// Everything the feature needs lives in this package: model + storage
// (this file), controller.go, and module.go (the mountable module with
// its embedded migrations, page template, policy rows%[10]s and CSRF
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

// moduleSliceControllerSource renders the slice's JSON API: five typed
// endpoints (nucleus.Handle) whose request and response types are the
// contract. The framework binds and validates the input, writes the
// output, derives the OpenAPI operation from the two types (NU-145), and
// answers every error the handlers return in its one error shape — the
// envelope, or RFC 9457 problem details when the client asks — so the
// generated code writes no error body of its own (NU-136).
func moduleSliceControllerSource(snake, pascal, table string) string {
	return fmt.Sprintf(moduleSliceControllerTemplate, snake, pascal, table, toPascalCase(table))
}

const moduleSliceControllerTemplate = `package %[1]s

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	nucleusErrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// Controller is the JSON API of the %[1]s slice: five typed endpoints
// (nucleus.Handle). Each handler's signature says what it reads and what
// it answers, so the framework binds and validates the input, writes the
// output with the success status, describes both in the OpenAPI document
// the application serves at /openapi.json, and answers every error the
// handlers return in its one error shape — the envelope, or RFC 9457
// problem details for a client that asks for them. No handler here writes
// an error body of its own.
type Controller struct {
	storage *Storage
}

func NewController(storage *Storage) *Controller {
	return &Controller{storage: storage}
}

// Routes registers the JSON API under path (module.go passes "/%[3]s").
func (ctl *Controller) Routes(r nucleus.Router, path string) {
	nucleus.Handle(r, http.MethodGet, path, ctl.List%[4]s)
	nucleus.Handle(r, http.MethodPost, path, ctl.Create%[2]s, nucleus.Status(http.StatusCreated))
	nucleus.Handle(r, http.MethodGet, path+"/{id}", ctl.Show%[2]s)
	nucleus.Handle(r, http.MethodPut, path+"/{id}", ctl.Update%[2]s)
	nucleus.Handle(r, http.MethodDelete, path+"/{id}", ctl.Delete%[2]s)
}

// ListQuery is what GET /%[3]s reads from the query string.
type ListQuery struct {
	Query string ` + "`query:\"q\" doc:\"Only the records whose name contains this text\"`" + `
}

// %[2]sID names the record GET, PUT and DELETE /%[3]s/{id} act on. The id
// is bound from the path: a value that is not an integer is a 400 and a
// zero is a 422 before the handler runs.
type %[2]sID struct {
	ID uint ` + "`path:\"id\" validate:\"required\"`" + `
}

// %[2]sInput is the body POST /%[3]s reads. The validate tags run before
// the handler, and the document carries them.
type %[2]sInput struct {
	Name string ` + "`json:\"name\" validate:\"required,max=200\"`" + `
}

// %[2]sUpdate is what PUT /%[3]s/{id} reads: the id from the path, the
// name from the body. A body cannot overwrite the id the route named.
type %[2]sUpdate struct {
	ID   uint   ` + "`path:\"id\" validate:\"required\"`" + `
	Name string ` + "`json:\"name\" validate:\"required,max=200\"`" + `
}

// %[2]sList is what GET /%[3]s answers.
type %[2]sList struct {
	Data  []Record ` + "`json:\"data\"`" + `
	Count int      ` + "`json:\"count\"`" + `
}

// %[2]sResponse is what the single-record endpoints answer.
type %[2]sResponse struct {
	Data Record ` + "`json:\"data\"`" + `
}

// List%[4]s handles GET /%[3]s.
func (ctl *Controller) List%[4]s(c *nucleus.Context, q ListQuery) (%[2]sList, error) {
	records, err := ctl.storage.List(c.Request.Context(), ListParams{Query: strings.TrimSpace(q.Query)})
	if err != nil {
		return %[2]sList{}, fmt.Errorf("%[1]s: list: %%w", err)
	}
	return %[2]sList{Data: records, Count: len(records)}, nil
}

// Show%[2]s handles GET /%[3]s/{id}.
func (ctl *Controller) Show%[2]s(c *nucleus.Context, in %[2]sID) (%[2]sResponse, error) {
	record, err := ctl.storage.Get(c.Request.Context(), in.ID)
	if err != nil {
		return %[2]sResponse{}, storageError(in.ID, err)
	}
	return %[2]sResponse{Data: record}, nil
}

// Create%[2]s handles POST /%[3]s and answers 201 with the created record.
func (ctl *Controller) Create%[2]s(c *nucleus.Context, in %[2]sInput) (%[2]sResponse, error) {
	name, err := cleanName(in.Name)
	if err != nil {
		return %[2]sResponse{}, err
	}
	record, err := ctl.storage.Create(c.Request.Context(), CreateParams{Name: name})
	if err != nil {
		return %[2]sResponse{}, fmt.Errorf("%[1]s: create: %%w", err)
	}
	return %[2]sResponse{Data: record}, nil
}

// Update%[2]s handles PUT /%[3]s/{id}.
func (ctl *Controller) Update%[2]s(c *nucleus.Context, in %[2]sUpdate) (%[2]sResponse, error) {
	name, err := cleanName(in.Name)
	if err != nil {
		return %[2]sResponse{}, err
	}
	record, err := ctl.storage.Update(c.Request.Context(), in.ID, UpdateParams{Name: name})
	if err != nil {
		return %[2]sResponse{}, storageError(in.ID, err)
	}
	return %[2]sResponse{Data: record}, nil
}

// Delete%[2]s handles DELETE /%[3]s/{id} and answers 204: an output of
// struct{} is no content.
func (ctl *Controller) Delete%[2]s(c *nucleus.Context, in %[2]sID) (struct{}, error) {
	if err := ctl.storage.Delete(c.Request.Context(), in.ID); err != nil {
		return struct{}{}, storageError(in.ID, err)
	}
	return struct{}{}, nil
}

// cleanName trims the name and refuses one that is only whitespace with
// the same 422 the validate tags answer for a missing one.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nucleusErrors.ValidationFailed(map[string]string{"name": "this field is required"})
	}
	return name, nil
}

// storageError turns a Storage error into the error the framework answers:
// ErrNotFound is the 404 the client reads as NOT_FOUND; anything else is
// wrapped and answered as a 500 whose detail goes to the log, not on the
// wire.
func storageError(id uint, err error) error {
	if errors.Is(err, ErrNotFound) {
		return nucleusErrors.NotFound("%[1]s", strconv.FormatUint(uint64(id), 10))
	}
	return fmt.Errorf("%[1]s: %%w", err)
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
// with it — routes, storage, policy rows%[6]s, CSRF exemption, embedded
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

%[4]s
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
			%[5]s
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
			// The JSON API: five typed endpoints under /%[2]s (controller.go),
			// described in the application's OpenAPI document from their
			// request and response types.
			NewController(storage).Routes(r, "/%[2]s")
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

const moduleSliceTestTemplate = `package %[1]s_test

import (
	"net/http"
%[6]s	"strings"
	"testing"

%[7]s	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"

	"%[3]s/internal/%[1]s"
%[4]s)

// TestModuleServesItsResource mounts the slice on an in-process application
// and drives it through the test kit's client: the embedded migration is
// applied on start, %[13]s, and the JSON API answers in the slice's own
// Record type. Nothing here is faked — it is the same boot path main.go
// runs.
func TestModuleServesItsResource(t *testing.T) {
	if testing.Short() {
		t.Skip("boots an in-process application; skipped with -short")
	}

	srv := nucleustest.Start(t, nucleus.New().%[12]s
		WithDatabases(%[5]s).
		Mount(%[1]s.Module()))

	if records := listRecords(t, srv); len(records) != 0 {
		t.Errorf("GET /%[2]s on a fresh database: want an empty list, got %%+v", records)
	}

	// A record that does not exist is a 404 in the framework's error shape
	// — the envelope, or RFC 9457 problem details for a client that asks —
	// because the handlers return their errors instead of writing them.
	missing := srv.Get("/%[2]s/999999", nucleustest.WithHeader("Accept", "application/problem+json"))
	if missing.Status != http.StatusNotFound || !strings.HasPrefix(missing.Header.Get("Content-Type"), "application/problem+json") {
		t.Errorf("GET /%[2]s/999999: want 404 as problem details, got %%d %%s: %%s", missing.Status, missing.Header.Get("Content-Type"), missing)
	}

	%[9]s
	resp := srv.Post("/%[2]s", map[string]string{"name": "first"})
	if resp.Status != %[8]s {
		t.Fatalf("POST /%[2]s: want %%d, got %%d: %%s", %[8]s, resp.Status, resp)
	}%[11]s
}

// listRecords reads GET /%[2]s into the slice's Record type.
func listRecords(t *testing.T, srv *nucleustest.Server) []%[1]s.Record {
	t.Helper()
	resp := srv.Get("/%[2]s")
	if resp.Status != http.StatusOK {
		t.Fatalf("GET /%[2]s: want %%d, got %%d: %%s", http.StatusOK, resp.Status, resp)
	}
	// The keys "data" and "count" match these fields case-insensitively.
	var list struct {
		Data  []%[1]s.Record
		Count int
	}
	resp.JSON(t, &list)
	if list.Count != len(list.Data) {
		t.Errorf("GET /%[2]s: count %%d for %%d records", list.Count, len(list.Data))
	}
	return list.Data
}
%[10]s`

// moduleSliceTestReadBack is what the open-policy test adds once its write
// landed: the created record decoded with its key, and found in the list.
const moduleSliceTestReadBack = `
	var created struct{ Data %[1]s.Record }
	resp.JSON(t, &created)
	if created.Data.ID == 0 || created.Data.Name != "first" {
		t.Errorf("POST /%[2]s: want the created record with its key, got %%+v", created.Data)
	}
	if records := listRecords(t, srv); len(records) != 1 || records[0].ID != created.Data.ID {
		t.Errorf("GET /%[2]s after the POST: want the created record listed, got %%+v", records)
	}`

// moduleSliceQuarkStorageSource renders the Quark variant of the slice's
// model + storage file (--data quark): the same Record shape and Storage
// methods the controller calls, implemented with quark.For[T] over a client
// derived from the framework-managed pool (quark.NewWithDB), so the host
// keeps owning the connection lifecycle. The embedded SQL migration still
// creates the table; Quark only reads and writes it.
func moduleSliceQuarkStorageSource(snake, pascal, table, system string, enforced bool) string {
	driverImport := ""
	if driver := quarkDriverModule(system); driver != "" {
		driverImport = "\n\t// Quark's driver module for the engine registers the error classifier\n\t// (duplicate key, deadlock) the storage relies on; without this import\n\t// `go mod tidy` drops the module and those errors stay generic.\n\t_ " + strconv.Quote(driver) + "\n"
	}
	return fmt.Sprintf(moduleSliceQuarkStorageTemplate, snake, pascal, table, system, quarkDriverName(system), driverImport, moduleRowsNote(enforced))
}

const moduleSliceQuarkStorageTemplate = `// Package %[1]s is the %[2]s feature slice, generated by nucleus CLI with
// --data quark. Everything the feature needs lives in this package: model +
// storage (this file, on the Quark ORM), controller.go, and module.go (the
// mountable module with its embedded migrations, page template, policy
// rows%[7]s and CSRF exemption). The embedded migration was rendered for the
// %[4]s dialect at scaffold time and creates the table; Quark reads and
// writes it through the framework-managed pool.
package %[1]s

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jcsvwinston/quark"
%[6]s)

// ErrNotFound reports a %[3]s row that does not exist (or is soft-deleted).
var ErrNotFound = errors.New("%[3]s record not found")

// Record is the Quark model behind the /%[3]s resource. The deleted_at
// column makes quark.Delete a soft delete, and every read excludes
// soft-deleted rows, matching the plain-SQL variant of this slice.
type Record struct {
	ID        uint       ` + "`db:\"id\" pk:\"true\" json:\"id\"`" + `
	Name      string     ` + "`db:\"name\" quark:\"not_null\" json:\"name\"`" + `
	CreatedAt time.Time  ` + "`db:\"created_at\" json:\"created_at\"`" + `
	UpdatedAt time.Time  ` + "`db:\"updated_at\" json:\"updated_at\"`" + `
	DeletedAt *time.Time ` + "`db:\"deleted_at\" json:\"-\"`" + `
}

// TableName pins the model to the table the embedded migration creates.
func (Record) TableName() string { return "%[3]s" }

type ListParams struct {
	Query string
}

type CreateParams struct {
	Name string
}

type UpdateParams struct {
	Name string
}

// Storage runs Quark queries against the framework-managed *sql.DB —
// injected by the module's OnStart hook via rt.DB(). quark.NewWithDB
// borrows the pool: the framework owns its lifecycle, and Storage never
// opens or closes connections itself.
type Storage struct {
	client *quark.Client
}

// NewStorage derives a Quark client over the managed pool. The driver name
// tells Quark which dialect to speak; it matches the dialect the embedded
// migration was rendered for.
func NewStorage(db *sql.DB) (*Storage, error) {
	client, err := quark.NewWithDB(%[5]q, db)
	if err != nil {
		return nil, err
	}
	return &Storage{client: client}, nil
}

// listPageSize caps what List returns. Quark warns when List() runs without
// an explicit Limit and falls back to 100 — so the cap was already there,
// just not where anyone could see or change it. Raise it here, or take a
// page parameter, when the feature needs more.
const listPageSize = 100

func (s *Storage) List(ctx context.Context, params ListParams) ([]Record, error) {
	q := quark.For[Record](ctx, s.client).OrderBy("id", "ASC").Limit(listPageSize)
	// The filter is a WHERE, not a loop over the page: filtering after the
	// fact only ever searched the rows that fit in the cap, so a match on
	// row 101 did not exist as far as the caller was concerned.
	if query := strings.TrimSpace(params.Query); query != "" {
		q = q.Where("name", "LIKE", "%%"+query+"%%")
	}
	records, err := q.List()
	if err != nil {
		return nil, err
	}
	return records, nil
}

func (s *Storage) Get(ctx context.Context, id uint) (Record, error) {
	record, err := quark.For[Record](ctx, s.client).Find(id)
	if errors.Is(err, quark.ErrNotFound) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Storage) Create(ctx context.Context, params CreateParams) (Record, error) {
	now := time.Now().UTC()
	record := Record{Name: params.Name, CreatedAt: now, UpdatedAt: now}
	if err := quark.For[Record](ctx, s.client).Create(&record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Storage) Update(ctx context.Context, id uint, params UpdateParams) (Record, error) {
	record, err := s.Get(ctx, id)
	if err != nil {
		return Record{}, err
	}
	record.Name = params.Name
	record.UpdatedAt = time.Now().UTC()
	affected, err := quark.For[Record](ctx, s.client).UpdateFields(&record, "name", "updated_at")
	if err != nil {
		return Record{}, err
	}
	if affected == 0 {
		return Record{}, ErrNotFound
	}
	return record, nil
}

func (s *Storage) Delete(ctx context.Context, id uint) error {
	record, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	affected, err := quark.For[Record](ctx, s.client).Delete(&record)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
`
