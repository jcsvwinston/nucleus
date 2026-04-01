package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func runTestServer(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("testserver", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	fixturePath := fs.String("fixture", "", "Path to fixture JSON file")
	tablesRaw := fs.String("tables", "", "Comma-separated subset of fixture tables to load")
	truncate := fs.Bool("truncate", true, "Truncate fixture tables before loading")
	force := fs.Bool("force", false, "Force destructive actions (recommended in CI)")
	yes := fs.Bool("yes", false, "Auto-confirm destructive actions without prompt")
	dryRun := fs.Bool("dry-run", false, "Run fixture load plan and skip server startup")
	host := fs.String("host", "", "Override host")
	port := fs.Int("port", 0, "Override port")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if *port < 0 || *port > 65535 {
		return fmt.Errorf("invalid --port %d: expected 0-65535", *port)
	}

	path, err := resolveTestServerFixturePath(*fixturePath, fs.Args())
	if err != nil {
		return err
	}

	loadArgs := buildTestServerLoadArgs(testServerLoadOptions{
		configPath: *configPath,
		fixture:    path,
		tablesRaw:  *tablesRaw,
		truncate:   *truncate,
		force:      *force,
		yes:        *yes,
		dryRun:     *dryRun,
	})
	if err := runLoadData(loadArgs, stdin, stdout, stderr); err != nil {
		return fmt.Errorf("testserver fixture load: %w", err)
	}

	if *dryRun {
		fmt.Fprintln(stdout, "Dry-run completed; server startup skipped")
		return nil
	}

	serveArgs := buildTestServerServeArgs(*configPath, *host, *port)
	return runServe(serveArgs, stdin, stdout, stderr)
}

type testServerLoadOptions struct {
	configPath string
	fixture    string
	tablesRaw  string
	truncate   bool
	force      bool
	yes        bool
	dryRun     bool
}

func resolveTestServerFixturePath(flagValue string, positional []string) (string, error) {
	flagValue = strings.TrimSpace(flagValue)
	if flagValue == "" {
		if len(positional) != 1 {
			return "", fmt.Errorf("usage: goframe testserver [--config goframe.yaml] [--host ...] [--port ...] [--truncate] [--dry-run] <fixture.json>")
		}
		flagValue = strings.TrimSpace(positional[0])
	} else if len(positional) > 0 {
		return "", fmt.Errorf("testserver accepts either positional fixture path or --fixture, not both")
	}

	if flagValue == "" {
		return "", fmt.Errorf("fixture path cannot be empty")
	}
	return flagValue, nil
}

func buildTestServerLoadArgs(opts testServerLoadOptions) []string {
	out := make([]string, 0, 12)
	if strings.TrimSpace(opts.configPath) != "" {
		out = append(out, "--config", strings.TrimSpace(opts.configPath))
	}
	if strings.TrimSpace(opts.tablesRaw) != "" {
		out = append(out, "--tables", strings.TrimSpace(opts.tablesRaw))
	}
	if opts.truncate {
		out = append(out, "--truncate")
	}
	if opts.force {
		out = append(out, "--force")
	}
	if opts.yes {
		out = append(out, "--yes")
	}
	if opts.dryRun {
		out = append(out, "--dry-run")
	}
	out = append(out, opts.fixture)
	return out
}

func buildTestServerServeArgs(configPath, host string, port int) []string {
	out := make([]string, 0, 6)
	if strings.TrimSpace(configPath) != "" {
		out = append(out, "--config", strings.TrimSpace(configPath))
	}
	if strings.TrimSpace(host) != "" {
		out = append(out, "--host", strings.TrimSpace(host))
	}
	if port > 0 {
		out = append(out, "--port", strconv.Itoa(port))
	}
	return out
}
