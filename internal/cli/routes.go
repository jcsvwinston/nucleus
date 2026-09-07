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
	"os/signal"
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
// answers from the compiled binary: --dir (default ".") is the directory of
// the application's main package and the project is the nearest go.mod at
// or above it, so both a main.go at the module root and the cmd/<app>
// layout are read. The command builds that package and runs it with
// NUCLEUS_PRINT_ROUTES set, which makes nucleus.Run print the route table —
// the framework's routes and every mounted module's, attributed — and exit
// before listening. The build output and the application's own log lines
// stay off this command's stdout; only the table is printed. The run is
// bounded by --timeout and by the command's own life: a main that serves
// instead of printing (it does not boot through nucleus.Run, or its OnStart
// blocks) is killed with its process group at the deadline, and so is the
// application when the command itself is interrupted (SIGINT, SIGTERM) —
// nothing is left listening either way. A --dir that holds no main package
// (the module root of a cmd/<app> layout, a library package) is an error
// that names --dir and the main packages the module holds, not a build
// failure. A project whose nucleus requirement predates the variable is not
// built at all: the command says so and answers from configuration.
// Outside a project, or with --framework-only, it falls back to the
// configuration-only listing (a fresh app built from nucleus.yml, which
// mounts no module, run at log level error so its boot log stays off
// stdout).
//
// --config belongs to the configuration-only listing: the binary reads its
// own configuration, so on the binary path the flag is refused rather than
// silently ignored.
func runRoutes(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("routes", flag.ContinueOnError)
	fs.SetOutput(stderr)
	installUsage(fs, "routes")

	configPath := fs.String("config", "", "Path to nucleus config file (configuration-only listing; your binary reads its own)")
	dir := fs.String("dir", ".", "Directory of the application's main package; the project is the nearest go.mod at or above it")
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
		note = "NOTE: --framework-only: listing framework-owned routes only. Built from configuration;\nthe modules of your binary are not mounted here."
	default:
		root, inProject := findModuleRoot(*dir)
		if !inProject {
			routes, err = frameworkRoutesFromConfig(*configPath)
			note = fmt.Sprintf("NOTE: no go.mod at or above %s: listing framework-owned routes only. Built from configuration.\nRun this command inside your project (or pass --dir with the directory of your main package) to read the routes of your binary.", *dir)
			break
		}
		if *configPath != "" {
			return fmt.Errorf("--config applies to the configuration-only listing; the binary in %s reads its own configuration — add --framework-only to list the framework's routes from %s", *dir, *configPath)
		}
		if *timeout <= 0 {
			return fmt.Errorf("--timeout must be positive, got %s", *timeout)
		}
		dep := resolveNucleusDependency(root)
		if dep.known && !dep.carriesRouteDump {
			routes, err = frameworkRoutesFromConfig(projectConfigPath(*dir, root))
			note = fmt.Sprintf("NOTE: %s required by %s predates %s: listing framework-owned routes only. Built from configuration.\nRaise the requirement to a release that carries the variable to read the routes of your binary.", dep.describe(), filepath.Join(root, "go.mod"), routedump.EnvVar)
			break
		}
		routes, err = routesFromBinary(*dir, root, *timeout)
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

// findModuleRoot resolves the project --dir belongs to: the nearest
// directory at or above it that holds a go.mod, as the go tool itself
// resolves the main module. "Inside a project" therefore covers the main
// package at the module root, a cmd/<app> layout and any subdirectory the
// command is run from; only a directory with no go.mod anywhere above it
// is outside. The root is returned absolute.
func findModuleRoot(dir string) (string, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		if info, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil && !info.IsDir() {
			return abs, true
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", false
		}
		abs = parent
	}
}

