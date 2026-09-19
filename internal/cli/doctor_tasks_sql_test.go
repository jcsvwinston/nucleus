// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `jobs_provider: sql` is accepted by the configuration validator and boots,
// but doctor's tasks check fell through to its default branch and answered
// `unknown jobs_provider "sql" (supported: memory, asynq)`. A deployment on the
// durable queue was told by its own diagnostic that its configuration was
// broken — and doctor is the thing an operator runs to find out whether it is.
func TestDoctorAcceptsTheSQLJobsProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nucleus.yml")
	yaml := "jobs_provider: sql\njobs_table: some_jobs\ndatabases:\n  default:\n    url: sqlite://" +
		filepath.Join(dir, "app.db") + "\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out, errOut bytes.Buffer
	if err := runDoctor([]string{"--check", "tasks", "--config", path, "--json"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("a valid sql jobs configuration must pass the tasks check: %v\n%s%s", err, out.String(), errOut.String())
	}
	body := out.String()
	if strings.Contains(body, "unknown jobs_provider") {
		t.Fatalf("doctor still refuses the sql provider: %s", body)
	}
	// The table it names has to be the configured one, not the default: an
	// operator reads this line to confirm doctor looked at their deployment.
	if !strings.Contains(body, "some_jobs") {
		t.Fatalf("the tasks check does not name the configured table: %s", body)
	}
}

// Redis plays no part in the sql provider, so a jobs_redis_url left behind
// from an asynq deployment is dead configuration. Doctor says so rather than
// letting an operator believe the queue is using it.
func TestDoctorWarnsAboutARedisURLTheSQLProviderIgnores(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nucleus.yml")
	yaml := "jobs_provider: sql\njobs_redis_url: redis://127.0.0.1:6379/0\ndatabases:\n  default:\n    url: sqlite://" +
		filepath.Join(dir, "app.db") + "\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out, errOut bytes.Buffer
	_ = runDoctor([]string{"--check", "tasks", "--config", path, "--json"}, strings.NewReader(""), &out, &errOut)
	body := out.String()
	if !strings.Contains(body, "jobs_redis_url") || !strings.Contains(body, "unused") {
		t.Fatalf("the tasks check does not report the unused Redis URL: %s", body)
	}
}

// The queue lives in the application's own database, so a database doctor
// cannot open is a tasks failure, not just a database one: the deployment will
// not have a queue. (There is always a default database in the effective
// config — a SQLite file — so the way to be without one is to name a database
// that cannot be opened, which is also the realistic way to get here.)
func TestDoctorFailsTheSQLProviderWhenTheDatabaseCannotBeOpened(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nucleus.yml")
	// The postgres driver is a sibling module and is not linked into this
	// binary, which is exactly what an operator hits who configures postgres
	// without running `nucleus add postgres`.
	yaml := "jobs_provider: sql\ndatabases:\n  default:\n    url: postgres://127.0.0.1:1/nucleus?sslmode=disable\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out, errOut bytes.Buffer
	if err := runDoctor([]string{"--check", "tasks", "--config", path, "--json"}, strings.NewReader(""), &out, &errOut); err == nil {
		t.Fatalf("sql jobs on a database that cannot be opened must fail the tasks check: %s%s", out.String(), errOut.String())
	}
	if strings.Contains(out.String(), "unknown jobs_provider") {
		t.Fatalf("the failure must name the database, not the provider: %s", out.String())
	}
}
