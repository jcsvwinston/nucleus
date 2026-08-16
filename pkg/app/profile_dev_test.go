// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// DX-23 (DX audit 2026-08-16): a realistic production nucleus.yml names six
// backing services — PostgreSQL (+ replica), Redis for sessions, Redis for
// asynq jobs, S3 storage and SMTP — and NONE of them was optional by config:
// either all six are up or boot fails. `profile: dev` boots the SAME file
// with SQLite + in-memory sessions and jobs + local filesystem storage + the
// no-op mailer, so the smoke test runs without Docker.
package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// realisticProductionYML is the six-service shape the audit measured, with
// every endpoint pointing at a port nothing listens on: any code path that
// still tries to reach a backing service fails loudly.
const realisticProductionYML = `profile: dev
session_store: redis
session_redis_url: redis://127.0.0.1:1/0
redis_url: redis://127.0.0.1:1/0
jobs_provider: asynq
jobs_redis_url: redis://127.0.0.1:1/0
mail_driver: smtp
smtp_host: 127.0.0.1
smtp_port: 1
storage:
  provider: s3
  s3:
    endpoint: http://127.0.0.1:1
    bucket: demo
databases:
  default:
    url: postgres://nobody@127.0.0.1:1/demo
  replica:
    url: postgres://nobody@127.0.0.1:1/demo
`

func TestProfileDevBootsRealisticConfigWithoutBackingServices(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	if err := os.WriteFile(cfgPath, []byte(realisticProductionYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run from the temp dir so the profile's SQLite file and local storage
	// root land there, not in the repo.
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("profile: dev must load the realistic config, got: %v", err)
	}

	if cfg.SessionStore != "memory" {
		t.Errorf("session_store: want memory under profile dev, got %q", cfg.SessionStore)
	}
	if cfg.JobsProvider != "memory" {
		t.Errorf("jobs_provider: want memory under profile dev, got %q", cfg.JobsProvider)
	}
	if cfg.Storage.Provider != "local" {
		t.Errorf("storage.provider: want local under profile dev, got %q", cfg.Storage.Provider)
	}
	if cfg.MailDriver != "noop" {
		t.Errorf("mail_driver: want noop under profile dev, got %q", cfg.MailDriver)
	}
	defaultDB, ok := cfg.DatabaseByAlias("default")
	if !ok || !strings.HasPrefix(defaultDB.URL, "sqlite://") {
		t.Errorf("databases.default.url: want sqlite:// under profile dev, got %q", defaultDB.URL)
	}
	if _, replicaStays := cfg.Databases["replica"]; replicaStays {
		t.Errorf("databases.replica must be dropped under profile dev")
	}

	// The smoke half: the app must actually boot — no Redis, no S3, no SMTP,
	// no PostgreSQL are listening anywhere.
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("app.New under profile dev must boot without backing services: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.Shutdown(ctx)
}

// An unknown profile is a typo, not a silent fall-through to production
// behavior.
func TestProfileRejectsUnknownValue(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	if err := os.WriteFile(cfgPath, []byte("profile: prod\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(cfgPath)
	if err == nil {
		t.Fatal("unknown profile value must fail config load")
	}
	if !strings.Contains(err.Error(), "profile") {
		t.Errorf("error should name the profile key, got: %v", err)
	}
}
