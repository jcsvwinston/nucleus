// Command sample-app is a minimal Nucleus application used by
// examples/admin-quickstart. It registers a /healthz handler, emits
// synthetic HTTP traffic against itself on a timer (so the admin UI
// has something to look at), and wires the admin observability agent
// via the standard app.Extension path.
//
// This is NOT an idiomatic Nucleus app — production apps use
// `nucleus new` to scaffold a richer layout. The point here is to be
// the smallest thing that exercises the full agent → admin server
// data path.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jcsvwinston/nucleus/admin/agent"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/observability"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := app.DefaultConfig()
	cfg.Port = portFromEnv("PORT", 8080)
	cfg.Env = "development"
	cfg.LogFormat = "json"
	cfg.LogLevel = "info"
	cfg.AdminPrefix = ""           // disable legacy CRUD admin (no DB here)
	cfg.AdminAuthDatabase = "skip"

	cfg.StateDir = strings.TrimSpace(os.Getenv("NUCLEUS_STATE_DIR"))
	if cfg.StateDir == "" {
		cfg.StateDir = "./.nucleus-state"
	}
	cfg.AdminAgent.Endpoints = splitCSV(os.Getenv("NUCLEUS_ADMIN_ENDPOINTS"))
	cfg.AdminAgent.Token = os.Getenv("NUCLEUS_ADMIN_TOKEN")
	cfg.AdminAgent.Labels = map[string]string{
		"role": "sample-app",
		"pod":  hostname(),
	}

	a, err := app.New(&cfg, app.WithExtensions(
		agent.NewExtension(cfg.AdminAgent, cfg.StateDir, "v0.0.0-quickstart"),
	))
	if err != nil {
		return fmt.Errorf("app.New: %w", err)
	}

	a.Router.Get("/healthz", router.FromHTTP(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	}))
	a.Router.Get("/", router.FromHTTP(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "sample-app on %s — request id %s\n",
			hostname(), r.Header.Get("X-Request-Id"))
	}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		emitSyntheticTraffic(ctx, a.Observability, hostname())
	}()

	logger.Info("sample-app starting",
		"port", cfg.Port,
		"admin_endpoints", cfg.AdminAgent.Endpoints)
	if err := a.Run(ctx); err != nil {
		return err
	}
	wg.Wait()
	return nil
}

// emitSyntheticTraffic invents one fake HTTP event every 2 seconds so
// the admin UI has rolling activity to display in the quickstart.
// Production apps don't need this — they emit naturally as users hit
// real endpoints.
func emitSyntheticTraffic(ctx context.Context, bus *observability.Bus, node string) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	paths := []string{"/api/articles", "/api/users", "/api/orders/42", "/healthz"}
	statuses := []int{200, 200, 200, 201, 204, 404, 500}
	i := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !bus.HasSubscribers(observability.KindHTTPRequest) {
				continue
			}
			ev := observability.AcquireHTTPRequestEvent(time.Now().UTC(), node)
			ev.Method = "GET"
			ev.Path = paths[i%len(paths)]
			ev.Status = statuses[i%len(statuses)]
			ev.Duration = time.Duration(int64(i%9)+1) * time.Millisecond
			ev.RequestID = fmt.Sprintf("synth-%d", i)
			ev.UserAgent = "synthetic/1.0"
			ev.RemoteIP = "127.0.0.1"
			bus.Emit(ev)
			i++
		}
	}
}

func portFromEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	var p int
	if _, err := fmt.Sscanf(raw, "%d", &p); err != nil || p <= 0 {
		return fallback
	}
	return p
}

func splitCSV(s string) []string {
	out := make([]string, 0)
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}
