package cli

import (
	"errors"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/internal/cli/scaffold"
	"github.com/jcsvwinston/nucleus/internal/knownproviders"
)

// goModTidy runs `go mod tidy` in root. A variable, like goGet, so the
// command's tests can prove the scaffold sequence without a module proxy.
var goModTidy = func(root string, stdout, stderr io.Writer) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = root
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// scaffoldDatabase is one engine `nucleus new --db` can start a project on:
// the driver module the generated main.go imports and the URL nucleus.yml
// starts with. The names are the ones people type (postgres, not pgx).
//
// QuarkDriver and QuarkDSN are what the Quark ORM opens the SAME engine
// with (`quark.New(driver, dsn)`: a database/sql driver name and the data
// source that driver takes — a file for sqlite, a DSN for mysql, a URL for
// the rest) and QuarkDriverDir the directory of Quark's driver module for
// it. The suite template and `--with quark` read them; nothing else does.
type scaffoldDatabase struct {
	Name           string
	Driver         knownproviders.Provider
	URL            string
	QuarkDriver    string
	QuarkDSN       string
	QuarkDriverDir string
}

// scaffoldDatabases lists the engines by the name --db accepts. Every
// entry maps to a driver module the framework publishes (ADR-031), so
// the scaffold's `go get` is a promise the project keeps.
func scaffoldDatabases() map[string]scaffoldDatabase {
	table := map[string]scaffoldDatabase{}
	for _, e := range []struct{ name, driver, url, quarkDriver, quarkDSN, quarkDir string }{
		{"sqlite", "sqlite", "sqlite://app.db", "sqlite", "app.db", "sqlite"},
		{"postgres", "pgx", "postgres://postgres:postgres@localhost:5432/app?sslmode=disable", "pgx", "postgres://postgres:postgres@localhost:5432/app?sslmode=disable", "postgres"},
		{"mysql", "mysql", "mysql://root:root@localhost:3306/app", "mysql", "root:root@tcp(localhost:3306)/app?parseTime=true", "mysql"},
		{"sqlserver", "sqlserver", "sqlserver://sa:YourStrong!Passw0rd@localhost:1433?database=app", "sqlserver", "sqlserver://sa:YourStrong!Passw0rd@localhost:1433?database=app", "mssql"},
		{"oracle", "oracle", "oracle://app:app@localhost:1521/FREEPDB1", "oracle", "oracle://app:app@localhost:1521/FREEPDB1", "oracle"},
	} {
		p, ok := knownproviders.DBDriver(e.driver)
		if !ok {
			panic("scaffoldDatabases: unknown driver " + e.driver)
		}
		table[e.name] = scaffoldDatabase{Name: e.name, Driver: p, URL: e.url, QuarkDriver: e.quarkDriver, QuarkDSN: e.quarkDSN, QuarkDriverDir: e.quarkDir}
	}
	return table
}

// quarkDriverModuleFor is Quark's driver module for the engine — the
// module that teaches the ORM the engine's duplicate-key error.
func quarkDriverModuleFor(db scaffoldDatabase) string {
	quark, ok := knownproviders.SuiteModuleByName("quark")
	if !ok || quark.DriverModule == "" {
		return ""
	}
	return fmt.Sprintf(quark.DriverModule, db.QuarkDriverDir)
}

// resolveWith turns the --with list into catalogue entries, in catalogue
// order and without duplicates. The suite template is the four siblings
// wired together, so it implies all of them; --with on the other
// templates names exactly what to fetch. An unknown name lists the
// catalogue, the way an unknown --db lists the engines.
func resolveWith(raw, tmpl string) ([]knownproviders.SuiteModule, error) {
	wanted := map[string]bool{}
	if tmpl == "suite" {
		for _, m := range knownproviders.SuiteModules() {
			wanted[m.Name] = true
		}
	}
	for _, name := range strings.Split(raw, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if _, ok := knownproviders.SuiteModuleByName(name); !ok {
			return nil, fmt.Errorf("unknown --with %q (suite modules: %s)", name, strings.Join(knownproviders.SuiteModuleNames(), ", "))
		}
		wanted[name] = true
	}
	var out []knownproviders.SuiteModule
	for _, m := range knownproviders.SuiteModules() {
		if wanted[m.Name] {
			out = append(out, m)
		}
	}
	return out, nil
}

