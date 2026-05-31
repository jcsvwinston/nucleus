package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/outbox"
)

type doctorStatus string

const (
	doctorStatusPass    doctorStatus = "pass"
	doctorStatusWarning doctorStatus = "warning"
	doctorStatusError   doctorStatus = "error"
)

type doctorCheck struct {
	name        string
	description string
	check       func(*app.Config, string) doctorCheckOutcome
}

type doctorCheckOutcome struct {
	status  doctorStatus
	message string
	err     error
}

type doctorResult struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	Duration  string    `json:"duration"`
	Timestamp time.Time `json:"timestamp"`
}

type doctorReport struct {
	OverallStatus string         `json:"overall_status"`
	TotalChecks   int            `json:"total_checks"`
	Passed        int            `json:"passed"`
	Failed        int            `json:"failed"`
	Warnings      int            `json:"warnings"`
	Results       []doctorResult `json:"results"`
	Timestamp     time.Time      `json:"timestamp"`
}

func runDoctor(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)

	checkName := fs.String("check", "", "Specific check to run (default: all)")
	configPath := fs.String("config", "", "Path to nucleus config file")
	jsonOutput := fs.Bool("json", false, "Output results as JSON")
	verbose := fs.Bool("verbose", false, "Show detailed output for each check")

	if err := fs.Parse(args); err != nil {
		return err
	}

	rest := fs.Args()
	if len(rest) > 0 {
		return fmt.Errorf("doctor does not accept positional arguments")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return fmt.Errorf("doctor load config: %w", err)
	}

	checks := []doctorCheck{
		{name: "tasks", description: "Check background tasks (Asynq) worker and queue health", check: checkTasks},
		{name: "outbox", description: "Check outbox dispatcher and pending events", check: checkOutbox},
		{name: "storage", description: "Check storage backend connectivity and bucket access", check: checkStorage},
		{name: "observability", description: "Check OpenTelemetry exporters and metrics", check: checkObservability},
		{name: "tenancy", description: "Check multi-tenant configuration and isolation", check: checkTenancy},
		{name: "rbac", description: "Check RBAC policies and Casbin enforcer", check: checkRBAC},
		{name: "audit", description: "Check audit log configuration and retention", check: checkAudit},
	}

	report := doctorReport{
		OverallStatus: "healthy",
		Results:       []doctorResult{},
		Timestamp:     time.Now().UTC(),
	}

	for _, check := range checks {
		if *checkName != "" && check.name != *checkName {
			continue
		}

		report.TotalChecks++
		start := time.Now()
		outcome := check.check(cfg, *configPath)
		duration := time.Since(start)

		result := doctorResult{
			Name:      check.name,
			Status:    string(outcome.status),
			Message:   outcome.message,
			Duration:  duration.String(),
			Timestamp: time.Now().UTC(),
		}

		switch {
		case outcome.err != nil:
			result.Status = string(doctorStatusError)
			result.Message = fmt.Sprintf("%s: %v", outcome.message, outcome.err)
			report.Failed++
			report.OverallStatus = "unhealthy"
		case outcome.status == doctorStatusPass:
			report.Passed++
		case outcome.status == doctorStatusWarning:
			report.Warnings++
			if report.OverallStatus == "healthy" {
				report.OverallStatus = "degraded"
			}
		default:
			result.Status = string(doctorStatusError)
			report.Failed++
			report.OverallStatus = "unhealthy"
		}

		report.Results = append(report.Results, result)

		if *verbose {
			fmt.Fprintf(stdout, "[%s] %s: %s (%s)\n", strings.ToUpper(result.Status), check.name, result.Message, duration)
		}
	}

	if *checkName != "" && report.TotalChecks == 0 {
		return fmt.Errorf("unknown doctor check %q", *checkName)
	}

	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return fmt.Errorf("encode JSON output: %w", err)
		}
		return nil
	}

	fmt.Fprintf(stdout, "\nDoctor Report (%s)\n", report.Timestamp.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(stdout, "Overall Status: %s\n", strings.ToUpper(report.OverallStatus))
	fmt.Fprintf(stdout, "Total Checks: %d | Passed: %d | Failed: %d | Warnings: %d\n\n",
		report.TotalChecks, report.Passed, report.Failed, report.Warnings)

	for _, result := range report.Results {
		statusSymbol := "✓"
		if result.Status == string(doctorStatusWarning) {
			statusSymbol = "!"
		}
		if result.Status == string(doctorStatusError) {
			statusSymbol = "✗"
		}
		fmt.Fprintf(stdout, "%s %-20s %s\n", statusSymbol, result.Name, result.Message)
	}

	fmt.Fprintln(stdout)

	if report.OverallStatus == "unhealthy" {
		return fmt.Errorf("doctor checks failed")
	}
	return nil
}