// projectConfigPath is the project's own nucleus.yml when it has one, so
// the configuration-only fallback inside a project reads the file the
// binary would: the one at the module root first (the binary's working
// directory, as `go run ./cmd/<app>` from the root), then the one next to
// the main package; empty (defaults) otherwise.
func projectConfigPath(dir, root string) string {
	for _, candidate := range []string{filepath.Join(root, "nucleus.yml"), filepath.Join(dir, "nucleus.yml")} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// ensureMainPackage classifies --dir before anything is built: `go build`
// in a directory without Go files fails with its raw output, and in a
// library package it exits 0 writing a package archive that cannot run —
// neither says what to do. The error returned here names --dir, the main
// packages the module holds (best effort, from `go list ./...` at the
// root) and, for routes, --framework-only. command is the caller
// (`routes` or `dev`): only routes has the configuration-only listing to
// point at.
//
// `go list` writes the package name to stdout and its chatter to stderr —
// a cold module cache prints one `go: downloading ...` line per module,
// `-mod=mod` prints `go: finding module for package ...` — so the two are
// read apart: the name is compared alone, and stderr is only shown when
// the classification fails. Reading them combined glued that chatter to
// the name and refused a fresh clone with "it is the library package go:
// downloading ... main".
func ensureMainPackage(dir, root, command string) error {
	list := exec.Command("go", "list", "-f", "{{.Name}}", ".")
	list.Dir = dir
	var stdout, stderr bytes.Buffer
	list.Stdout = &stdout
	list.Stderr = &stderr
	err := list.Run()
	name := strings.TrimSpace(stdout.String())
	chatter := strings.TrimSpace(stderr.String())
	var reason string
	switch {
	case err != nil && strings.Contains(chatter, "no Go files"):
		reason = "it holds no Go files"
	case err != nil:
		return fmt.Errorf("go list . (in %s) failed: %w\n%s", dir, err, strings.TrimSpace(chatter+"\n"+name))
	case name != "main":
		reason = fmt.Sprintf("it is the library package %s, not a main package", name)
	default:
		return nil
	}
	hint := ""
	if mains := mainPackageDirs(root); len(mains) > 0 {
		hint = " — this project's main packages: " + strings.Join(mains, ", ")
	}
	wayOut := ""
	if command == "routes" {
		wayOut = "; or use --framework-only for the configuration-only listing"
	}
	return fmt.Errorf("nothing to run in %s: %s. Pass --dir with the directory of your main package%s%s", dir, reason, hint, wayOut)
}

// mainPackageDirs lists the directories of the main packages under the
// module root, as paths the user can pass to --dir from the working
// directory. Best effort: a module that does not list cleanly yields none.
func mainPackageDirs(root string) []string {
	list := exec.Command("go", "list", "-f", `{{if eq .Name "main"}}{{.Dir}}{{end}}`, "./...")
	list.Dir = root
	list.Stderr = io.Discard
	out, err := list.Output()
	if err != nil {
		return nil
	}
	cwd, _ := os.Getwd()
	var dirs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		shown := line
		if cwd != "" {
			if rel, err := filepath.Rel(cwd, line); err == nil && !strings.HasPrefix(rel, "..") {
				shown = "."
				if rel != "." {
					shown = "./" + filepath.ToSlash(rel)
				}
			}
		}
		dirs = append(dirs, "--dir "+shown)
	}
	return dirs
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

// routesFromBinary builds the main package in dir and runs the binary from
// root, the project's module root — the working directory `go run
// ./cmd/<app>` gives it, where its nucleus.yml and relative paths resolve
// — with NUCLEUS_PRINT_ROUTES set for at most timeout, and reads the table
// nucleus.Run prints. The child's stdout is captured whole (the structured
// logger writes there too) and searched for the document line; its stderr
// — the application's own — is shown only when the run fails, with the
// tail of stdout when stderr is empty (a boot that fails through the
// structured logger says why there). The binary runs in its own process group and the whole group is
// killed when the run context ends, so an application that serves instead
// of printing (a main that never reaches nucleus.Run, an OnStart that
// blocks) is never left listening behind this command. That context ends
// at the deadline AND when this command receives an interrupt or a
// termination signal: the child being in its own group means a terminal's
// Ctrl-C never reaches it on its own, and a SIGTERM to the command would
// otherwise orphan it with its listener and the build directory. The
// build is a separate step with no deadline (a cold module cache must not
// turn into "your application kept running") but shares the signal
// context, so an interrupted build is reported as such.
func routesFromBinary(dir, root string, timeout time.Duration) ([]routeEntry, error) {
	if err := ensureMainPackage(dir, root, "routes"); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "nucleus-routes-")
	if err != nil {
		return nil, fmt.Errorf("create build directory: %w", err)
	}
	defer os.RemoveAll(tmp)
	bin := filepath.Join(tmp, "app")

	sigCtx, stop := signal.NotifyContext(context.Background(), routesStopSignals()...)
	defer stop()

	if buildOut, err := buildMainPackage(sigCtx, dir, bin); err != nil {
		if sigCtx.Err() != nil {
			return nil, fmt.Errorf("stopped on signal while building the application in %s", dir)
		}
		return nil, fmt.Errorf("go build . (in %s) failed: %w\n%s", dir, err, buildOut)
	}
	return readRouteDump(sigCtx, bin, dir, root, timeout, nil)
}

