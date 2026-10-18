// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tasks check reads the JOBS configuration (jobs_provider +
// jobs_redis_url), not redis_url (NF-3), and this binary links the asynq
// provider (providers.go), so the check inspects Redis through it. An asynq
// deployment whose Redis is unreachable must NOT be reported as "Redis is
// not configured" or as a provider that is not linked — it is configured,
// linked and broken, which is an error that names the inspection.
func TestRun_DoctorTasksInspectsAsynqThroughTheLinkedProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nucleus.yml")
	// 127.0.0.1:1 refuses connections immediately: configured-but-down.
	const yaml = "jobs_provider: asynq\njobs_redis_url: redis://127.0.0.1:1/0\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"doctor", "--check", "tasks", "--config", path, "--json"}, &out, &errOut); code == 0 {
		t.Fatalf("an asynq config with unreachable Redis must fail the tasks check\n%s%s", out.String(), errOut.String())
	}

	var report struct {
		OverallStatus string `json:"overall_status"`
		Failed        int    `json:"failed"`
		Results       []struct {
			Message string `json:"message"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode doctor report: %v\n%s", err, out.String())
	}
	if report.OverallStatus != "unhealthy" || report.Failed != 1 || len(report.Results) != 1 {
		t.Fatalf("expected unhealthy report with one failure, got %+v", report)
	}
	msg := strings.ToLower(report.Results[0].Message)
	for _, banned := range []string{"not configured", "nucleus add", "unknown jobs_provider"} {
		if strings.Contains(msg, banned) {
			t.Fatalf("configured-but-unreachable must not read as %q: %q", banned, report.Results[0].Message)
		}
	}
	if !strings.Contains(msg, "inspection failed") {
		t.Fatalf("the failure does not name the inspection: %q", report.Results[0].Message)
	}
}
