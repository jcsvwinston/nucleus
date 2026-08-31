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

func TestAddCSRFExemptionInlineList(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "nucleus.yml")
	if err := os.WriteFile(cfg, []byte("csrf_enabled: true\ncsrf_exempt_paths: [\"/api/\", \"/notes\"]\nport: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := addCSRFExemption(cfg, "/books", &out); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(cfg)
	if !strings.Contains(string(raw), `csrf_exempt_paths: ["/api/", "/notes", "/books"]`) {
		t.Fatalf("inline list not extended:\n%s", raw)
	}

	// Idempotent: a second run must not duplicate the entry.
	out.Reset()
	if err := addCSRFExemption(cfg, "/books", &out); err != nil {
		t.Fatal(err)
	}
	raw2, _ := os.ReadFile(cfg)
	if !bytes.Equal(raw, raw2) {
		t.Fatalf("second run modified the file:\n%s", raw2)
	}
	if !strings.Contains(out.String(), "already exempt") {
		t.Fatalf("second run must report already-exempt, got: %s", out.String())
	}
}

func TestAddCSRFExemptionMissingKeyAppends(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "nucleus.yml")
	if err := os.WriteFile(cfg, []byte("port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := addCSRFExemption(cfg, "/books", &out); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(cfg)
	if !strings.Contains(string(raw), `csrf_exempt_paths: ["/books"]`) {
		t.Fatalf("missing key not appended:\n%s", raw)
	}
}

func TestAddCSRFExemptionBlockListRefusesToGuess(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "nucleus.yml")
	const body = "csrf_exempt_paths:\n  - \"/api/\"\nport: 8080\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := addCSRFExemption(cfg, "/books", &out); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(cfg)
	if string(raw) != body {
		t.Fatalf("block-style list must not be edited, got:\n%s", raw)
	}
	if !strings.Contains(out.String(), "add \"/books\" to it yourself") {
		t.Fatalf("must print the manual instruction, got: %s", out.String())
	}
}

func TestAppendPolicyRowsSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	policy := filepath.Join(dir, "rbac_policy.csv")
	if err := os.WriteFile(policy, []byte("p, anonymous, /books, read, allow\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := appendPolicyRows(policy, "/books", &out); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(policy)
	if strings.Count(string(raw), "p, anonymous, /books, read, allow") != 1 {
		t.Fatalf("existing row duplicated:\n%s", raw)
	}
	for _, row := range []string{
		"p, anonymous, /books, create, allow",
		"p, anonymous, /books/*, delete, allow",
	} {
		if !strings.Contains(string(raw), row) {
			t.Fatalf("missing row %q:\n%s", row, raw)
		}
	}
}
