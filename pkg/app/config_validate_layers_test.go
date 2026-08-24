// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Regression tests for the "same file, two verdicts" gap of ADR-010 §2
// layers 3–4: validateSemantics/validateReferential ran only on the
// pkg/nucleus surfaces, so `log_level: verbose` failed `go run .` but
// sailed through every `nucleus <cmd>` (whose LoadConfig skipped both
// layers). The layers now live in pkg/app and LoadConfig applies them on
// the EFFECTIVE config — post-profile, post-normalisation, the same order
// the builder uses.
package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nucleus.yml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfig_AppliesSemanticLayer(t *testing.T) {
	path := writeConfigFile(t, "log_level: verbose\n")
	_, err := LoadConfig(path)
	if !errors.Is(err, ErrInvalidConfigValue) {
		t.Fatalf("the CLI path must reject what the builder rejects: want ErrInvalidConfigValue, got %v", err)
	}
	for _, want := range []string{"log_level", "verbose"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must name %q, got %v", want, err)
		}
	}
}

func TestLoadConfig_AppliesReferentialLayer(t *testing.T) {
	path := writeConfigFile(t, "mail_driver: smtp\n")
	_, err := LoadConfig(path)
	if !errors.Is(err, ErrInvalidConfigReference) {
		t.Fatalf("want ErrInvalidConfigReference for smtp without smtp_host, got %v", err)
	}
	if !strings.Contains(err.Error(), "smtp_host") {
		t.Errorf("error must name smtp_host, got %v", err)
	}
}

// The layers validate the EFFECTIVE config: `profile: dev` swaps backing
// services before validation, so a production config that the profile
// rewrites must still load.
func TestLoadConfig_ValidatesAfterProfile(t *testing.T) {
	path := writeConfigFile(t, "profile: dev\nsession_store: redis\nlog_level: info\n")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("profile: dev must produce a valid effective config, got %v", err)
	}
	if cfg.SessionStore != "memory" {
		t.Fatalf("profile: dev should have swapped session_store to memory, got %q", cfg.SessionStore)
	}
}

// A valid config keeps loading — the layers only reject explicitly wrong
// values (empty/zero mean "use the default").
func TestLoadConfig_ValidConfigStillLoads(t *testing.T) {
	path := writeConfigFile(t, "log_level: info\nport: 8080\n")
	if _, err := LoadConfig(path); err != nil {
		t.Fatalf("valid config must load, got %v", err)
	}
}

// The credential-source class, closed end-to-end: the shapes the README
// promises for sensitive values must LOAD through the real config path —
// both the full source shape and the legacy plain string.
func TestLoadConfig_CredentialSourceShapesBind(t *testing.T) {
	path := writeConfigFile(t, `
storage:
  provider: s3
  s3:
    bucket: b
    access_key_id:
      env_var: AWS_ACCESS_KEY_ID
    secret_access_key: literal-secret
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("the documented credential shape must load, got %v", err)
	}
	if cfg.Storage.S3.AccessKeyID.EnvVar != "AWS_ACCESS_KEY_ID" {
		t.Fatalf("env_var source did not bind: %+v", cfg.Storage.S3.AccessKeyID)
	}
	// Legacy scalar promotes to {Value: …} via the decode hook.
	if cfg.Storage.S3.SecretAccessKey.Value != "literal-secret" {
		t.Fatalf("plain-string credential did not promote to Value: %+v", cfg.Storage.S3.SecretAccessKey)
	}
}