func checkTasks(cfg *app.Config, configPath string) doctorCheckOutcome {
	if strings.TrimSpace(cfg.RedisURL) == "" {
		return doctorWarning("Redis is not configured; Asynq-backed task queues are disabled or not inspectable")
	}
	return doctorPass("Redis URL is configured for task-backed features")
}

func checkOutbox(cfg *app.Config, configPath string) doctorCheckOutcome {
	if !cfg.Outbox.Enabled {
		return doctorWarning("Outbox is disabled in configuration")
	}
	loadedCfg, database, cleanup, err := newDatabase(configPath)
	if err != nil {
		return doctorError("Outbox is enabled but the default database could not be opened", err)
	}
	defer cleanup()

	sqlDB, err := database.SqlDB()
	if err != nil {
		return doctorError("Outbox is enabled but the SQL handle is unavailable", err)
	}
	dbCfg, _ := loadedCfg.DatabaseByAlias(loadedCfg.DefaultDatabaseAlias())
	snapshot := outbox.InspectRuntime(sqlDB, outbox.Config{
		TableName: cfg.Outbox.TableName,
		Flavor:    doctorOutboxFlavor(dbCfg.URL),
	})
	if !snapshot.Enabled {
		return doctorError("Outbox is enabled but runtime inspection failed", fmt.Errorf("%s", snapshot.Reason))
	}
	if snapshot.Failed > 0 {
		return doctorError("Outbox has failed messages", fmt.Errorf("%d failed messages", snapshot.Failed))
	}
	if snapshot.Processing > 0 {
		return doctorWarning(fmt.Sprintf("Outbox has %d processing messages; verify dispatcher progress", snapshot.Processing))
	}
	return doctorPass(fmt.Sprintf("Outbox table %q reachable; pending=%d delivered=%d", snapshot.Table, snapshot.Pending, snapshot.Delivered))
}

func checkStorage(cfg *app.Config, configPath string) doctorCheckOutcome {
	provider := strings.ToLower(strings.TrimSpace(cfg.Storage.Provider))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(cfg.StorageDriver))
	}
	if provider == "" {
		return doctorError("Storage provider is not configured", nil)
	}
	switch provider {
	case "local":
		path := strings.TrimSpace(cfg.Storage.Local.Path)
		if path == "" {
			path = strings.TrimSpace(cfg.StoragePath)
		}
		if path == "" {
			return doctorError("Local storage selected but no path is configured", nil)
		}
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return doctorWarning(fmt.Sprintf("Local storage path %q does not exist yet", path))
			}
			return doctorError("Local storage path cannot be inspected", err)
		}
		return doctorPass(fmt.Sprintf("Local storage path %q is accessible", path))
	case "s3":
		if strings.TrimSpace(cfg.Storage.S3.Bucket) == "" {
			return doctorError("S3 storage selected but storage.s3.bucket is empty", nil)
		}
		return doctorWarning("S3 storage is configured; run a provider-specific health check with deployment credentials")
	case "gcs":
		if strings.TrimSpace(cfg.Storage.GCS.Bucket) == "" {
			return doctorError("GCS storage selected but storage.gcs.bucket is empty", nil)
		}
		return doctorWarning("GCS storage is configured; run a provider-specific health check with ADC or mounted credentials")
	case "azure":
		if strings.TrimSpace(cfg.Storage.Azure.Container) == "" {
			return doctorError("Azure storage selected but storage.azure.container is empty", nil)
		}
		return doctorWarning("Azure storage is configured; run a provider-specific health check with account credentials")
	default:
		return doctorError(fmt.Sprintf("Unknown storage provider %q", provider), nil)
	}
}

