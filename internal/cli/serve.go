package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/jcsvwinston/GoFrame/pkg/app"
)

func runServe(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	host := fs.String("host", "", "Override host")
	port := fs.Int("port", 0, "Override port")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("serve does not accept positional arguments")
	}

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

	a, err := app.New(cfg)
	if err != nil {
		return fmt.Errorf("create app: %w", err)
	}

	fmt.Fprintf(stdout, "GoFrame server listening on http://%s\n", cfg.Addr())
	return a.Run(context.Background())
}
