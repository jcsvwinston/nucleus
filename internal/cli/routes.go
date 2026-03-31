package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jcsvwinston/GoFrame/pkg/app"
)

type routeEntry struct {
	Method      string `json:"method"`
	Pattern     string `json:"pattern"`
	Middlewares int    `json:"middlewares"`
}

func runRoutes(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("routes", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
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

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	a, err := app.New(cfg)
	if err != nil {
		return fmt.Errorf("create app: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = a.Shutdown(ctx)
	}()

	routes := make([]routeEntry, 0, 16)
	if err := chi.Walk(a.Router, func(method string, route string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if *pathPrefix != "" && !strings.HasPrefix(route, *pathPrefix) {
			return nil
		}
		routes = append(routes, routeEntry{
			Method:      method,
			Pattern:     route,
			Middlewares: len(middlewares),
		})
		return nil
	}); err != nil {
		return fmt.Errorf("walk routes: %w", err)
	}

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Pattern == routes[j].Pattern {
			return routes[i].Method < routes[j].Method
		}
		return routes[i].Pattern < routes[j].Pattern
	})

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(routes)
	}

	if len(routes) == 0 {
		fmt.Fprintln(stdout, "No routes registered")
		return nil
	}

	if *verbose {
		for _, r := range routes {
			fmt.Fprintf(stdout, "%s\t%s\tmiddleware=%d\n", r.Method, r.Pattern, r.Middlewares)
		}
		return nil
	}

	for _, r := range routes {
		fmt.Fprintf(stdout, "%s\t%s\n", r.Method, r.Pattern)
	}
	return nil
}
