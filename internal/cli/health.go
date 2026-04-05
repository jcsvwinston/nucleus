package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/app"
)

type healthComponent struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Details string `json:"details,omitempty"`
}

type healthReport struct {
	Status     string            `json:"status"`
	CheckedAt  string            `json:"checked_at"`
	Components []healthComponent `json:"components"`
}

func runHealth(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	timeout := fs.Duration("timeout", 3*time.Second, "Health check timeout")
	asJSON := fs.Bool("json", false, "Print output as JSON")
	deploy := fs.Bool("deploy", false, "Run additional deployment hardening checks")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("health does not accept positional arguments")
	}
	if *timeout <= 0 {
		return fmt.Errorf("timeout must be greater than 0")
	}

	cfg, database, cleanup, err := newDatabase(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	report := healthReport{
		Status:    "ok",
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Components: []healthComponent{
			{Name: "database", Status: "ok", Details: fmt.Sprintf("engine=%s", cfg.DatabaseEngine)},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := database.Health(ctx); err != nil {
		report.Status = "degraded"
		report.Components[0].Status = "error"
		report.Components[0].Details = err.Error()
	}

	if *deploy {
		applyDeployChecks(cfg, &report)
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(stdout, "overall\t%s\n", report.Status)
		for _, c := range report.Components {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", c.Name, c.Status, c.Details)
		}
	}

	if report.Status != "ok" {
		return fmt.Errorf("health check failed")
	}
	return nil
}

func applyDeployChecks(cfg *app.Config, report *healthReport) {
	if cfg == nil || report == nil {
		return
	}

	addHealthComponent(report, healthComponent{
		Name:    "deploy.env",
		Status:  statusByCondition(cfg.IsProd(), "ok", "warning"),
		Details: "env should be production",
	})

	addHealthComponent(report, healthComponent{
		Name:    "deploy.debug",
		Status:  statusByCondition(!cfg.Debug, "ok", "error"),
		Details: "debug should be false",
	})

	jwtSecret := strings.TrimSpace(cfg.JWTSecret)
	addHealthComponent(report, healthComponent{
		Name:    "deploy.jwt_secret",
		Status:  statusByCondition(len(jwtSecret) >= 32, "ok", "error"),
		Details: "jwt_secret should be set with at least 32 chars",
	})

	addHealthComponent(report, healthComponent{
		Name:    "deploy.rate_limit",
		Status:  statusByCondition(cfg.RateLimitRequests > 0, "ok", "warning"),
		Details: "rate_limit_requests should be > 0 for internet-facing deployments",
	})

	addHealthComponent(report, healthComponent{
		Name:    "deploy.log_format",
		Status:  statusByCondition(strings.EqualFold(strings.TrimSpace(cfg.LogFormat), "json"), "ok", "warning"),
		Details: "log_format should be json in production",
	})

	addHealthComponent(report, healthComponent{
		Name:    "deploy.storage_driver",
		Status:  statusByCondition(strings.TrimSpace(cfg.StorageDriver) != "", "ok", "warning"),
		Details: "storage_driver should be configured",
	})

	applyDeploySessionChecks(cfg, report)
	applyDeployMailChecks(cfg, report)
}

func applyDeploySessionChecks(cfg *app.Config, report *healthReport) {
	store := normalizeSessionStore(cfg.SessionStore)
	if !isSupportedSessionStore(store) {
		addHealthComponent(report, healthComponent{
			Name:    "deploy.session_store",
			Status:  "error",
			Details: "session_store must be one of memory|sql|redis",
		})
		return
	}

	storeStatus := "ok"
	storeDetails := fmt.Sprintf("session_store=%s", store)
	if store == "memory" {
		storeStatus = "warning"
		storeDetails = "session_store=memory is process-local; use sql or redis for multi-replica deployments"
	}
	addHealthComponent(report, healthComponent{
		Name:    "deploy.session_store",
		Status:  storeStatus,
		Details: storeDetails,
	})

	if store == "redis" {
		redisURL := strings.TrimSpace(cfg.SessionRedisURL)
		if redisURL == "" {
			redisURL = strings.TrimSpace(cfg.RedisURL)
		}
		addHealthComponent(report, healthComponent{
			Name:    "deploy.session_redis_url",
			Status:  statusByCondition(redisURL != "", "ok", "error"),
			Details: "session_redis_url (or redis_url fallback) must be configured when session_store=redis",
		})
	}

	if store == "sql" {
		addHealthComponent(report, healthComponent{
			Name:    "deploy.session_table",
			Status:  statusByCondition(strings.TrimSpace(cfg.SessionTable) != "", "ok", "error"),
			Details: "session_table must be configured when session_store=sql",
		})
	}

	addHealthComponent(report, healthComponent{
		Name:    "deploy.session_cookie_secure",
		Status:  statusByCondition(cfg.SessionCookieSecure, "ok", "error"),
		Details: "session_cookie_secure should be true in production",
	})

	sameSite := strings.ToLower(strings.TrimSpace(cfg.SessionCookieSameSite))
	addHealthComponent(report, healthComponent{
		Name:    "deploy.session_cookie_samesite",
		Status:  statusByCondition(isValidSessionSameSite(sameSite), "ok", "error"),
		Details: "session_cookie_samesite should be lax, strict, or none",
	})

	if sameSite == "none" {
		addHealthComponent(report, healthComponent{
			Name:    "deploy.session_cookie_none_requires_secure",
			Status:  statusByCondition(cfg.SessionCookieSecure, "ok", "error"),
			Details: "session_cookie_samesite=none requires session_cookie_secure=true",
		})
	}
}

func applyDeployMailChecks(cfg *app.Config, report *healthReport) {
	driver := strings.ToLower(strings.TrimSpace(cfg.MailDriver))
	if driver == "" {
		driver = "noop"
	}

	mailFrom := strings.TrimSpace(cfg.MailFrom)
	addHealthComponent(report, healthComponent{
		Name:    "deploy.mail_from",
		Status:  statusByCondition(isValidEmailAddress(mailFrom), "ok", "warning"),
		Details: "mail_from should be a valid sender email address",
	})

	switch driver {
	case "noop":
		addHealthComponent(report, healthComponent{
			Name:    "deploy.mail_driver",
			Status:  "warning",
			Details: "mail_driver is noop; configure smtp/sendgrid/plugin for production email",
		})
	case "smtp":
		addHealthComponent(report, healthComponent{
			Name:    "deploy.mail_driver",
			Status:  "ok",
			Details: "mail_driver=smtp",
		})
		addHealthComponent(report, healthComponent{
			Name:    "deploy.mail.smtp",
			Status:  statusByCondition(strings.TrimSpace(cfg.SMTPHost) != "" && cfg.SMTPPort > 0, "ok", "error"),
			Details: "smtp_host and smtp_port should be configured for smtp driver",
		})
	case "sendgrid":
		addHealthComponent(report, healthComponent{
			Name:    "deploy.mail_driver",
			Status:  "ok",
			Details: "mail_driver=sendgrid",
		})
		addHealthComponent(report, healthComponent{
			Name:    "deploy.mail.sendgrid",
			Status:  statusByCondition(strings.TrimSpace(cfg.SendGridAPIKey) != "", "ok", "error"),
			Details: "sendgrid_api_key should be configured for sendgrid driver",
		})
	default:
		addHealthComponent(report, healthComponent{
			Name:    "deploy.mail_driver",
			Status:  "ok",
			Details: fmt.Sprintf("mail_driver=%s (external plugin goframe-plugin-%s or legacy goframe-mail-%s)", driver, driver, driver),
		})
	}
}

func isValidEmailAddress(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	_, err := mail.ParseAddress(raw)
	return err == nil
}

func addHealthComponent(report *healthReport, component healthComponent) {
	report.Components = append(report.Components, component)

	switch component.Status {
	case "error":
		report.Status = "degraded"
	case "warning":
		if report.Status == "ok" {
			report.Status = "warning"
		}
	}
}

func statusByCondition(ok bool, okStatus, failStatus string) string {
	if ok {
		return okStatus
	}
	return failStatus
}

func normalizeSessionStore(raw string) string {
	store := strings.ToLower(strings.TrimSpace(raw))
	if store == "" {
		return "memory"
	}
	return store
}

func isSupportedSessionStore(store string) bool {
	switch store {
	case "memory", "sql", "redis":
		return true
	default:
		return false
	}
}

func isValidSessionSameSite(raw string) bool {
	switch raw {
	case "lax", "strict", "none":
		return true
	default:
		return false
	}
}