// buildMainPackage compiles the main package in dir into the binary bin
// (`go build -o bin .`) and returns the trimmed build output with the
// error. It is the one build step `routes` and `dev` share; the caller
// decides what a failure means (an error for routes, "keep the last good
// binary" for dev).
func buildMainPackage(ctx context.Context, dir, bin string) (string, error) {
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
	build.Dir = dir
	var out bytes.Buffer
	build.Stdout = &out
	build.Stderr = &out
	err := build.Run()
	return strings.TrimSpace(out.String()), err
}

// readRouteDump runs the built binary bin from root with NUCLEUS_PRINT_ROUTES
// set for at most timeout — in its own process group, killed whole when the
// run context ends — and returns the table nucleus.Run printed. extraEnv is
// appended to the process environment after the variable (dev passes the
// NUCLEUS_ENV and NUCLEUS_PORT it gives the application). The errors name
// dir, the directory of the main package, as the user passed it.
func readRouteDump(sigCtx context.Context, bin, dir, root string, timeout time.Duration, extraEnv []string) ([]routeEntry, error) {
	ctx, cancel := context.WithTimeout(sigCtx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = root
	cmd.Env = append(append(os.Environ(), routedump.EnvVar+"=1"), extraEnv...)
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runInOwnProcessGroup(cmd)
	runErr := cmd.Run()
	switch {
	case sigCtx.Err() != nil:
		return nil, fmt.Errorf("stopped on signal: the application in %s was killed with its process group before it printed its routes", dir)
	case ctx.Err() == context.DeadlineExceeded:
		return nil, fmt.Errorf("the application in %s kept running for %s instead of printing its routes and was stopped: it must boot through nucleus.Run (or Start), which reads %s and exits before listening — a main that serves through another path cannot be listed this way; use --framework-only for the configuration-only listing, or --timeout to allow a slower start", dir, timeout, routedump.EnvVar)
	case runErr != nil:
		return nil, fmt.Errorf("the application in %s failed under %s=1: %w\n%s", dir, routedump.EnvVar, runErr, failureOutput(stderr.String(), stdout.String()))
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

// failureOutput is what a failed run shows: stderr when the application
// wrote there, else the last lines of stdout (the structured logger's
// home), so the reason is never swallowed with the capture.
func failureOutput(stderr, stdout string) string {
	if out := strings.TrimSpace(stderr); out != "" {
		return out
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	const keep = 10
	if len(lines) > keep {
		lines = lines[len(lines)-keep:]
	}
	return strings.Join(lines, "\n")
}

// frameworkRoutesFromConfig is the configuration-only listing: a fresh app
// built from the config file, which mounts no module, walked and torn down.
// The app's logger writes to stdout, where the table goes: it is built at
// log level error so its boot lines (metrics, telemetry, auth, storage)
// never precede the table and --json stays parseable on this path too.
func frameworkRoutesFromConfig(configPath string) ([]routeEntry, error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}
	cfg.LogLevel = "error"
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
