package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/internal/routedump"
	"github.com/jcsvwinston/nucleus/pkg/app"
)

type routeEntry struct {
	Method      string `json:"method"`
	Pattern     string `json:"pattern"`
	Module      string `json:"module"`
	Middlewares int    `json:"middlewares"`
}

// runRoutes lists the routes of the application in the current project. It
// answers from the compiled binary: with a go.mod in --dir it builds the
// main package and runs it with NUCLEUS_PRINT_ROUTES set, which makes
// nucleus.Run print the route table — the framework's routes and every
// mounted module's, attributed — and exit before listening. The build
// output and the application's own log lines stay off this command's
// stdout; only the table is printed. The run is bounded by --timeout: a
// main that serves instead of printing (it does not boot through
// nucleus.Run, or its OnStart blocks) is killed with its process group and
// reported as an error, never left listening. A project whose nucleus
// requirement predates the variable is not built at all: the command says
// so and answers from configuration. Outside a project, or with
// --framework-only, it falls back to the configuration-only listing (a
// fresh app built from nucleus.yml, which mounts no module).
//
// --config belongs to the configuration-only listing: the binary reads its
// own configuration, so on the binary path the flag is refused rather than
// silently ignored.
func runRoutes(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("routes", flag.ContinueOnError)
	fs.SetOutput(stderr)
	installUsage(fs, "routes")

	configPath := fs.String("config", "", "Path to nucleus config file (configuration-only listing; your binary reads its own)")
	dir := fs.String("dir", ".", "Project directory containing go.mod and the application's main package")
	frameworkOnly := fs.Bool("framework-only", false, "List the framework's own routes from configuration without building or running the application")
	timeout := fs.Duration("timeout", defaultRoutesTimeout, "How long the built application may take to print its routes before it is killed (the build is not counted)")
	pathPrefix := fs.String("path", "", "Filter routes by prefix")
	asJSON := fs.Bool("json", false, "Print routes as JSON")
	verbose := fs.Bool("verbose", false, "Include middleware count")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("routes does not accept positional arguments")
	}

	var (
		routes []routeEntry
		note   string
		err    error
	)
	switch {
	case *frameworkOnly:
		routes, err = frameworkRoutesFromConfig(*configPath)
		note = "NOTE: --framework-only: listing framework-owned routes only (built from configuration);\nthe modules of your binary are not mounted here."
	case projectHasGoMod(*dir):
		if *configPath != "" {
			return fmt.Errorf("--config applies to the configuration-only listing; the binary in %s reads its own configuration — add --framework-only to list the framework's routes from %s", *dir, *configPath)
		}
		if *timeout <= 0 {
			return fmt.Errorf("--timeout must be positive, got %s", *timeout)
		}
		dep := resolveNucleusDependency(*dir)
		if dep.known && !dep.carriesRouteDump {
			routes, err = frameworkRoutesFromConfig(projectConfigPath(*dir))
			note = fmt.Sprintf("NOTE: %s required by %s/go.mod predates %s: listing framework-owned routes only (built from configuration).\nRaise the requirement to a release that carries the variable to read the routes of your binary.", dep.describe(), *dir, routedump.EnvVar)
			break
		}
		routes, err = routesFromBinary(*dir, *timeout)
	default:
		routes, err = frameworkRoutesFromConfig(*configPath)
		note = fmt.Sprintf("NOTE: no go.mod in %s: listing framework-owned routes only (built from configuration).\nRun this command inside your project (or pass --dir) to read the routes of your binary.", *dir)
	}
	if err != nil {
		return err
	}

	filtered := routes[:0]
	for _, r := range routes {
		if *pathPrefix != "" && !strings.HasPrefix(r.Pattern, *pathPrefix) {
			continue
		}
		filtered = append(filtered, r)
	}
	routes = filtered

	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].Pattern == routes[j].Pattern {
			return routes[i].Method < routes[j].Method
		}
		return routes[i].Pattern < routes[j].Pattern
	})

	if outputWantsJSON(*asJSON) {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(routes)
	}

	if note != "" {
		fmt.Fprintln(stdout, note)
		fmt.Fprintln(stdout, "")
	}
	if len(routes) == 0 {
		fmt.Fprintln(stdout, "No routes registered")
		return nil
	}

	if outputIsPretty() {
		fmt.Fprintf(stdout, "Routes: %d\n", len(routes))
		for _, r := range routes {
			owner := ""
			if r.Module != "" {
				owner = "  (" + r.Module + ")"
			}
			if *verbose {
				fmt.Fprintf(stdout, "  %s  %s  mw=%d%s\n", r.Method, r.Pattern, r.Middlewares, owner)
				continue
			}
			fmt.Fprintf(stdout, "  %s  %s%s\n", r.Method, r.Pattern, owner)
		}
		return nil
	}

	// Plain output appends the module as the LAST column so every column a
	// consumer read before the binary path existed keeps its position:
	// METHOD, PATTERN and, with --verbose, middleware=N third as it always
	// was. The JSON shape carries the module key on every entry (empty for
	// the framework's own routes) so consumers need no presence check.
	for _, r := range routes {
		if *verbose {
			fmt.Fprintf(stdout, "%s\t%s\tmiddleware=%d\t%s\n", r.Method, r.Pattern, r.Middlewares, r.Module)
			continue
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", r.Method, r.Pattern, r.Module)
	}
	return nil
}

func projectHasGoMod(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil && !info.IsDir()
}

// projectConfigPath is the project's own nucleus.yml when it has one, so
// the configuration-only fallback inside a project reads the file the
// binary would, not one from the working directory; empty (defaults)
// otherwise.
func projectConfigPath(dir string) string {
	p := filepath.Join(dir, "nucleus.yml")
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p
	}
	return ""
}

