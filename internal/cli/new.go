package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jcsvwinston/nucleus/internal/cli/scaffold"
)

func runNew(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(stderr)

	outDir := fs.String("out", ".", "Parent directory where the project folder will be created")
	modulePath := fs.String("module", "", "Go module path (default: example.com/<project_name>)")
	port := fs.Int("port", 8080, "HTTP port in nucleus.yml")
	force := fs.Bool("force", false, "Overwrite scaffold files if the project directory exists")
	templateName := fs.String("template", "mvc", "Starter template (mvc: full-stack, api: lightweight core-only)")

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
		return fmt.Errorf("usage: nucleus new <project_name> [--module example.com/name] [--out .] [--port 8080] [--template mvc]")
	}
	if *port <= 0 {
		return fmt.Errorf("port must be greater than 0")
	}
	tmpl := strings.TrimSpace(strings.ToLower(*templateName))
	if tmpl != "mvc" && tmpl != "api" {
		return fmt.Errorf("unsupported template %q (supported: mvc, api)", *templateName)
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

	fmt.Fprintf(stdout, "Project scaffold created: %s (template: %s)\n", projectDir, tmpl)
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "This is an empty skeleton — no feature code yet.\n")
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "Next steps:\n")
	fmt.Fprintf(stdout, "  cd %s\n", projectDir)
	fmt.Fprintf(stdout, "  go mod tidy\n")
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
	fmt.Fprintf(stdout, "Add your first feature as a module, then Mount() it in main.go.\n")
	fmt.Fprintf(stdout, "See the docs Quickstart and the examples/mvc_api reference app.\n")
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
// `toolchain` line ("" = none). The toolchain pin exists since go1.26.5
// (GO-2026-5856, DEP-free security bump): generated projects inherit the
// fixed crypto/tls.
const (
	scaffoldGoVersion = "1.26.4"
	scaffoldToolchain = "go1.26.5"
)

// defaultPinnedFrameworkVersion is the published nucleus tag written into
// generated go.mod files for development CLI builds (Version == "dev"). It is a
// concrete, reproducible tag rather than the floating "latest" pseudo-version:
// a scaffold produced by a dev build resolves to a known release instead of
// "whatever happens to be newest", so generated projects are deterministic and
// offline-friendly. Bump this to the current latest release on every tag.
const defaultPinnedFrameworkVersion = "v1.2.0"

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
