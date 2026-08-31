package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/outbox"
	"github.com/jcsvwinston/nucleus/pkg/storage"
	asynqprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/asynq"
)

type doctorStatus string

const (
	doctorStatusPass    doctorStatus = "pass"
	doctorStatusWarning doctorStatus = "warning"
	doctorStatusError   doctorStatus = "error"
	// doctorStatusInfo reports an OPTIONAL subsystem that is simply not
	// enabled. It does not count against the overall verdict: a feature the
	// operator never turned on is not a degradation, and a doctor that
	// answers DEGRADED on a freshly generated scaffold trains people to
	// ignore it (NC-10/NF-14/GF-06). Warnings stay reserved for things that
	// are configured but incomplete or unverifiable; errors for broken.
	doctorStatusInfo doctorStatus = "info"
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
	Info          int            `json:"info"`
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
		if errors.Is(err, flag.ErrHelp) {
			// --help is a success, same as every other subcommand (NC-07).
			return nil
		}
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
		{name: "tasks", description: "Check the configured jobs provider (jobs_provider/jobs_redis_url); with asynq, inspect queue reachability over Redis", check: checkTasks},
		{name: "outbox", description: "Check outbox dispatcher and pending events", check: checkOutbox},
		{name: "storage", description: "Check storage backend connectivity and bucket access", check: func(cfg *app.Config, path string) doctorCheckOutcome {
			// NF-10: a live probe against S3/GCS/Azure only when the operator
			// explicitly targets this check — the full `doctor` run stays
			// offline so its other checks are not held hostage by a slow or
			// unreachable remote.
			return checkStorage(cfg, path, *checkName == "storage")
		}},
		{name: "observability", description: "Check OpenTelemetry exporters and metrics", check: checkObservability},
		{name: "tenancy", description: "Check multi-tenant configuration and isolation", check: checkTenancy},
		{name: "rbac", description: "Check RBAC policies and Casbin enforcer", check: checkRBAC},
		{name: "security", description: "Check for high-risk security misconfiguration (CORS, trusted proxies, signing key, CSRF)", check: checkSecurity},
		{name: "auth", description: "Check the authentication chain: backend order, per-backend configuration, break-glass path", check: checkAuth},
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
		case outcome.status == doctorStatusInfo:
			// Optional subsystem, deliberately off: reported, never counted
			// against the verdict.
			report.Info++
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
		// The verdict belongs to the REPORT, not to the renderer. This
		// branch used to return nil, so the same unhealthy report exited 1
		// as text and 0 as JSON — and the one mode that never failed was
		// exactly the one CI consumes (QCD-CLI-7). The JSON on stdout stays
		// untouched and parseable; the failure travels in the exit code,
		// which is where a pipeline reads it.
		return doctorVerdict(report)
	}

	fmt.Fprintf(stdout, "\nDoctor Report (%s)\n", report.Timestamp.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(stdout, "Overall Status: %s\n", strings.ToUpper(report.OverallStatus))
	fmt.Fprintf(stdout, "Total Checks: %d | Passed: %d | Failed: %d | Warnings: %d | Not configured: %d\n\n",
		report.TotalChecks, report.Passed, report.Failed, report.Warnings, report.Info)

	for _, result := range report.Results {
		statusSymbol := "✓"
		if result.Status == string(doctorStatusInfo) {
			statusSymbol = "-"
		}
		if result.Status == string(doctorStatusWarning) {
			statusSymbol = "!"
		}
		if result.Status == string(doctorStatusError) {
			statusSymbol = "✗"
		}
		fmt.Fprintf(stdout, "%s %-20s %s\n", statusSymbol, result.Name, result.Message)
	}

	fmt.Fprintln(stdout)

	return doctorVerdict(report)
}

// doctorVerdict turns a finished report into the command's exit status, so
// both renderers reach the same conclusion from the same data.
func doctorVerdict(report doctorReport) error {
	if report.OverallStatus == "unhealthy" {
		return fmt.Errorf("doctor checks failed")
	}
	return nil
}

// checkTasks inspects the JOBS runtime the application actually uses:
// jobs_provider + jobs_redis_url (pkg/nucleus/jobs.go). It used to look
// only at redis_url — the key for sessions/rate limiting — so a correctly
// configured asynq deployment was reported as "Redis is not configured"
// (NF-3). With asynq configured it now performs a real inspection through
// the same provider the runtime uses.
func checkTasks(cfg *app.Config, configPath string) doctorCheckOutcome {
	provider := strings.ToLower(strings.TrimSpace(cfg.JobsProvider))
	jobsRedis := strings.TrimSpace(cfg.JobsRedisURL)

	switch provider {
	case "", "memory":
		if jobsRedis != "" {
			return doctorWarning("jobs_redis_url is set but jobs_provider is not \"asynq\"; the URL is unused (set jobs_provider: asynq to use it)")
		}
		return doctorInfo("Jobs use the in-process memory provider (jobs_provider: memory); no queue backend to inspect. Configure jobs_provider: asynq + jobs_redis_url for a durable queue")
	case "asynq":
		if jobsRedis == "" {
			return doctorError("jobs_provider is \"asynq\" but jobs_redis_url is empty; the jobs runtime will refuse to start", nil)
		}
		snapshot := asynqprovider.InspectRuntime(jobsRedis)
		if !snapshot.Enabled {
			return doctorError("Asynq jobs are configured but the Redis inspection failed", fmt.Errorf("%s", snapshot.Reason))
		}
		return doctorPass(fmt.Sprintf("Asynq reachable via jobs_redis_url; queues=%d pending=%d active=%d retry=%d",
			len(snapshot.Queues), snapshot.TotalPending, snapshot.TotalActive, snapshot.TotalRetry))
	default:
		return doctorError(fmt.Sprintf("unknown jobs_provider %q (supported: memory, asynq)", cfg.JobsProvider), nil)
	}
}

