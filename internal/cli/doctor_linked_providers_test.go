// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/providerns"
)

// NU-132: `doctor`, `health` and `config print` used to validate the
// configuration with the registries of the `nucleus` binary, which links no
// provider module — so the block `nucleus add sentry` had just written was
// refused as an unknown key by the very next command, against a file the
// application booted on. The commands now read the project's imports and
// accept a provider's subtree exactly when main.go imports the provider.
func TestDoctorAcceptsTheBlockNucleusAddWrote(t *testing.T) {
	t.Cleanup(func() { providerns.SetLinked(providerns.Declared{}) })
	prev := goGet
	goGet = func(string, string, io.Writer, io.Writer) error { return nil }
	t.Cleanup(func() { goGet = prev })

	dir := recipeProject(t, starterConfig)
	if out, err := addOut(t, "sentry", "--dir", dir); err != nil {
		t.Fatalf("nucleus add sentry: %v\n%s", err, out)
	}
	cfgPath := filepath.Join(dir, "nucleus.yml")
	if cfg := readFile(t, cfgPath); !strings.Contains(cfg, "interceptors:\n  sentry:") || !strings.Contains(cfg, "http_interceptors: [sentry]") {
		t.Fatalf("nucleus add sentry did not write the block this test is about:\n%s", cfg)
	}

	// The loader every command goes through.
	if _, err := loadConfig(cfgPath); err != nil {
		t.Fatalf("the configuration `nucleus add sentry` wrote does not load from the CLI: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := runDoctor([]string{"--config", cfgPath}, strings.NewReader(""), &stdout, &stderr)
	if err != nil && strings.Contains(err.Error(), "unknown configuration key") {
		t.Fatalf("doctor refused the block nucleus add wrote: %v", err)
	}
	if strings.Contains(stdout.String()+stderr.String(), "unknown configuration key") {
		t.Fatalf("doctor reported the block nucleus add wrote as unknown:\n%s%s", stdout.String(), stderr.String())
	}

	// `config print` validates on the builder's loader: same file, same
	// verdict.
	stdout.Reset()
	stderr.Reset()
	if err := runConfigPrint([]string{"--config", cfgPath}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("config print: %v\n%s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "will not boot") || strings.Contains(stderr.String(), "unknown configuration key") {
		t.Fatalf("config print warned about the block nucleus add wrote:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "interceptors.sentry.sample_rate") {
		t.Fatalf("config print must print the provider's keys with the rest, got:\n%s", stdout.String())
	}
}

// The exemption follows the import, not the namespace: the same block in a
// project whose main.go does not import the provider is still refused — and
// the refusal names the import to add, not a did-you-mean.
func TestDoctorRefusesAProviderBlockTheProjectDoesNotImport(t *testing.T) {
	t.Cleanup(func() { providerns.SetLinked(providerns.Declared{}) })
	dir := recipeProject(t, starterConfig+"\nhttp_interceptors: [sentry]\ninterceptors:\n  sentry:\n    dsn: \"\"\n")
	cfgPath := filepath.Join(dir, "nucleus.yml")

	var stdout, stderr bytes.Buffer
	err := runDoctor([]string{"--config", cfgPath}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatalf("doctor accepted interceptors.sentry.* in a project that does not import the provider:\n%s", stdout.String())
	}
	for _, want := range []string{
		"interceptors.sentry.dsn (not installed: `nucleus add sentry`)",
		"the project's main.go does not import it",
		"`_ \"github.com/jcsvwinston/nucleus/providers/errors-sentry\"`",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must say %q, got:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("a provider key is not a typo of a schema key:\n%v", err)
	}
}

// A backend published as a module (ldap) under auth.*: the project imports
// it, lists it in the chain, configures it — and the CLI, which does not
// link it, accepts the file the way the application does.
func TestDoctorAcceptsALinkedAuthBackend(t *testing.T) {
	t.Cleanup(func() { providerns.SetLinked(providerns.Declared{}) })
	dir := recipeProject(t, starterConfig+"\nauth_backends: [ldap, local]\nauth:\n  ldap:\n    url: ldaps://directory.example.com\n    base_dn: dc=example,dc=com\n")
	mainPath := filepath.Join(dir, "main.go")
	if _, err := ensureBlankImport(mainPath, "github.com/jcsvwinston/nucleus/providers/ldap"); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "nucleus.yml")
	if _, err := loadConfig(cfgPath); err != nil {
		t.Fatalf("a linked ldap backend's block does not load from the CLI: %v", err)
	}

	// Remove the import: the same file is refused again.
	src := readFile(t, mainPath)
	if err := os.WriteFile(mainPath, []byte(strings.ReplaceAll(src, "github.com/jcsvwinston/nucleus/providers/ldap", "github.com/jcsvwinston/nucleus/pkg/nucleus")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(cfgPath); err == nil || !strings.Contains(err.Error(), "auth.ldap.url (not installed: `nucleus add ldap`)") {
		t.Fatalf("without the import the block must be refused naming the install, got %v", err)
	}
}
