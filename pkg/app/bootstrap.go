package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// Bootstrap is a high-level helper that loads the configuration from a file
// and initializes a new App container with default settings.
// It simplifies the typical main.go boilerplate into a single call.
func Bootstrap(configPath string, opts ...Option) (*App, error) {
	if configPath == "" {
		configPath = "goframe.yaml"
	}
	
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: failed to load config: %w", err)
	}
	
	return New(cfg, opts...)
}

// QuickStart provides a "standard" entry point for enterprise applications.
// It handles configuration loading, app initialization, signal handling for 
// graceful shutdown, and error reporting.
//
// Example:
//
//	func main() {
//	    app.QuickStart(func(a *app.App) error {
//	        a.Router.Get("/", func(c *router.Context) error {
//	            return c.String(200, "Hello GoFrame")
//	        })
//	        return nil
//	    })
//	}
func QuickStart(fn func(a *App) error, opts ...Option) {
	configPath := os.Getenv("GOFRAME_CONFIG")
	if configPath == "" {
		configPath = "goframe.yaml"
	}

	a, err := Bootstrap(configPath, opts...)
	if err != nil {
		log.Fatalf("FAILED TO BOOTSTRAP: %v", err)
	}

	if err := fn(a); err != nil {
		log.Fatalf("FAILED TO INITIALIZE: %v", err)
	}

	// Wait for termination signal
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := a.Run(ctx); err != nil {
		log.Fatalf("APPLICATION ERROR: %v", err)
	}
}
