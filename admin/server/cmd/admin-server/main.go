// Command admin-server is the standalone Nucleus admin observability
// server. It accepts AgentService streams from agents (one per framework
// process) and ControlService unary/server-streaming calls from the
// embedded web UI.
//
// Configuration sources, in order of precedence:
//
//   1. Command-line flags
//   2. Environment variables (NUCLEUS_ADMIN_*)
//   3. Built-in defaults (see admin/server.Config.withDefaults)
//
// Two listeners run by default:
//
//   * --agent-addr (default :9090) — h2c by default; mTLS when --agent-cert
//     and --agent-key are supplied.
//   * --ui-addr (default :8080) — h2c + embedded UI; trusted-proxy headers
//     and bearer fallback per --ui-* flags.
//
// Run "admin-server --help" for the full surface.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	server "github.com/jcsvwinston/nucleus/admin/server"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("admin-server", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	agentAddr := fs.String("agent-addr", envOr("NUCLEUS_ADMIN_AGENT_ADDR", ":9090"), "agent listener address")
	uiAddr := fs.String("ui-addr", envOr("NUCLEUS_ADMIN_UI_ADDR", ":8080"), "UI/operator listener address")
	agentToken := fs.String("agent-token", os.Getenv("NUCLEUS_ADMIN_AGENT_TOKEN"), "shared bearer token agents present")
	uiBearer := fs.String("ui-bearer", os.Getenv("NUCLEUS_ADMIN_UI_BEARER"), "fallback UI bearer token (when no reverse proxy)")
	uiAuthHeader := fs.String("ui-auth-header", envOr("NUCLEUS_ADMIN_UI_AUTH_HEADER", "X-Auth-User"), "trusted-proxy header carrying authenticated user")
	uiEmailHeader := fs.String("ui-email-header", envOr("NUCLEUS_ADMIN_UI_EMAIL_HEADER", "X-Auth-Email"), "trusted-proxy header carrying user email")
	uiTrustedCIDRs := fs.String("ui-trusted-cidrs", os.Getenv("NUCLEUS_ADMIN_UI_TRUSTED_CIDRS"), "comma-separated CIDRs allowed to set trusted-proxy headers")
	agentCert := fs.String("agent-cert", os.Getenv("NUCLEUS_ADMIN_AGENT_CERT"), "PEM cert for agent listener (enables TLS)")
	agentKey := fs.String("agent-key", os.Getenv("NUCLEUS_ADMIN_AGENT_KEY"), "PEM key for agent listener")
	uiCert := fs.String("ui-cert", os.Getenv("NUCLEUS_ADMIN_UI_CERT"), "PEM cert for UI listener (enables TLS)")
	uiKey := fs.String("ui-key", os.Getenv("NUCLEUS_ADMIN_UI_KEY"), "PEM key for UI listener")
	logLevel := fs.String("log-level", envOr("NUCLEUS_ADMIN_LOG_LEVEL", "info"), "log level: debug | info | warn | error")
	logFormat := fs.String("log-format", envOr("NUCLEUS_ADMIN_LOG_FORMAT", "json"), "log format: json | text")
	versionFlag := fs.Bool("version", false, "print build version and exit")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *versionFlag {
		fmt.Println("nucleus-admin-server (phase 4)")
		return nil
	}

	logger := newLogger(*logLevel, *logFormat)

	cfg := server.Config{
		AgentAddr:           *agentAddr,
		UIAddr:              *uiAddr,
		AgentToken:          strings.TrimSpace(*agentToken),
		UIBearerToken:       strings.TrimSpace(*uiBearer),
		UIAuthHeader:        *uiAuthHeader,
		UIEmailHeader:       *uiEmailHeader,
		UITrustedProxyCIDRs: splitCSV(*uiTrustedCIDRs),
		Logger:              logger,
	}

	if *agentCert != "" || *agentKey != "" {
		tc, err := loadTLS(*agentCert, *agentKey)
		if err != nil {
			return fmt.Errorf("agent TLS: %w", err)
		}
		cfg.AgentTLS = tc
	}
	if *uiCert != "" || *uiKey != "" {
		tc, err := loadTLS(*uiCert, *uiKey)
		if err != nil {
			return fmt.Errorf("ui TLS: %w", err)
		}
		cfg.UITLS = tc
	}

	srv := server.New(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("admin-server boot",
		"agent_addr", cfg.AgentAddr,
		"ui_addr", cfg.UIAddr,
		"agent_token_set", cfg.AgentToken != "",
		"ui_bearer_set", cfg.UIBearerToken != "",
		"agent_tls", cfg.AgentTLS != nil,
		"ui_tls", cfg.UITLS != nil)

	if err := srv.Run(ctx); err != nil {
		return fmt.Errorf("admin-server: %w", err)
	}
	return nil
}

func newLogger(level, format string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "text":
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	default:
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
}

func loadTLS(certFile, keyFile string) (*tls.Config, error) {
	if strings.TrimSpace(certFile) == "" || strings.TrimSpace(keyFile) == "" {
		return nil, errors.New("both --cert and --key must be supplied to enable TLS")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func splitCSV(in string) []string {
	parts := strings.Split(in, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Defensive: keep time imported in case future flags add timeouts.
var _ = time.Second
