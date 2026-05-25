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
	// scaffold sub-package). The template files are real Go/YAML/SQL files so
	// the toolchain type-checks the demo project's source; this function only
	// owns the surrounding logic (flags, layout, post-scaffold output).
	files, err := scaffold.Render(tmpl, scaffold.TemplateData{
		Module:           module,
		ProjectName:      projectName,
		Port:             *port,
		FrameworkVersion: resolveFrameworkVersion(),
		OpenAPITitle:     defaultOpenAPITitle(projectName, module, projectDir),
	})
	if err != nil {
		return err
	}

	// Keep the generated project aligned with the documented default layout even
	// when some layers do not contain files yet.
	extraDirs := []string{
		filepath.Join(projectDir, "internal", "contracts"),
		filepath.Join(projectDir, "internal", "services"),
		filepath.Join(projectDir, "internal", "repositories"),
		filepath.Join(projectDir, "internal", "web", "static"),
	}
	for _, dirPath := range extraDirs {
		if err := ensureDir(dirPath); err != nil {
			return err
		}
	}

	for _, f := range files {
		target := filepath.Join(projectDir, filepath.FromSlash(f.RelPath))
		if err := writeFileIfNotExists(target, strings.TrimSpace(f.Body)+"\n", *force); err != nil {
			return err
		}
	}

	fmt.Fprintf(stdout, "Project scaffold created: %s (template: %s)\n", projectDir, tmpl)
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "Next steps:\n")
	fmt.Fprintf(stdout, "  cd %s\n", projectDir)
	fmt.Fprintf(stdout, "  go mod tidy\n")
	fmt.Fprintf(stdout, "  go run github.com/jcsvwinston/nucleus/cmd/nucleus@latest migrate --config nucleus.yml --migrations migrations up\n")
	fmt.Fprintf(stdout, "  go run .\n")
	if tmpl == "mvc" {
		fmt.Fprintf(stdout, "\n")
		fmt.Fprintf(stdout, "Optional (requires Redis):\n")
		fmt.Fprintf(stdout, "  go run ./cmd/worker\n")
	}
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "Maintenance (no local Nucleus source needed):\n")
	fmt.Fprintf(stdout, "  go run github.com/jcsvwinston/nucleus/cmd/nucleus@latest migrate --config nucleus.yml --migrations migrations up\n")
	if tmpl == "mvc" {
		fmt.Fprintf(stdout, "  go run github.com/jcsvwinston/nucleus/cmd/nucleus@latest seed --config nucleus.yml --seeds seeds\n")
	}
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "Access:\n")
	if tmpl == "api" {
		fmt.Fprintf(stdout, "  API:     http://localhost:%d/api/articles\n", *port)
		fmt.Fprintf(stdout, "  Health:  http://localhost:%d/health\n", *port)
		fmt.Fprintf(stdout, "  OpenAPI: http://localhost:%d/openapi.json\n", *port)
		fmt.Fprintf(stdout, "\n")
		fmt.Fprintf(stdout, "Note: This is a lightweight API-only scaffold.\n")
		fmt.Fprintf(stdout, "  Admin panel, file storage, and mail are not included.\n")
		fmt.Fprintf(stdout, "  Add the full app by removing .WithoutDefaults() in main.go.\n")
		fmt.Fprintf(stdout, "  WARNING: WithoutDefaults() also disables authz — all routes are\n")
		fmt.Fprintf(stdout, "  unauthenticated. Add access control before exposing this service.\n")
	} else {
		fmt.Fprintf(stdout, "  Web:   http://localhost:%d/\n", *port)
		fmt.Fprintf(stdout, "  API:   http://localhost:%d/api/articles\n", *port)
		fmt.Fprintf(stdout, "  Admin: http://localhost:%d/admin\n", *port)
	}
	return nil
}

func defaultModulePath(projectName string) string {
	slug := toSnakeCase(projectName)
	if slug == "" {
		slug = "nucleus_app"
	}
	return "example.com/" + slug
}

// resolveFrameworkVersion returns the module version string to use in generated
// go.mod files. When the CLI was built with a release tag (e.g. "0.5.5" via
// goreleaser ldflags), we use "v" + Version. For development builds ("dev"),
// we use "latest" so `go mod tidy` resolves the newest published tag.
func resolveFrameworkVersion() string {
	v := strings.TrimSpace(Version)
	if v == "" || v == "dev" {
		return "latest"
	}
	// goreleaser sets Version without the "v" prefix (e.g. "0.5.5").
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}