func checkObservability(cfg *app.Config, configPath string) doctorCheckOutcome {
	if strings.TrimSpace(cfg.OTLPEndpoint) == "" {
		return doctorWarning("OTLP endpoint is not configured; traces/metrics will stay local unless exporters are added")
	}
	return doctorPass(fmt.Sprintf("OTLP endpoint configured: %s", cfg.OTLPEndpoint))
}

func checkTenancy(cfg *app.Config, configPath string) doctorCheckOutcome {
	if !cfg.MultiTenant.Enabled {
		return doctorWarning("Multi-tenant routing is disabled")
	}
	if strings.TrimSpace(cfg.MultiTenant.Resolver) == "" {
		return doctorError("Multi-tenant routing is enabled but resolver is empty", nil)
	}
	if cfg.MultiTenant.RequireIsolatedDB && len(cfg.MultiTenant.Tenants) == 0 {
		return doctorWarning("Multi-tenant isolation is required but no explicit tenants are configured")
	}
	return doctorPass(fmt.Sprintf("Multi-tenant routing enabled via %s resolver", cfg.MultiTenant.Resolver))
}

func checkRBAC(cfg *app.Config, configPath string) doctorCheckOutcome {
	path := strings.TrimSpace(cfg.AdminRBACPolicyFile)
	if path == "" {
		for _, candidate := range []string{
			"admin_rbac.csv", "config/admin_rbac.csv", "rbac/admin_rbac.csv",
			"rbac_policy.csv", "config/rbac_policy.csv", "rbac/rbac_policy.csv",
		} {
			if _, err := os.Stat(candidate); err == nil {
				return doctorPass(fmt.Sprintf("RBAC policy file found at %s", candidate))
			}
		}
		return doctorWarning("RBAC policy file is not configured; admin RBAC enforcer will not be enabled")
	}
	if _, err := os.Stat(path); err != nil {
		return doctorError("Configured RBAC policy file is not accessible", err)
	}
	return doctorPass(fmt.Sprintf("RBAC policy file found at %s", path))
}

func checkAudit(cfg *app.Config, configPath string) doctorCheckOutcome {
	if strings.TrimSpace(cfg.AdminPrefix) == "" {
		return doctorError("Admin prefix is empty; admin audit routes will not mount correctly", nil)
	}
	return doctorWarning("Admin audit logging is in-memory; configure an external/persistent audit sink before relying on it for compliance")
}

func doctorPass(message string) doctorCheckOutcome {
	return doctorCheckOutcome{status: doctorStatusPass, message: message}
}

func doctorWarning(message string) doctorCheckOutcome {
	return doctorCheckOutcome{status: doctorStatusWarning, message: message}
}

func doctorError(message string, err error) doctorCheckOutcome {
	if err == nil {
		err = fmt.Errorf("check failed")
	}
	return doctorCheckOutcome{status: doctorStatusError, message: message, err: err}
}

func doctorOutboxFlavor(raw string) outbox.Flavor {
	switch detectDBFlavor(raw) {
	case dbFlavorPostgres:
		return outbox.FlavorPostgres
	case dbFlavorMySQL:
		return outbox.FlavorMySQL
	default:
		return outbox.FlavorSQLite
	}
}
