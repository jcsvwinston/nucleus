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
	Module      string `json:"module,omitempty"`
	Middlewares int    `json:"middlewares"`
}

// runRoutes lists the routes of the application in the current project. It
// answers from the compiled binary: with a go.mod in --dir it runs
// `go run .` with NUCLEUS_PRINT_ROUTES set, which makes nucleus.Run print
// the route table — the framework's routes and every mounted module's,
// attributed — and exit before listening. The build output and the
// application's own log lines stay off this command's stdout; only the
// table is printed. Outside a project, or with --framework-only, it falls
// back to the configuration-only listing (a fresh app built from
// nucleus.yml, which mounts no module).
func runRoutes(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("routes", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to nucleus config file (configuration-only listing; your binary reads its own)")
	dir := fs.String("dir", ".", "Project directory containing go.mod and the application's main package")
	frameworkOnly := fs.Bool("framework-only", false, "List the framework's own routes from configuration without building or running the application")
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
		note = "NOTE: --framework-only lists framework-owned routes built from configuration;\nthe modules of your binary are not mounted here."
	case projectHasGoMod(*dir):
		routes, err = routesFromBinary(*dir)
	default:
		routes, err = frameworkRoutesFromConfig(*configPath)
		note = fmt.Sprintf("NOTE: no go.mod in %s: listing framework-owned routes built from configuration.\nRun this command inside your project (or pass --dir) to read the routes of your binary.", *dir)
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

	for _, r := range routes {
		if *verbose {
			fmt.Fprintf(stdout, "%s\t%s\t%s\tmiddleware=%d\n", r.Method, r.Pattern, r.Module, r.Middlewares)
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

// routesFromBinary compiles and runs the project's main package with
// NUCLEUS_PRINT_ROUTES set and reads the table nucleus.Run prints. The
// child's stdout is captured whole (the structured logger writes there
// too) and searched for the document line; its stderr — go's build errors,
// download notices — is shown only when the run fails.
func routesFromBinary(dir string) ([]routeEntry, error) {
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), routedump.EnvVar+"=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go run . (in %s) failed: %w\n%s", dir, err, strings.TrimSpace(stderr.String()))
	}

	doc, found, err := routedump.Parse(stdout.Bytes())
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("the application in %s exited without printing its routes: it must reach nucleus.Run (or Start) with %s set — a main that serves through another path, or a Nucleus release that predates the variable, cannot be listed this way; use --framework-only for the configuration-only listing", dir, routedump.EnvVar)
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