// scaffoldGoGets is the `go get` list a scaffold runs, in order: the
// framework's driver for --db, then each suite module — Quark followed by
// its own driver module for the engine, so the classifier the shop module
// relies on is linked. It is one list so the post-scaffold text, the
// --offline hand-back and the network step cannot disagree.
func scaffoldGoGets(db scaffoldDatabase, with []knownproviders.SuiteModule) []string {
	gets := []string{db.Driver.Module}
	for _, m := range with {
		gets = append(gets, m.Module)
		if m.DriverModule != "" {
			gets = append(gets, fmt.Sprintf(m.DriverModule, db.QuarkDriverDir))
		}
	}
	return gets
}

// splitWiredGoGets separates the `go get` targets the rendered code
// imports (wired) from the ones nothing in the project imports yet
// (unwired: quark, its driver and the two bridges on the mvc and api
// templates, which fetch them without wiring them). The distinction
// decides the order of the network step: `go mod tidy` drops a require
// nothing imports, so a wired module is fetched BEFORE the tidy, which
// then writes its go.sum, and an unwired one AFTER it, where `go get`
// records it as an indirect require that survives until a module imports
// it (`nucleus generate module <name> --data quark` adds the import
// before its own tidy). The answer is read from the rendered imports, not
// from a table per template, so a template that starts importing a
// sibling moves it to the wired list on its own.
func splitWiredGoGets(gets []string, files []scaffold.File) (wired, unwired []string) {
	imports := renderedImports(files)
	for _, target := range gets {
		if imports[target] || importsUnder(imports, target) {
			wired = append(wired, target)
		} else {
			unwired = append(unwired, target)
		}
	}
	return wired, unwired
}

// renderedImports collects the import paths of every rendered Go file. The
// templates render gofmt-clean Go, so a parse failure is a template bug
// the caller's write step reports through the compiler; here it only
// means "imports nothing", the conservative answer (the module is fetched
// after the tidy either way).
func renderedImports(files []scaffold.File) map[string]bool {
	imports := map[string]bool{}
	fset := token.NewFileSet()
	for _, f := range files {
		if !strings.HasSuffix(f.RelPath, ".go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, f.RelPath, f.Body, parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, imp := range parsed.Imports {
			imports[strings.Trim(imp.Path.Value, `"`)] = true
		}
	}
	return imports
}

// importsUnder reports whether any import path is a package inside the
// module (target/...): the go get target is the module path, the import
// may be one of its packages.
func importsUnder(imports map[string]bool, target string) bool {
	for path := range imports {
		if strings.HasPrefix(path, target+"/") {
			return true
		}
	}
	return false
}

// goGetHandBack is the one-line recipe --offline hands back and a failed
// network step names: the same sequence runNew runs, so the two cannot
// disagree.
func goGetHandBack(wired, unwired []string) string {
	line := "go get " + strings.Join(wired, " ") + " && go mod tidy"
	if len(unwired) > 0 {
		line += " && go get " + strings.Join(unwired, " ")
	}
	return line
}

// resolveScaffoldDatabase accepts the human spellings (postgresql, pg,
// mssql) as `nucleus add` does.
func resolveScaffoldDatabase(raw string) (scaffoldDatabase, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	switch name {
	case "postgresql", "pg":
		name = "postgres"
	case "mssql":
		name = "sqlserver"
	}
	table := scaffoldDatabases()
	if db, ok := table[name]; ok {
		return db, nil
	}
	names := make([]string, 0, len(table))
	for n := range table {
		names = append(names, n)
	}
	sort.Strings(names)
	return scaffoldDatabase{}, fmt.Errorf("unsupported --db %q (supported: %s)", raw, strings.Join(names, ", "))
}

