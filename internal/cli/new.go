package cli

import (
	"errors"
	"flag"
	"fmt"
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
type scaffoldDatabase struct {
	Name   string
	Driver knownproviders.Provider
	URL    string
}

// scaffoldDatabases lists the engines by the name --db accepts. Every
// entry maps to a driver module the framework publishes (ADR-031), so
// the scaffold's `go get` is a promise the project keeps.
func scaffoldDatabases() map[string]scaffoldDatabase {
	table := map[string]scaffoldDatabase{}
	for _, e := range []struct{ name, driver, url string }{
		{"sqlite", "sqlite", "sqlite://app.db"},
		{"postgres", "pgx", "postgres://postgres:postgres@localhost:5432/app?sslmode=disable"},
		{"mysql", "mysql", "mysql://root:root@localhost:3306/app"},
		{"sqlserver", "sqlserver", "sqlserver://sa:YourStrong!Passw0rd@localhost:1433?database=app"},
		{"oracle", "oracle", "oracle://app:app@localhost:1521/FREEPDB1"},
	} {
		p, ok := knownproviders.DBDriver(e.driver)
		if !ok {
			panic("scaffoldDatabases: unknown driver " + e.driver)
		}
		table[e.name] = scaffoldDatabase{Name: e.name, Driver: p, URL: e.url}
	}
	return table
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

	outDir := fs.String("out", ".", "Parent directory where the project folder will be created")
	modulePath := fs.String("module", "", "Go module path (default: example.com/<project_name>)")
	port := fs.Int("port", 8080, "HTTP port in nucleus.yml")
	force := fs.Bool("force", false, "Overwrite scaffold files if the project directory exists")
	templateName := fs.String("template", "mvc", "Starter template (mvc: full-stack, api: lightweight core-only)")
	dbName := fs.String("db", "sqlite", "Database engine the project starts on (sqlite, postgres, mysql, sqlserver, oracle): its driver module is required and imported")
	offline := fs.Bool("offline", false, "Do not touch the network: skip the `go get` of the driver module and `go mod tidy` (run them yourself before `go run .`)")

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
		return fmt.Errorf("usage: nucleus new <project_name> [--module example.com/name] [--out .] [--port 8080] [--template mvc] [--db sqlite] [--offline]")
	}
	if *port <= 0 {
		return fmt.Errorf("port must be greater than 0")
	}
	tmpl := strings.TrimSpace(strings.ToLower(*templateName))
	if tmpl != "mvc" && tmpl != "api" {
		return fmt.Errorf("unsupported template %q (supported: mvc, api)", *templateName)
	}
	database, err := resolveScaffoldDatabase(*dbName)
	if err != nil {
		return err
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
		Module:           module,
		ProjectName:      projectName,
		Port:             *port,
		FrameworkVersion: resolveFrameworkVersion(),
		GoVersion:        goVersion,
		Toolchain:        toolchain,
		Database:         database.Name,
		DatabaseURL:      database.URL,
		DriverModule:     database.Driver.Module,
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
	// generated main.go imports is a sibling module with its own tag the
	// CLI does not know. `go get` resolves it and `go mod tidy` writes
	// go.sum, so the project builds as written — the two commands the
	// post-scaffold text used to hand back to the person, run here instead.
	// --offline keeps the scaffold hermetic (tests, air-gapped machines)
	// and hands them back.
	if !*offline {
		fmt.Fprintf(stdout, "go get %s\n", database.Driver.Module)
		if err := goGet(projectDir, database.Driver.Module, stdout, stderr); err != nil {
			return fmt.Errorf("go get %s: %w (re-run with --offline to skip the network and run it yourself)", database.Driver.Module, err)
		}
		fmt.Fprintln(stdout, "go mod tidy")
		if err := goModTidy(projectDir, stdout, stderr); err != nil {
			return fmt.Errorf("go mod tidy: %w (re-run with --offline to skip the network and run it yourself)", err)
		}
	}

	fmt.Fprintf(stdout, "Project scaffold created: %s (template: %s, database: %s)\n", projectDir, tmpl, database.Name)
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "This is an empty skeleton — no feature code yet.\n")
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "Next steps:\n")
	fmt.Fprintf(stdout, "  cd %s\n", projectDir)
	if *offline {
		fmt.Fprintf(stdout, "  go get %s && go mod tidy   # skipped by --offline\n", database.Driver.Module)
	}
	fmt.Fprintf(stdout, "  nucleus generate module notes --mount   # your first feature, mounted in main.go\n")
	fmt.Fprintf(stdout, "  go run .\n")
	fmt.Fprintf(stdout, "\n")
	if tmpl == "api" {
		fmt.Fprintf(stdout, "Running endpoints: http://localhost:%d/healthz\n", *port)
		fmt.Fprintf(stdout, "  This lightweight (api) template runs WithoutDefaults() — no admin,\n")
		fmt.Fprintf(stdout, "  storage, mail, and (WARNING) no authz: routes are unauthenticated.\n")
		fmt.Fprintf(stdout, "  Add access control before exposing this service.\n")
	} else {
		fmt.Fprintf(stdout, "Running endpoints: http://localhost:%d/healthz  (plus the built-in framework routes)\n", *port)
		fmt.Fprintf(stdout, "  For an admin UI, add github.com/jcsvwinston/orbit and Mount(orbit.Module(...)).\n")
	}
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "A generated module carries its routes, storage, policy rows, migrations and a test;\n")
	fmt.Fprintf(stdout, "--mount writes the Mount() line into main.go. See the docs Quickstart and examples/mvc_api.\n")
	return nil
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
const defaultPinnedFrameworkVersion = "v1.24.0" // x-release-please-version

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