// defaultRoutesTimeout bounds the run of the built application, not its
// build: a boot that prints the table takes well under a second on top of
// whatever the module OnStart hooks do.
const defaultRoutesTimeout = 30 * time.Second

// nucleusModulePath is the module the pre-check resolves in the project.
const nucleusModulePath = "github.com/jcsvwinston/nucleus"

// nucleusDependency is what the project's go.mod resolves for the
// framework: where the module lives on disk and whether that copy carries
// the route dump (internal/routedump exists there). known is false when the
// project does not require nucleus or the resolution failed; the command
// then runs the binary and lets the deadline decide.
type nucleusDependency struct {
	known            bool
	version          string
	replacePath      string
	carriesRouteDump bool
}

func (d nucleusDependency) describe() string {
	if d.replacePath != "" {
		return fmt.Sprintf("the nucleus checkout %s", d.replacePath)
	}
	return fmt.Sprintf("nucleus %s", d.version)
}

// resolveNucleusDependency asks `go list -m` (and `go mod download` when
// the module is not in the cache yet — the build would fetch it anyway)
// for the directory of the nucleus module the project resolves, with any
// replace applied, and checks it for internal/routedump. Every release
// that reads NUCLEUS_PRINT_ROUTES carries that package; a release that
// predates it does not, and running such a binary would serve instead of
// printing. Resolving through the toolchain rather than parsing go.mod
// keeps replace directives, workspaces and pseudo-versions honest and
// needs no version constant that would go stale.
func resolveNucleusDependency(dir string) nucleusDependency {
	type moduleInfo struct {
		Version string
		Dir     string
		Replace *struct {
			Path string
			Dir  string
		}
	}
	var info moduleInfo
	list := exec.Command("go", "list", "-m", "-json", nucleusModulePath)
	list.Dir = dir
	list.Stderr = io.Discard
	out, err := list.Output()
	if err != nil || json.Unmarshal(out, &info) != nil {
		return nucleusDependency{}
	}
	if info.Dir == "" {
		download := exec.Command("go", "mod", "download", "-json", nucleusModulePath)
		download.Dir = dir
		download.Stderr = io.Discard
		out, err := download.Output()
		if err != nil || json.Unmarshal(out, &info) != nil || info.Dir == "" {
			return nucleusDependency{}
		}
	}
	dep := nucleusDependency{known: true, version: info.Version}
	if info.Replace != nil {
		dep.replacePath = info.Replace.Path
	}
	_, statErr := os.Stat(filepath.Join(info.Dir, "internal", "routedump", "routedump.go"))
	dep.carriesRouteDump = statErr == nil
	return dep
}

// routesFromBinary builds the project's main package, runs the binary with
// NUCLEUS_PRINT_ROUTES set for at most timeout, and reads the table
// nucleus.Run prints. The child's stdout is captured whole (the structured
// logger writes there too) and searched for the document line; its stderr
// — the application's own — is shown only when the run fails. The binary
// runs in its own process group and the whole group is killed at the
// deadline, so an application that serves instead of printing (a main that
// never reaches nucleus.Run, an OnStart that blocks) is never left
// listening behind this command. The build is a separate step with no
// deadline: a cold module cache must not turn into "your application kept
// running".
func routesFromBinary(dir string, timeout time.Duration) ([]routeEntry, error) {
	tmp, err := os.MkdirTemp("", "nucleus-routes-")
	if err != nil {
		return nil, fmt.Errorf("create build directory: %w", err)
	}
	defer os.RemoveAll(tmp)
	bin := filepath.Join(tmp, "app")

	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = dir
	var buildOut bytes.Buffer
	build.Stdout = &buildOut
	build.Stderr = &buildOut
	if err := build.Run(); err != nil {
		return nil, fmt.Errorf("go build . (in %s) failed: %w\n%s", dir, err, strings.TrimSpace(buildOut.String()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), routedump.EnvVar+"=1")
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runInOwnProcessGroup(cmd)
	runErr := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("the application in %s kept running for %s instead of printing its routes and was stopped: it must boot through nucleus.Run (or Start), which reads %s and exits before listening — a main that serves through another path cannot be listed this way; use --framework-only for the configuration-only listing, or --timeout to allow a slower start", dir, timeout, routedump.EnvVar)
	}
	if runErr != nil {
		return nil, fmt.Errorf("the application in %s failed under %s=1: %w\n%s", dir, routedump.EnvVar, runErr, strings.TrimSpace(stderr.String()))
	}

	doc, found, err := routedump.Parse(stdout.Bytes())
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("the application in %s exited without printing its routes: it must reach nucleus.Run (or Start) with %s set — a main that exits through another path cannot be listed this way; use --framework-only for the configuration-only listing", dir, routedump.EnvVar)
	}
	routes := make([]routeEntry, 0, len(doc.Routes))
	for _, r := range doc.Routes {
		routes = append(routes, routeEntry{Method: r.Method, Pattern: r.Pattern, Module: r.Module, Middlewares: r.Middlewares})
	}
	return routes, nil
}

// frameworkRoutesFromConfig is the configuration-only listing: a fresh app
// built from the config file, which mounts no module, walked and torn down.
func frameworkRoutesFromConfig(configPath string) ([]routeEntry, error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}
	a, err := app.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("create app: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = a.Shutdown(ctx)
	}()

	routes := make([]routeEntry, 0, 16)
	if err := a.Router.Walk(func(method string, route string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if method == "" {
			method = "*"
		}
		routes = append(routes, routeEntry{
			Method:      method,
			Pattern:     route,
			Middlewares: len(middlewares),
		})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk routes: %w", err)
	}
	return routes, nil
}