func runNew(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(stderr)
	installUsage(fs, "new")

	outDir := fs.String("out", ".", "Parent directory where the project folder will be created")
	modulePath := fs.String("module", "", "Go module path (default: example.com/<project_name>)")
	port := fs.Int("port", 8080, "HTTP port in nucleus.yml")
	force := fs.Bool("force", false, "Overwrite scaffold files if the project directory exists")
	templateName := fs.String("template", "mvc", "Starter template (mvc: full-stack, api: lightweight core-only, suite: Nucleus + Quark + Orbit wired together)")
	dbName := fs.String("db", "sqlite", "Database engine the project starts on (sqlite, postgres, mysql, sqlserver, oracle): its driver module is required and imported")
	with := fs.String("with", "", "Suite modules to fetch and wire, comma-separated (orbit, quark, quarkbridge, quarkdatasource); --template suite implies all four")
	offline := fs.Bool("offline", false, "Do not touch the network: skip the go get of the driver and suite modules and the go mod tidy (run them yourself before go run .)")

	projectFirst := ""
	parseArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		projectFirst = strings.TrimSpace(args[0])
		parseArgs = args[1:]
	}

	if err := fs.Parse(parseArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	rest := fs.Args()
	if projectFirst != "" {
		rest = append([]string{projectFirst}, rest...)
	}
	if len(rest) != 1 {
		return usageError("new")
	}
	if *port <= 0 {
		return fmt.Errorf("port must be greater than 0")
	}
	tmpl := strings.TrimSpace(strings.ToLower(*templateName))
	if tmpl != "mvc" && tmpl != "api" && tmpl != "suite" {
		return fmt.Errorf("unsupported template %q (supported: mvc, api, suite)", *templateName)
	}
	database, err := resolveScaffoldDatabase(*dbName)
	if err != nil {
		return err
	}
	suite, err := resolveWith(*with, tmpl)
	if err != nil {
		return err
	}
	withNames := make([]string, 0, len(suite))
	for _, m := range suite {
		withNames = append(withNames, m.Name)
	}

	projectName := strings.TrimSpace(rest[0])
	if projectName == "" {
		return fmt.Errorf("project name cannot be empty")
	}

	projectDir := filepath.Join(*outDir, projectName)
	if info, err := os.Stat(projectDir); err == nil && !info.IsDir() {
		return fmt.Errorf("target path exists and is not a directory: %s", projectDir)
	} else if err == nil && !*force {
		return fmt.Errorf("project directory already exists: %s (use --force to overwrite scaffold files)", projectDir)
	}
	if err := ensureDir(projectDir); err != nil {
		return err
	}

	module := strings.TrimSpace(*modulePath)
	if module == "" {
		module = defaultModulePath(projectName)
	}

	// Render the starter project from the embedded template tree (see the
	// scaffold sub-package). The templates are a minimal SKELETON — config, a
	// composition-root main.go, and an empty migrations/ dir; no demo feature
	// code (that lives in examples/mvc_api, not baked into the CLI). This
	// function owns only the surrounding logic (flags, post-scaffold output).
	goVersion, toolchain := resolveGoDirectives()
	files, err := scaffold.Render(tmpl, scaffold.TemplateData{
		Module:            module,
		ProjectName:       projectName,
		Port:              *port,
		FrameworkVersion:  resolveFrameworkVersion(),
		Template:          tmpl,
		GoVersion:         goVersion,
		Toolchain:         toolchain,
		Database:          database.Name,
		DatabaseURL:       database.URL,
		DriverModule:      database.Driver.Module,
		QuarkDriver:       database.QuarkDriver,
		QuarkDSN:          database.QuarkDSN,
		QuarkDriverModule: quarkDriverModuleFor(database),
		With:              withNames,
	})
	if err != nil {
		return err
	}

	for _, f := range files {
		target := filepath.Join(projectDir, filepath.FromSlash(f.RelPath))
		if err := writeFileIfNotExists(target, strings.TrimSpace(f.Body)+"\n", *force); err != nil {
			return err
		}
	}

	// The rendered go.mod requires the framework alone; the driver the
	// generated main.go imports — and every suite module --with names —
	// is a sibling module with its own tag the CLI does not know. `go get`
	// resolves each from the module proxy at its published tag and `go mod
	// tidy` writes go.sum, so the project builds as written — the commands
	// the post-scaffold text used to hand back to the person, run here
	// instead. --offline keeps the scaffold hermetic (tests, air-gapped
	// machines) and hands them back.
	//
	// The suite tags Nucleus before Orbit in every release train, so right
	// after a Nucleus release the orbit tag the proxy serves still pins the
	// previous Nucleus minor; `go get` keeps the higher of the two and the
	// next Orbit tag closes the gap — no version table to bump here.
	//
	// A sibling the rendered code does not import (quark and the bridges
	// on the mvc and api templates) is fetched AFTER the tidy, which would
	// otherwise drop it: `go get` then records it as an indirect require
	// the next tidy keeps once a generated module imports it.
	wired, unwired := splitWiredGoGets(scaffoldGoGets(database, suite), files)
	handBack := goGetHandBack(wired, unwired)
	if !*offline {
		// The scaffold files are already on disk when a command fails, so
		// a plain "re-run with --offline" would stop at "project directory
		// already exists": the advice names --force, and the commands to
		// run inside the directory instead.
		escape := fmt.Sprintf("the scaffold is written in %s; run `%s` there when the network is back, or re-run with --offline --force", projectDir, handBack)
		for _, module := range wired {
			fmt.Fprintf(stdout, "go get %s\n", module)
			if err := goGet(projectDir, module, stdout, stderr); err != nil {
				return fmt.Errorf("go get %s: %w (%s)", module, err, escape)
			}
		}
		fmt.Fprintln(stdout, "go mod tidy")
		if err := goModTidy(projectDir, stdout, stderr); err != nil {
			return fmt.Errorf("go mod tidy: %w (%s)", err, escape)
		}
		for _, module := range unwired {
			fmt.Fprintf(stdout, "go get %s\n", module)
			if err := goGet(projectDir, module, stdout, stderr); err != nil {
				return fmt.Errorf("go get %s: %w (%s)", module, err, escape)
			}
		}
	}

	summary := fmt.Sprintf("template: %s, database: %s", tmpl, database.Name)
	if len(withNames) > 0 {
		summary += ", with: " + strings.Join(withNames, ",")
	}
	fmt.Fprintf(stdout, "Project scaffold created: %s (%s)\n", projectDir, summary)
	fmt.Fprintf(stdout, "\n")
	if tmpl == "suite" {
		printSuiteNextSteps(stdout, projectDir, *port, *offline, handBack)
		return nil
	}
	fmt.Fprintf(stdout, "This is an empty skeleton — no feature code yet.\n")
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "Next steps:\n")
	fmt.Fprintf(stdout, "  cd %s\n", projectDir)
	if *offline {
		fmt.Fprintf(stdout, "  %s   # skipped by --offline\n", handBack)
	}
	if len(unwired) > 0 {
		fmt.Fprintf(stdout, "  nucleus generate module notes --mount --data quark   # your first feature on the Quark ORM, mounted in main.go\n")
	} else {
		fmt.Fprintf(stdout, "  nucleus generate module notes --mount   # your first feature, mounted in main.go\n")
	}
	fmt.Fprintf(stdout, "  go run .\n")
	fmt.Fprintf(stdout, "\n")
	if len(unwired) > 0 {
		fmt.Fprintf(stdout, "Not wired by this template (nothing in the scaffold imports them yet): %s.\n", strings.Join(unwired, ", "))
		fmt.Fprintf(stdout, "  go.mod keeps them as indirect requires until a module imports them; generate module --data quark does.\n")
		fmt.Fprintf(stdout, "\n")
	}
	if tmpl == "api" {
		fmt.Fprintf(stdout, "Running endpoints: http://localhost:%d/healthz\n", *port)
		if hasWith(withNames, "orbit") {
			fmt.Fprintf(stdout, "  Admin panel: http://localhost:%d/admin — user admin, password from ADMIN_BOOTSTRAP_PASSWORD (default \"quickstart\"); it brings its own login gate.\n", *port)
			fmt.Fprintf(stdout, "  This lightweight (api) template runs WithoutDefaults() — no storage,\n")
			fmt.Fprintf(stdout, "  mail, and (WARNING) no authz: your own routes are unauthenticated.\n")
		} else {
			fmt.Fprintf(stdout, "  This lightweight (api) template runs WithoutDefaults() — no admin,\n")
			fmt.Fprintf(stdout, "  storage, mail, and (WARNING) no authz: routes are unauthenticated.\n")
		}
		fmt.Fprintf(stdout, "  Add access control before exposing this service.\n")
	} else if hasWith(withNames, "orbit") {
		fmt.Fprintf(stdout, "Running endpoints: http://localhost:%d/healthz  (plus the built-in framework routes)\n", *port)
		fmt.Fprintf(stdout, "  Admin panel: http://localhost:%d/admin — user admin, password from ADMIN_BOOTSTRAP_PASSWORD (default \"quickstart\").\n", *port)
	} else {
		fmt.Fprintf(stdout, "Running endpoints: http://localhost:%d/healthz  (plus the built-in framework routes)\n", *port)
		fmt.Fprintf(stdout, "  For an admin UI, add github.com/jcsvwinston/orbit and Mount(orbit.Module(...)), or scaffold with --with orbit.\n")
	}
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "A generated module carries its routes, storage, policy rows, migrations and a test;\n")
	fmt.Fprintf(stdout, "--mount writes the Mount() line into main.go. See the docs Quickstart and examples/mvc_api.\n")
	return nil
}

