package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An unconfigured OPTIONAL subsystem reports "info" and does not degrade
// the overall verdict (NC-10/NF-14/GF-06): a fresh scaffold with jobs on
// the memory provider, outbox off, no OTLP and no tenancy is a healthy
// app, not a degraded one. The message must stay honest — no placeholder
// passes either.
func TestRunDoctorOptionalSubsystemOffIsInfoNotDegraded(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	if err := runDoctor([]string{"--check", "tasks", "--json"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("doctor should exit 0 when an optional subsystem is off: %v", err)
	}

	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode doctor report: %v", err)
	}
	if report.OverallStatus != "healthy" {
		t.Fatalf("an off-by-default optional subsystem must not degrade the verdict, got %q", report.OverallStatus)
	}
	if report.Info != 1 || report.Warnings != 0 || report.Passed != 0 {
		t.Fatalf("expected one info result (no warning, no placeholder pass), got passed=%d warnings=%d info=%d", report.Passed, report.Warnings, report.Info)
	}
	if len(report.Results) != 1 || report.Results[0].Status != "info" {
		t.Fatalf("expected info result, got %#v", report.Results)
	}
	if strings.Contains(strings.ToLower(report.Results[0].Message), "placeholder") {
		t.Fatalf("doctor message must be honest, got %q", report.Results[0].Message)
	}
}

// The tasks check resolves a provider that is not memory or sql through the
// pkg/tasks registry, the way the runtime does. This package is in the
// framework module, which links no provider module, so `jobs_provider:
// asynq` here is the unlinked case: the check fails naming the module and
// `nucleus add tasks-asynq` — not "not configured", which it is, and not
// "unknown", which it is not (DEP-2026-020). The linked case, where the
// inspection reaches Redis, is tested from cmd/nucleus, the binary that
// links the provider.
func TestRunDoctorTasksUnlinkedProviderNamesNucleusAdd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nucleus.yml")
	const yaml = "jobs_provider: asynq\njobs_redis_url: redis://127.0.0.1:1/0\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out, errOut bytes.Buffer
	err := runDoctor([]string{"--check", "tasks", "--config", path, "--json"}, strings.NewReader(""), &out, &errOut)
	if err == nil {
		t.Fatal("an asynq config in a binary that does not link the provider must fail the tasks check")
	}

	var report doctorReport
	if jsonErr := json.Unmarshal(out.Bytes(), &report); jsonErr != nil {
		t.Fatalf("decode doctor report: %v", jsonErr)
	}
	if report.OverallStatus != "unhealthy" || report.Failed != 1 {
		t.Fatalf("expected unhealthy report with one failure, got %q failed=%d", report.OverallStatus, report.Failed)
	}
	msg := report.Results[0].Message
	for _, want := range []string{"nucleus add tasks-asynq", "github.com/jcsvwinston/nucleus/providers/tasks-asynq"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the failure does not say %q: %q", want, msg)
		}
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "not configured") || strings.Contains(lower, "unknown jobs_provider") {
		t.Fatalf("a published provider that is not linked is neither unconfigured nor unknown: %q", msg)
	}
}

// jobs_redis_url set while the provider stays "memory" is a half-applied
// configuration — exactly what warnings are for.
func TestRunDoctorTasksWarnsOnUnusedJobsRedisURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nucleus.yml")
	const yaml = "jobs_redis_url: redis://127.0.0.1:6399/0\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out, errOut bytes.Buffer
	if err := runDoctor([]string{"--check", "tasks", "--config", path, "--json"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("a warning-only report still exits 0: %v", err)
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode doctor report: %v", err)
	}
	if report.Warnings != 1 || report.OverallStatus != "degraded" {
		t.Fatalf("expected one warning (degraded), got %#v", report)
	}
}

func TestRunDoctorRejectsUnknownCheck(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	err := runDoctor([]string{"--check", "missing"}, strings.NewReader(""), &out, &errOut)
	if err == nil {
		t.Fatal("expected unknown check error")
	}
}

// A freshly generated config directory (storage local, everything optional
// off) reports OK overall — the first `nucleus doctor` a new user runs must
// not answer DEGRADED for features they never enabled (GF-06). The full
// check set runs against a scaffold-shaped config written into a temp dir
// with the scaffold's rbac_policy.csv and storage path present.
func TestRunDoctorFreshScaffoldIsHealthy(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	yaml := "database_default: default\n" +
		"databases:\n  default:\n    url: sqlite://" + filepath.Join(dir, "app.db") + "\n" +
		"env: development\n" +
		"rbac_policy_file: " + filepath.Join(dir, "rbac_policy.csv") + "\n" +
		"storage:\n  provider: local\n  local:\n    path: " + dir + "\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rbac_policy.csv"), []byte("p, anonymous, /healthz, read, allow\n"), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	var out, errOut bytes.Buffer
	if err := runDoctor([]string{"--config", cfgPath, "--json"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("doctor on a fresh scaffold-shaped config must exit 0: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode doctor report: %v", err)
	}
	if report.OverallStatus != "healthy" {
		t.Fatalf("fresh scaffold must be healthy, got %q\n%s", report.OverallStatus, out.String())
	}
	if report.Failed != 0 || report.Warnings != 0 {
		t.Fatalf("fresh scaffold must have no failures or warnings, got %#v", report)
	}
	if report.Info == 0 {
		t.Fatal("optional-off subsystems must still be reported (as info), got none")
	}
}