func checkOutbox(cfg *app.Config, configPath string) doctorCheckOutcome {
	if !cfg.Outbox.Enabled {
		return doctorInfo("Outbox is not enabled (optional; enable with outbox.enabled: true)")
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
	// Hand the raw URL to the outbox so its own flavor resolution runs:
	// mapping unsupported dialects (mssql, oracle) to a fallback flavor
	// here would hide the runtime's fail-fast from doctor (NU6-3).
	snapshot := outbox.InspectRuntime(sqlDB, outbox.Config{
		TableName:   cfg.Outbox.TableName,
		DatabaseURL: dbCfg.URL,
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

func checkStorage(cfg *app.Config, configPath string, live bool) doctorCheckOutcome {
	// storage.provider is the only source — the legacy storage_driver key
	// was removed in v0.12.0 (DEP-2026-005).
	provider := strings.ToLower(strings.TrimSpace(cfg.Storage.Provider))
	if provider == "" {
		return doctorError("Storage provider is not configured", nil)
	}
	switch provider {
	case "local":
		path := strings.TrimSpace(cfg.Storage.Local.Path)
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
	case "gcs":
		if strings.TrimSpace(cfg.Storage.GCS.Bucket) == "" {
			return doctorError("GCS storage selected but storage.gcs.bucket is empty", nil)
		}
	case "azure":
		if strings.TrimSpace(cfg.Storage.Azure.Container) == "" {
			return doctorError("Azure storage selected but storage.azure.container is empty", nil)
		}
	default:
		return doctorError(fmt.Sprintf("Unknown storage provider %q", provider), nil)
	}

	// Remote provider with the static config in order. Without --check
	// storage the full run stays offline (and says how to go further);
	// with it, probe for real (NF-10: doctor used to delegate to "a
	// provider-specific health check" that did not exist anywhere).
	if !live {
		return doctorWarning(fmt.Sprintf("%s storage is configured (config only); run `nucleus doctor --check storage` for a live connectivity probe with the deployment credentials", strings.ToUpper(provider)))
	}
	return probeStorageLive(cfg, provider)
}

// probeStorageLive builds the real store from the effective configuration —
// credentials, endpoint, TLS, everything the runtime would use — and
// performs one cheap authenticated call (List with Limit 1) against the
// bucket/container. This is the same code path `app.New` wires, so a PASS
// here means the running application can reach its storage too.
func probeStorageLive(cfg *app.Config, provider string) doctorCheckOutcome {
	storCfg := cfg.ToStorageConfig()
	// The probe wants the dependency's true answer, not the breaker's memory
	// of previous failures — and a one-shot CLI has no breaker history
	// anyway; disabling it keeps the error message the provider's own.
	storCfg.CircuitBreaker.Enabled = false

	store, err := storage.New(storCfg, nil)
	if err != nil {
		return doctorError(fmt.Sprintf("%s storage configuration was rejected building the store", strings.ToUpper(provider)), err)
	}
	defer func() { _ = store.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := store.List(ctx, storage.ListOptions{Limit: 1}); err != nil {
		return doctorError(fmt.Sprintf("%s storage live probe failed (List with the configured credentials)", strings.ToUpper(provider)), err)
	}
	return doctorPass(fmt.Sprintf("%s storage live probe succeeded: bucket/container is reachable with the configured credentials", strings.ToUpper(provider)))
}

func checkObservability(cfg *app.Config, configPath string) doctorCheckOutcome {
	if strings.TrimSpace(cfg.OTLPEndpoint) == "" {
		return doctorInfo("OTLP endpoint is not configured (optional); traces/metrics stay local unless exporters are added")
	}
	return doctorPass(fmt.Sprintf("OTLP endpoint configured: %s", cfg.OTLPEndpoint))
}

func checkTenancy(cfg *app.Config, configPath string) doctorCheckOutcome {
	if !cfg.MultiTenant.Enabled {
		return doctorInfo("Multi-tenant routing is not enabled (optional)")
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
	// rbac_policy_file is the only source — the deprecated
	// admin_rbac_policy_file alias was removed in v0.12.0 (DEP-2026-004).
	path := strings.TrimSpace(cfg.RBACPolicyFile)
	if path == "" {
		for _, candidate := range []string{
			"admin_rbac.csv", "config/admin_rbac.csv", "rbac/admin_rbac.csv",
			"rbac_policy.csv", "config/rbac_policy.csv", "rbac/rbac_policy.csv",
		} {
			if _, err := os.Stat(candidate); err == nil {
				return doctorPass(fmt.Sprintf("RBAC policy file found at %s", candidate))
			}
		}
		return doctorWarning("RBAC policy file is not configured; the RBAC enforcer will only serve bootstrap routes")
	}
	if _, err := os.Stat(path); err != nil {
		return doctorError("Configured RBAC policy file is not accessible", err)
	}
	return doctorPass(fmt.Sprintf("RBAC policy file found at %s", path))
}

func doctorPass(message string) doctorCheckOutcome {
	return doctorCheckOutcome{status: doctorStatusPass, message: message}
}

func doctorWarning(message string) doctorCheckOutcome {
	return doctorCheckOutcome{status: doctorStatusWarning, message: message}
}

func doctorInfo(message string) doctorCheckOutcome {
	return doctorCheckOutcome{status: doctorStatusInfo, message: message}
}

func doctorError(message string, err error) doctorCheckOutcome {
	if err == nil {
		err = fmt.Errorf("check failed")
	}
	return doctorCheckOutcome{status: doctorStatusError, message: message, err: err}
}
