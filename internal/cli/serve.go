package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// printModuleBlindnessNote is the serve/testserver sibling of the DX-14 note
// `routes` prints: these commands build an application FROM CONFIG ONLY —
// the modules compiled into YOUR binary are not mounted, so none of your
// routes exist on the server they start. Saying so up front matters because
// a developer who runs `nucleus serve` (or the `runserver` alias, straight
// from the README's command list) and gets 404s on every own route used to
// have no hint why.
func printModuleBlindnessNote(w io.Writer, command string) {
	fmt.Fprintf(w, "NOTE: %s starts an application built from configuration only. Modules\n", command)
	fmt.Fprintln(w, "compiled into YOUR binary are not mounted here — their routes will 404 on")
	fmt.Fprintln(w, "this server. To serve your application, run your own binary (go run .).")
	fmt.Fprintln(w, "")
}

func runServe(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	installUsage(fs, "serve")

	configPath := fs.String("config", "", "Path to nucleus config file")
	host := fs.String("host", "", "Override host")
	port := fs.Int("port", 0, "Override port")
	withoutDefaults := fs.Bool("without-defaults", false, "Serve a core-only app without the default subsystems (admin, authz, mail, storage), matching what an api scaffold's go run . starts")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("serve does not accept positional arguments")
	}

	printModuleBlindnessNote(stdout, "serve")

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if *host != "" {
		cfg.Host = *host
	}
	if *port > 0 {
		cfg.Port = *port
	}

	if *withoutDefaults {
		a, err := app.New(cfg, app.WithoutDefaults())
		if err != nil {
			return fmt.Errorf("create app: %w", err)
		}
		fmt.Fprintf(stdout, "Nucleus server listening on http://%s\n", cfg.Addr())
		return a.Run(context.Background())
	}

	a, err := app.New(cfg)
	if err != nil {
		return fmt.Errorf("create app: %w", err)
	}

	fmt.Fprintf(stdout, "Nucleus server listening on http://%s\n", cfg.Addr())
	return a.Run(context.Background())
}