// printSuiteNextSteps is the post-scaffold text of the suite template: the
// project is not an empty skeleton — it carries the shop module, the admin
// panel and both bridges — so the next steps are the commands that show
// them working, in the order the docs walk them.
func printSuiteNextSteps(stdout io.Writer, projectDir string, port int, offline bool, handBack string) {
	fmt.Fprintf(stdout, "Nucleus + Quark + Orbit, wired: the shop module (Quark models, JSON API), the\n")
	fmt.Fprintf(stdout, "admin panel under /admin, Data Studio on the Quark models and the live SQL feed.\n")
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "Next steps:\n")
	fmt.Fprintf(stdout, "  cd %s\n", projectDir)
	if offline {
		fmt.Fprintf(stdout, "  %s   # skipped by --offline\n", handBack)
	}
	fmt.Fprintf(stdout, "  go run .\n")
	fmt.Fprintf(stdout, "  curl -s localhost:%d/api/articles\n", port)
	fmt.Fprintf(stdout, "  curl -s -X POST localhost:%d/api/articles -H 'Content-Type: application/json' -d '{\"author_id\":1,\"title\":\"probe\",\"body\":\"live feed\"}'\n", port)
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "Admin panel: http://localhost:%d/admin — user admin, password from ADMIN_BOOTSTRAP_PASSWORD (default \"quickstart\").\n", port)
	fmt.Fprintf(stdout, "  Watch the live SQL view while hitting the API; browse Author and Article in Data Studio.\n")
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "The shop module carries its own policy rows (anonymous read and create on /api/articles is a\n")
	fmt.Fprintf(stdout, "development default) and CSRF exemption; `go test ./...` boots it in-process. Your next feature:\n")
	fmt.Fprintf(stdout, "  nucleus generate module notes --mount --data quark\n")
}

