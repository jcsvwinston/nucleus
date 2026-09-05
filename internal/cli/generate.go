package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
)

func runGenerate(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(stderr)

	force := fs.Bool("force", false, "Overwrite existing files")
	outDir := fs.String("out", ".", "Project root output directory")
	migrationsDir := fs.String("migrations", "", "Migrations directory (defaults to <out>/migrations)")
	dialect := fs.String("dialect", "", "Migration SQL dialect (sqlite|postgresql|mysql|mssql|oracle); defaults to the configured database")
	configPath := fs.String("config", "", "Path to nucleus config file (defaults to <out>/nucleus.yml)")
	databaseAlias := fs.String("database", "", "Database alias whose dialect the migration targets (defaults to database_default)")
	withPolicy := fs.Bool("with-policy", false, "resource: seed anonymous RBAC rows and a CSRF exemption for the generated routes; module: open every verb to anonymous callers instead of read-only (development defaults)")
	mount := fs.Bool("mount", false, "module: add the import and the Mount(<name>.Module()) call to the nucleus.New() chain in main.go")
	dataLayer := fs.String("data", moduleDataSQL, "module: storage implementation — sql (database/sql statements for the configured dialect) or quark (the Quark ORM over the managed pool; the project then needs `go get github.com/jcsvwinston/quark`)")

	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: nucleus generate <kind> <name> [flags]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Scaffolds application code. <kind> is one of:")
		fmt.Fprintln(stderr, "  model       a model.BaseModel struct")
		fmt.Fprintln(stderr, "  handler     an HTTP handler with a Mount method")
		fmt.Fprintln(stderr, "  service     a service layer type")
		fmt.Fprintln(stderr, "  repository  a repository layer type")
		fmt.Fprintln(stderr, "  migration   an up/down migration pair")
		fmt.Fprintln(stderr, "  resource    model + handler + service + repository + contract + migration")
		fmt.Fprintln(stderr, "  module      a self-contained feature slice under internal/<name>/ — model+storage,")
		fmt.Fprintln(stderr, "              controller, mountable module with its own policy rows and CSRF exemption,")
		fmt.Fprintln(stderr, "              embedded migrations, page template and a test; Mount() is the whole")
		fmt.Fprintln(stderr, "              integration, and --mount writes that line into main.go for you")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Examples:")
		fmt.Fprintln(stderr, "  nucleus generate module notes --mount")
		fmt.Fprintln(stderr, "  nucleus generate module notes --mount --data quark")
		fmt.Fprintln(stderr, "  nucleus generate resource Widget --with-policy")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Flags:")
		fs.PrintDefaults()
	}

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
		return fmt.Errorf("usage: nucleus generate <model|handler|service|repository|migration|resource|module> <name>")
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
	if *mount && kind != "module" {
		return fmt.Errorf("--mount applies to `generate module` only (kind %q); the printed Mount line is the manual step for the other kinds", kind)
	}
	if *dataLayer != moduleDataSQL && kind != "module" {
		return fmt.Errorf("--data applies to `generate module` only (kind %q)", kind)
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
		system, err := resolveScaffoldSystem(*dialect, *configPath, *databaseAlias, *outDir)
		if err != nil {
			return err
		}
		// The contracts aggregator (internal/contracts/contracts.go) is the
		// OpenAPI registrar the resource's contract file registers into. It
		// belongs to this kind alone: a module slice or a lone model never
		// touches it, and a project that never asked for OpenAPI should not
		// find the package in its tree.
		if err := ensureContractsAggregator(*outDir, defaultOpenAPITitle("", modulePath, *outDir)); err != nil {
			return err
		}
		result, err := generateResourceScaffold(*outDir, dir, snake, pascal, system, *force)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Resource scaffold created: %s\n", pascal)
		fmt.Fprintf(stdout, "  model: %s\n", result.ModelPath)
		fmt.Fprintf(stdout, "  controller: %s\n", result.HandlerPath)
		fmt.Fprintf(stdout, "  service: %s\n", result.ServicePath)
		fmt.Fprintf(stdout, "  repository: %s\n", result.RepositoryPath)
		fmt.Fprintf(stdout, "  contract: %s\n", result.ContractPath)
		fmt.Fprintf(stdout, "  test: %s\n", result.TestPath)
		if result.ModulePath != "" {
			fmt.Fprintf(stdout, "  module: %s\n", result.ModulePath)
		}
		fmt.Fprintf(stdout, "  migration up: %s\n", result.MigrationUpPath)
		fmt.Fprintf(stdout, "  migration down: %s\n", result.MigrationDownPath)
		fmt.Fprintf(stdout, "  migration dialect: %s\n", system)
		resourcePath := "/" + pluralizeResource(snake)
		if result.ModulePath != "" {
			fmt.Fprintf(stdout, "Mount it in main.go:  nucleus.New().Mount(modules.%sModule())\n", pascal)
			printMountedRouteTable(stdout, "", resourcePath)
			if *withPolicy {
				if err := seedResourcePolicy(*outDir, *configPath, resourcePath, stdout); err != nil {
					return err
				}
				fmt.Fprintln(stdout, "Then apply the migration (nucleus migrate up). The seeded rows grant anonymous CRUD — scope them down before production.")
			} else {
				fmt.Fprintln(stdout, "Then apply the migration (nucleus migrate up). With the default-deny authorizer, the routes above need rbac_policy.csv rows and a CSRF exemption for cookie-less JSON writes — re-run with --with-policy to seed both, or add them yourself.")
			}
		} else if *withPolicy {
			fmt.Fprintln(stdout, "--with-policy skipped: no go.mod module detected, so no mountable module (or routes) was generated.")
		}
		return nil

	case "module":
		data, err := moduleDataLayer(*dataLayer)
		if err != nil {
			return err
		}
		system, err := resolveScaffoldSystem(*dialect, *configPath, *databaseAlias, *outDir)
		if err != nil {
			return err
		}
		result, err := generateModuleScaffold(*outDir, snake, pascal, system, *force, *withPolicy, data)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Module slice created: %s (package %s)\n", pascal, snake)
		if *withPolicy {
			fmt.Fprintf(stdout, "  policy: OPEN — every verb is allowed to anonymous callers (--with-policy); scope it before the slice faces a network\n")
		} else {
			fmt.Fprintf(stdout, "  policy: anonymous read only — writes need an authenticated subject (add rows in module.go, or --with-policy for a development spike)\n")
		}
		if data == moduleDataQuark {
			fmt.Fprintf(stdout, "  model+storage (quark): %s\n", result.StoragePath)
		} else {
			fmt.Fprintf(stdout, "  model+storage: %s\n", result.StoragePath)
		}
		fmt.Fprintf(stdout, "  controller: %s\n", result.ControllerPath)
		fmt.Fprintf(stdout, "  module: %s\n", result.ModulePath)
		fmt.Fprintf(stdout, "  test: %s\n", result.TestPath)
		fmt.Fprintf(stdout, "  migration up (embedded): %s\n", result.MigrationUpPath)
		fmt.Fprintf(stdout, "  migration down (embedded): %s\n", result.MigrationDownPath)
		fmt.Fprintf(stdout, "  template (embedded): %s\n", result.TemplatePath)
		fmt.Fprintf(stdout, "  migration dialect: %s\n", system)

		mountExpr := snake + ".Module()"
		importPath := modulePath + "/internal/" + snake
		if *mount {
			mainPath, err := pickImportFile(*outDir)
			if err != nil {
				return fmt.Errorf("--mount: %w", err)
			}
			added, err := ensureMountCall(mainPath, importPath, mountExpr)
			if errors.Is(err, errNoBuilderChain) {
				// The slice is written; only the wiring is left to the
				// person, because their main.go is not the scaffold's and
				// the editor does not guess where a Mount call belongs.
				fmt.Fprintf(stdout, "Mount it in %s yourself — no nucleus.New() builder chain to edit:\n", rel(*outDir, mainPath))
				fmt.Fprintf(stdout, "  import %q\n", importPath)
				fmt.Fprintf(stdout, "  nucleus.New().Mount(%s)\n", mountExpr)
				return fmt.Errorf("--mount: %w", err)
			}
			if err != nil {
				return fmt.Errorf("--mount: %w", err)
			}
			if added {
				fmt.Fprintf(stdout, "Mounted in %s:  Mount(%s)\n", rel(*outDir, mainPath), mountExpr)
			} else {
				fmt.Fprintf(stdout, "Already mounted in %s:  Mount(%s)\n", rel(*outDir, mainPath), mountExpr)
			}
		} else {
			fmt.Fprintf(stdout, "Mount it in main.go:  nucleus.New().Mount(%s)   (or re-run with --mount)\n", mountExpr)
		}
		table := pluralizeResource(snake)
		printMountedRouteTable(stdout, modulePageRoute(snake, table), "/"+table)
		if data == moduleDataQuark {
			fmt.Fprintf(stdout, "The storage runs on the Quark ORM, which the project does not require yet:\n")
			fmt.Fprintf(stdout, "  go get %s", quarkModulePath)
			if driver := quarkDriverModule(system); driver != "" {
				fmt.Fprintf(stdout, " %s", driver)
			}
			fmt.Fprintln(stdout)
			fmt.Fprintln(stdout, "Then nothing else: the module carries its own policy rows, CSRF exemption and migrations (applied on start).")
		} else {
			fmt.Fprintln(stdout, "Nothing else: the module carries its own policy rows, CSRF exemption and migrations (applied on start).")
		}
		fmt.Fprintf(stdout, "Run its test:  go test ./internal/%s/\n", snake)
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
	ModulePath        string
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

// resolveScaffoldSystem decides which SQL dialect a generated migration
// targets (QCD-CLI-4). Precedence: an explicit --dialect wins; otherwise the
// project's configured default database (config file + NUCLEUS_* env, the
// exact config `nucleus migrate` will read before applying the migration).
// A fresh project with no config resolves to the sqlite default.
func resolveScaffoldSystem(dialect, configPath, databaseAlias, outDir string) (string, error) {
	if d := strings.ToLower(strings.TrimSpace(dialect)); d != "" {
		switch d {
		case "sqlite", "postgresql", "mysql", "mssql", "oracle":
			return d, nil
		case "postgres":
			return "postgresql", nil
		case "sqlserver":
			return "mssql", nil
		default:
			return "", fmt.Errorf("unknown dialect %q: expected sqlite|postgresql|mysql|mssql|oracle", dialect)
		}
	}

	// Default the config lookup to the project root being generated into —
	// generate runs with --out pointing at the project, not necessarily from
	// inside it.
	if configPath == "" {
		candidate := filepath.Join(outDir, "nucleus.yml")
		if _, err := os.Stat(candidate); err == nil {
			configPath = candidate
		}
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		return "", err
	}
	_, dbCfg, err := resolveDatabaseAlias(cfg, databaseAlias)
	if err != nil {
		return "", err
	}
	system := db.SystemFromURL(dbCfg.URL)
	if system == "unknown" {
		return "", fmt.Errorf("cannot resolve the migration dialect from database URL %q: pass --dialect explicitly", dbCfg.URL)
	}
	return system, nil
}

func generateResourceScaffold(outDir, migrationsDir, snake, pascal, system string, force bool) (*resourceScaffoldResult, error) {
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

	var moduleFilePath string
	if hasModule {
		repositoryPath, err = generateResourceRepositoryScaffold(outDir, snake, pascal, system, force)
		if err != nil {
			return nil, err
		}

		servicePath, err = generateResourceServiceScaffold(outDir, snake, pascal, modulePath, force)
		if err != nil {
			return nil, err
		}

		moduleFilePath, err = generateResourceModuleScaffold(outDir, snake, pascal, resourcePath, modulePath, force)
		if err != nil {
			return nil, err
		}

		handlerBody = fmt.Sprintf(resourceControllerTemplate, modulePath, pascal, resourcePath)
		testBody = fmt.Sprintf(resourceControllerTestTemplate, modulePath, pascal, resourcePath)
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
	upSQL, downSQL, err := model.BuildMigrationScaffoldForSystem(system, resourceScaffoldMeta(table, pascal))
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
		ModulePath:        moduleFilePath,
		MigrationUpPath:   upPath,
		MigrationDownPath: downPath,
	}, nil
}

func generateResourceRepositoryScaffold(outDir, snake, pascal, system string, force bool) (string, error) {
	path := filepath.Join(outDir, "internal", "repositories", snake+"_repository.go")
	body := resourceRepositorySource(pascal, pluralizeResource(snake), system)
	if err := writeFileIfNotExists(path, body, force); err != nil {
		return "", err
	}
	return path, nil
}

func generateResourceModuleScaffold(outDir, snake, pascal, resourcePath, modulePath string, force bool) (string, error) {
	path := filepath.Join(outDir, "internal", "modules", snake+"_module.go")
	body := fmt.Sprintf(resourceModuleTemplate, modulePath, pascal, resourcePath)
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
	base := fmt.Sprintf("%s_%s", migrationTimestamp(), toSnakeCase(name))
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

	Name string ` + "`db:\"column:name;required;index\" validate:\"required\"`" + `
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

// resourcePlaceholder renders the n-th positional bind parameter for the
// given migration system. The repository scaffold embeds the resulting SQL
// verbatim, so the placeholder style must match the driver the app runs on.
func resourcePlaceholder(system string, n int) string {
	switch system {
	case "postgresql":
		return fmt.Sprintf("$%d", n)
	case "mssql":
		return fmt.Sprintf("@p%d", n)
	case "oracle":
		return fmt.Sprintf(":%d", n)
	default: // sqlite, mysql
		return "?"
	}
}

// resourceRepositorySource renders the database-backed repository scaffold
// (DX-21). The SQL targets the table created by the migration generated in
// the same run, with placeholders and insert-id retrieval rendered for the
// same dialect the migration was rendered for — the two artifacts cannot
// disagree about the engine.
func resourceRepositorySource(pascal, table, system string) string {
	p := func(n int) string { return resourcePlaceholder(system, n) }

	listSQL := fmt.Sprintf("SELECT id, name, created_at, updated_at FROM %s WHERE deleted_at IS NULL ORDER BY id", table)
	getSQL := fmt.Sprintf("SELECT id, name, created_at, updated_at FROM %s WHERE id = %s AND deleted_at IS NULL", table, p(1))
	updateSQL := fmt.Sprintf("UPDATE %s SET name = %s, updated_at = %s WHERE id = %s AND deleted_at IS NULL", table, p(1), p(2), p(3))
	deleteSQL := fmt.Sprintf("UPDATE %s SET deleted_at = %s WHERE id = %s AND deleted_at IS NULL", table, p(1), p(2))

	var createBody string
	switch system {
	case "postgresql":
		insertSQL := fmt.Sprintf("INSERT INTO %s (name, created_at, updated_at) VALUES ($1, $2, $3) RETURNING id", table)
		createBody = fmt.Sprintf("\tif err := r.db.QueryRowContext(ctx, %q, params.Name, now, now).Scan(&id); err != nil {\n\t\treturn %sRecord{}, err\n\t}", insertSQL, pascal)
	case "mssql":
		insertSQL := fmt.Sprintf("INSERT INTO %s (name, created_at, updated_at) OUTPUT INSERTED.id VALUES (@p1, @p2, @p3)", table)
		createBody = fmt.Sprintf("\tif err := r.db.QueryRowContext(ctx, %q, params.Name, now, now).Scan(&id); err != nil {\n\t\treturn %sRecord{}, err\n\t}", insertSQL, pascal)
	case "oracle":
		insertSQL := fmt.Sprintf("INSERT INTO %s (name, created_at, updated_at) VALUES (:1, :2, :3) RETURNING id INTO :4", table)
		createBody = fmt.Sprintf("\t// Oracle returns the generated identity through an OUT bind parameter.\n\tvar generated int64\n\tif _, err := r.db.ExecContext(ctx, %q, params.Name, now, now, sql.Out{Dest: &generated}); err != nil {\n\t\treturn %sRecord{}, err\n\t}\n\tid = uint(generated)", insertSQL, pascal)
	default: // sqlite, mysql
		insertSQL := fmt.Sprintf("INSERT INTO %s (name, created_at, updated_at) VALUES (?, ?, ?)", table)
		createBody = fmt.Sprintf("\tres, err := r.db.ExecContext(ctx, %q, params.Name, now, now)\n\tif err != nil {\n\t\treturn %[2]sRecord{}, err\n\t}\n\tlastID, err := res.LastInsertId()\n\tif err != nil {\n\t\treturn %[2]sRecord{}, err\n\t}\n\tid = uint(lastID)", insertSQL, pascal)
	}

	return fmt.Sprintf(resourceRepositoryTemplate, pascal, table, listSQL, getSQL, updateSQL, deleteSQL, createBody, system)
}

const resourceRepositoryTemplate = `package repositories

// %[1]sRepository is a scaffold generated by nucleus CLI. It runs real SQL
// against the framework-managed *sql.DB — injected by the generated module's
// OnStart hook via rt.DB() — and targets the %[2]s table created by the
// migration generated alongside it. Statements were rendered for the %[8]s
// dialect at scaffold time; regenerate with --dialect (or edit the SQL) if
// the application changes engines.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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
	db *sql.DB
}

// New%[1]sRepository wraps the framework-managed database handle. The
// framework owns the pool's lifecycle — the repository never opens or
// closes connections itself.
func New%[1]sRepository(db *sql.DB) *%[1]sRepository {
	return &%[1]sRepository{db: db}
}

func (r *%[1]sRepository) List(ctx context.Context, params List%[1]sParams) ([]%[1]sRecord, error) {
	rows, err := r.db.QueryContext(ctx, %[3]q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	query := strings.ToLower(strings.TrimSpace(params.Query))
	records := make([]%[1]sRecord, 0, 16)
	for rows.Next() {
		var record %[1]sRecord
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

func (r *%[1]sRepository) Get(ctx context.Context, id uint) (%[1]sRecord, error) {
	var record %[1]sRecord
	err := r.db.QueryRowContext(ctx, %[4]q, id).
		Scan(&record.ID, &record.Name, &record.CreatedAt, &record.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return %[1]sRecord{}, Err%[1]sNotFound
	}
	if err != nil {
		return %[1]sRecord{}, err
	}
	return record, nil
}

func (r *%[1]sRepository) Create(ctx context.Context, params Create%[1]sParams) (%[1]sRecord, error) {
	now := time.Now().UTC()
	var id uint
%[7]s
	return %[1]sRecord{ID: id, Name: params.Name, CreatedAt: now, UpdatedAt: now}, nil
}

func (r *%[1]sRepository) Update(ctx context.Context, id uint, params Update%[1]sParams) (%[1]sRecord, error) {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, %[5]q, params.Name, now, id)
	if err != nil {
		return %[1]sRecord{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return %[1]sRecord{}, err
	}
	if affected == 0 {
		return %[1]sRecord{}, Err%[1]sNotFound
	}
	return r.Get(ctx, id)
}

func (r *%[1]sRepository) Delete(ctx context.Context, id uint) error {
	res, err := r.db.ExecContext(ctx, %[6]q, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return Err%[1]sNotFound
	}
	return nil
}
`

// resourceModuleTemplate is the mountable module the resource scaffold emits
// (DX-21): nucleus.New().Mount(modules.<Name>Module()) is the whole wiring —
// no hand-written adapter between the generated code and the framework.
const resourceModuleTemplate = `package modules

import (
	"context"
	"fmt"

	"%[1]s/internal/controllers"
	"%[1]s/internal/repositories"
	"%[1]s/internal/services"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// %[2]sModule returns the mountable module for the generated %[3]s resource.
// Register it in main.go:
//
//	nucleus.New().Mount(modules.%[2]sModule())
//
// Lifecycle: OnStart captures the framework-managed *sql.DB (rt.DB()) and
// builds the repository/service pair; Routes runs after OnStart (ADR-010
// Phase 4) and registers the REST resource. The framework owns the database
// pool — the module never opens or closes it.
func %[2]sModule() nucleus.ModuleSpec {
	var service *services.%[2]sService

	return nucleus.Module[struct{}]{
		Name: "%[3]s",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			db := rt.DB()
			if db == nil {
				return fmt.Errorf("%[3]s: no managed database configured (set databases.default.url in nucleus.yml)")
			}
			service = services.New%[2]sService(repositories.New%[2]sRepository(db))
			return nil
		},
		Routes: func(r nucleus.Router, _ struct{}) {
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

// resourceControllerTemplate implements the framework's REST Resource
// sub-interfaces directly on nucleus.Context (DX-21): the generated module
// registers it with r.Resource — nothing here needs *router.Mux or an
// adapter.
const resourceControllerTemplate = `package controllers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"%[1]s/internal/repositories"
	"%[1]s/internal/services"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

type %[2]sPayload struct {
	Name string ` + "`json:\"name\"`" + `
}

// %[2]sController implements the nucleus REST Resource sub-interfaces
// (Indexer, Shower, Creator, Updater, Destroyer). The generated module in
// internal/modules registers it via r.Resource — no adapter needed.
type %[2]sController struct {
	service *services.%[2]sService
}

func New%[2]sController(service *services.%[2]sService) *%[2]sController {
	return &%[2]sController{service: service}
}

// Index handles GET /%[3]s.
func (ctl *%[2]sController) Index(c *nucleus.Context) error {
	records, err := ctl.service.List(c.Request.Context(), services.List%[2]sInput{
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
func (ctl *%[2]sController) Show(c *nucleus.Context) error {
	id, err := parse%[2]sID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	record, err := ctl.service.Get(c.Request.Context(), id)
	if errors.Is(err, repositories.Err%[2]sNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "%[2]s " + strconv.FormatUint(uint64(id), 10) + " not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "unable to fetch %[2]s"})
	}

	return c.JSON(http.StatusOK, map[string]any{"data": record})
}

// Create handles POST /%[3]s.
func (ctl *%[2]sController) Create(c *nucleus.Context) error {
	payload, err := bind%[2]sPayload(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	record, err := ctl.service.Create(c.Request.Context(), services.Create%[2]sInput{Name: payload.Name})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "unable to create %[2]s"})
	}

	return c.JSON(http.StatusCreated, map[string]any{"data": record})
}

// Update handles PUT /%[3]s/{id}.
func (ctl *%[2]sController) Update(c *nucleus.Context) error {
	id, err := parse%[2]sID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	payload, err := bind%[2]sPayload(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	record, err := ctl.service.Update(c.Request.Context(), id, services.Update%[2]sInput{Name: payload.Name})
	if errors.Is(err, repositories.Err%[2]sNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "%[2]s " + strconv.FormatUint(uint64(id), 10) + " not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "unable to update %[2]s"})
	}

	return c.JSON(http.StatusOK, map[string]any{"data": record})
}

// Destroy handles DELETE /%[3]s/{id}.
func (ctl *%[2]sController) Destroy(c *nucleus.Context) error {
	id, err := parse%[2]sID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	err = ctl.service.Delete(c.Request.Context(), id)
	if errors.Is(err, repositories.Err%[2]sNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "%[2]s " + strconv.FormatUint(uint64(id), 10) + " not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "unable to delete %[2]s"})
	}

	return c.NoContent()
}

func bind%[2]sPayload(c *nucleus.Context) (%[2]sPayload, error) {
	var payload %[2]sPayload
	if err := c.BindJSON(&payload); err != nil {
		return payload, errors.New("request body must be valid JSON")
	}

	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" {
		return payload, errors.New("name is required")
	}

	return payload, nil
}

func parse%[2]sID(c *nucleus.Context) (uint, error) {
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

const resourceControllerTestTemplate = `package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"%[1]s/internal/repositories"
	"%[1]s/internal/services"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// fake%[2]sRepository implements services.%[2]sRepository in memory so the
// controller and service stay unit-testable without a database. The real
// SQL-backed repository in internal/repositories is exercised end-to-end by
// the running application (boot + migrate + HTTP).
type fake%[2]sRepository struct {
	nextID uint
	items  []repositories.%[2]sRecord
}

func (f *fake%[2]sRepository) List(_ context.Context, params repositories.List%[2]sParams) ([]repositories.%[2]sRecord, error) {
	query := strings.ToLower(strings.TrimSpace(params.Query))
	out := make([]repositories.%[2]sRecord, 0, len(f.items))
	for _, it := range f.items {
		if query != "" && !strings.Contains(strings.ToLower(it.Name), query) {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

func (f *fake%[2]sRepository) Get(_ context.Context, id uint) (repositories.%[2]sRecord, error) {
	for _, it := range f.items {
		if it.ID == id {
			return it, nil
		}
	}
	return repositories.%[2]sRecord{}, repositories.Err%[2]sNotFound
}

func (f *fake%[2]sRepository) Create(_ context.Context, params repositories.Create%[2]sParams) (repositories.%[2]sRecord, error) {
	f.nextID++
	record := repositories.%[2]sRecord{ID: f.nextID, Name: params.Name}
	f.items = append(f.items, record)
	return record, nil
}

func (f *fake%[2]sRepository) Update(_ context.Context, id uint, params repositories.Update%[2]sParams) (repositories.%[2]sRecord, error) {
	for i, it := range f.items {
		if it.ID == id {
			f.items[i].Name = params.Name
			return f.items[i], nil
		}
	}
	return repositories.%[2]sRecord{}, repositories.Err%[2]sNotFound
}

func (f *fake%[2]sRepository) Delete(_ context.Context, id uint) error {
	for i, it := range f.items {
		if it.ID == id {
			f.items = append(f.items[:i], f.items[i+1:]...)
			return nil
		}
	}
	return repositories.Err%[2]sNotFound
}

// call%[2]s invokes one controller method with a real nucleus.Context built
// from httptest primitives — the same shape the framework hands to handlers.
func call%[2]s(t *testing.T, handler func(*nucleus.Context) error, method, target, id string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatalf("encode request body: %%v", err)
		}
	}
	req := httptest.NewRequest(method, target, &body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	c := &nucleus.Context{Context: router.NewContext(rec, req, nil)}
	if err := handler(c); err != nil {
		t.Fatalf("handler returned error: %%v", err)
	}
	return rec
}

func decode%[2]sJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode response: %%v raw=%%s", err, string(raw))
	}
	return payload
}

func Test%[2]sController_CRUDLifecycle(t *testing.T) {
	service := services.New%[2]sService(&fake%[2]sRepository{})
	ctl := New%[2]sController(service)

	createRec := call%[2]s(t, ctl.Create, http.MethodPost, "/%[3]s", "", map[string]any{"name": "Books"})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: want %%d, got %%d body=%%s", http.StatusCreated, createRec.Code, createRec.Body.String())
	}
	created, ok := decode%[2]sJSON(t, createRec.Body.Bytes())["data"].(map[string]any)
	if !ok {
		t.Fatalf("create response has no data object: %%s", createRec.Body.String())
	}
	id, ok := created["id"].(float64)
	if !ok || id <= 0 {
		t.Fatalf("created record has no positive id: %%v", created["id"])
	}
	idStr := strconv.Itoa(int(id))

	call%[2]s(t, ctl.Create, http.MethodPost, "/%[3]s", "", map[string]any{"name": "Games"})

	listRec := call%[2]s(t, ctl.Index, http.MethodGet, "/%[3]s", "", nil)
	if got := int(decode%[2]sJSON(t, listRec.Body.Bytes())["count"].(float64)); got != 2 {
		t.Fatalf("list count: want 2, got %%d", got)
	}

	filteredRec := call%[2]s(t, ctl.Index, http.MethodGet, "/%[3]s?q=book", "", nil)
	if got := int(decode%[2]sJSON(t, filteredRec.Body.Bytes())["count"].(float64)); got != 1 {
		t.Fatalf("filtered count: want 1, got %%d", got)
	}

	showRec := call%[2]s(t, ctl.Show, http.MethodGet, fmt.Sprintf("/%[3]s/%%s", idStr), idStr, nil)
	if showRec.Code != http.StatusOK {
		t.Fatalf("show: want %%d, got %%d", http.StatusOK, showRec.Code)
	}

	updateRec := call%[2]s(t, ctl.Update, http.MethodPut, fmt.Sprintf("/%[3]s/%%s", idStr), idStr, map[string]any{"name": "Novels"})
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update: want %%d, got %%d body=%%s", http.StatusOK, updateRec.Code, updateRec.Body.String())
	}

	deleteRec := call%[2]s(t, ctl.Destroy, http.MethodDelete, fmt.Sprintf("/%[3]s/%%s", idStr), idStr, nil)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("destroy: want %%d, got %%d", http.StatusNoContent, deleteRec.Code)
	}

	missingRec := call%[2]s(t, ctl.Show, http.MethodGet, fmt.Sprintf("/%[3]s/%%s", idStr), idStr, nil)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("show after destroy: want %%d, got %%d", http.StatusNotFound, missingRec.Code)
	}

	badIDRec := call%[2]s(t, ctl.Show, http.MethodGet, "/%[3]s/not-a-number", "not-a-number", nil)
	if badIDRec.Code != http.StatusBadRequest {
		t.Fatalf("show with bad id: want %%d, got %%d", http.StatusBadRequest, badIDRec.Code)
	}
}

func Test%[2]sController_RejectsInvalidPayload(t *testing.T) {
	service := services.New%[2]sService(&fake%[2]sRepository{})
	ctl := New%[2]sController(service)

	rec := call%[2]s(t, ctl.Create, http.MethodPost, "/%[3]s", "", map[string]any{"name": "  "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create with blank name: want %%d, got %%d body=%%s", http.StatusBadRequest, rec.Code, rec.Body.String())
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

	write%[1]sJSON(w, http.StatusOK, map[string]any{
		"data":  records,
		"count": len(records),
	})
}

func (h *%[1]sHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parse%[1]sID(r)
	if err != nil {
		write%[1]sError(w, r, gferrors.BadRequest(err.Error()))
		return
	}

	record, ok := h.lookup(id)
	if !ok {
		write%[1]sError(w, r, gferrors.NotFound("%[1]s", strconv.FormatUint(uint64(id), 10)))
		return
	}

	write%[1]sJSON(w, http.StatusOK, map[string]any{"data": record})
}

func (h *%[1]sHandler) Create(w http.ResponseWriter, r *http.Request) {
	payload, err := decode%[1]sPayload(r)
	if err != nil {
		write%[1]sError(w, r, gferrors.BadRequest(err.Error()))
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

	write%[1]sJSON(w, http.StatusCreated, map[string]any{"data": record})
}

func (h *%[1]sHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parse%[1]sID(r)
	if err != nil {
		write%[1]sError(w, r, gferrors.BadRequest(err.Error()))
		return
	}

	payload, err := decode%[1]sPayload(r)
	if err != nil {
		write%[1]sError(w, r, gferrors.BadRequest(err.Error()))
		return
	}

	h.mu.Lock()
	record, ok := h.items[id]
	if !ok {
		h.mu.Unlock()
		write%[1]sError(w, r, gferrors.NotFound("%[1]s", strconv.FormatUint(uint64(id), 10)))
		return
	}

	record.Name = payload.Name
	record.UpdatedAt = time.Now().UTC()
	h.items[id] = record
	h.mu.Unlock()

	write%[1]sJSON(w, http.StatusOK, map[string]any{"data": record})
}

func (h *%[1]sHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parse%[1]sID(r)
	if err != nil {
		write%[1]sError(w, r, gferrors.BadRequest(err.Error()))
		return
	}

	h.mu.Lock()
	if _, ok := h.items[id]; !ok {
		h.mu.Unlock()
		write%[1]sError(w, r, gferrors.NotFound("%[1]s", strconv.FormatUint(uint64(id), 10)))
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

func parse%[1]sID(r *http.Request) (uint, error) {
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

func write%[1]sError(w http.ResponseWriter, r *http.Request, err error) {
	router.Error(w, r, err)
}

func write%[1]sJSON(w http.ResponseWriter, status int, payload any) {
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
	assert%[1]sErrorResponse(t,badIDRec, http.StatusBadRequest, "BAD_REQUEST")

	missingRec := perform%[1]sRequest(t, r, http.MethodGet, resourcePath, nil)
	assert%[1]sErrorResponse(t,missingRec, http.StatusNotFound, "NOT_FOUND")
}

func Test%[1]sHandler_RejectsInvalidPayload(t *testing.T) {
	h := New%[1]sHandler()
	r := router.NewMux()
	h.Mount(r)

	rec := perform%[1]sRequest(t, r, http.MethodPost, "/%[2]s/", map[string]any{"name": "  "})
	assert%[1]sErrorResponse(t,rec, http.StatusBadRequest, "BAD_REQUEST")
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

func assert%[1]sErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
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