func hasWith(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func defaultModulePath(projectName string) string {
	slug := toSnakeCase(projectName)
	if slug == "" {
		slug = "nucleus_app"
	}
	return "example.com/" + slug
}

// Framework go.mod directives written into generated projects. They MUST
// mirror the framework's own go.mod so a scaffolded project builds against the
// nucleus release it pins. The CLI binary cannot read the framework go.mod at
// scaffold time (on an end-user machine it lives in the module cache under an
// unpredictable path, not alongside the binary), so the values are pinned here
// as the single source of truth and interpolated into go.mod.tmpl.
//
// These are NOT free to drift: TestScaffoldGoDirectivesTrackGoMod reads the
// framework go.mod at test time and fails CI if either value diverges from the
// real `go` / `toolchain` directives (audit CLI-V2-1). When go.mod's `go`
// directive moves, bump scaffoldGoVersion; scaffoldToolchain mirrors go.mod's
// `toolchain` line ("" = none). Since go1.26.6 the `go` directive itself
// carries the stdlib security floor (GO-2026-6218/6091/6090/6089), so no
// separate toolchain pin is needed — generated projects inherit the fixed
// stdlib through the directive.
const (
	scaffoldGoVersion = "1.26.6"
	scaffoldToolchain = ""
)

// defaultPinnedFrameworkVersion is the published nucleus tag written into
// generated go.mod files for development CLI builds (Version == "dev"). It is a
// concrete, reproducible tag rather than the floating "latest" pseudo-version:
// a scaffold produced by a dev build resolves to a known release instead of
// "whatever happens to be newest", so generated projects are deterministic and
// offline-friendly. release-please rewrites the line below on every release
// (extra-files + the marker); check_version_claims.sh fails CI if it drifts —
// "bump on every tag" as a comment was exactly the manual step that got
// skipped, and v1.3.1 shipped with scaffolds pinning v1.3.0 (NU5-3).
const defaultPinnedFrameworkVersion = "v1.28.1" // x-release-please-version

// resolveGoDirectives returns the `go` and `toolchain` directive values for the
// generated go.mod, tracking the framework go.mod (see scaffoldGoVersion /
// scaffoldToolchain).
func resolveGoDirectives() (goVersion, toolchain string) {
	return scaffoldGoVersion, scaffoldToolchain
}

// resolveFrameworkVersion returns the module version string to use in generated
// go.mod files. When the CLI was built with a release tag (e.g. "0.5.5" via
// goreleaser ldflags), we use "v" + Version. For development builds ("dev"),
// we pin defaultPinnedFrameworkVersion — a concrete published tag — instead of
// the floating "latest", so generated projects are reproducible (a dev-built
// CLI never silently scaffolds against an unreleased or newer-than-expected
// nucleus).
func resolveFrameworkVersion() string {
	v := strings.TrimSpace(Version)
	if v == "" || v == "dev" {
		return defaultPinnedFrameworkVersion
	}
	// goreleaser sets Version without the "v" prefix (e.g. "0.5.5").
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}
