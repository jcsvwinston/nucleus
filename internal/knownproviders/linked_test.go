// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package knownproviders

import (
	"os"
	"path/filepath"
	"testing"
)

// NU-132: the CLI learns which providers a project links by reading its
// imports — the blank import `nucleus add` writes — not by consulting the
// registries of the `nucleus` binary, which links no provider module.
func TestLinkedInReadsTheProjectImports(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", `package main

import (
	"log"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"

	_ "github.com/jcsvwinston/nucleus/drivers/sqlite"
	_ "github.com/jcsvwinston/nucleus/providers/errors-sentry"
	_ "github.com/jcsvwinston/nucleus/providers/storage-s3"
)

func main() { log.Fatal(nucleus.Run(nucleus.App{})) }
`)
	// A second file of the package counts too: nucleus add --into may
	// have written the import there.
	write("wiring.go", "package main\n\nimport _ \"github.com/jcsvwinston/nucleus/providers/ldap\"\n")
	// A test file does not: the test binary links more than the server.
	write("main_test.go", "package main\n\nimport _ \"github.com/jcsvwinston/nucleus/providers/storage-gcs\"\n")

	got := names(LinkedIn(dir))
	want := []string{"sqlite", "s3", "ldap", "sentry"}
	if len(got) != len(want) {
		t.Fatalf("LinkedIn = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("LinkedIn = %v, want %v (catalog order)", got, want)
		}
	}
}

// A project laid out as cmd/<name>/main.go is read when the root has no
// Go files — the same fallback `nucleus add` uses to place the import.
func TestLinkedInFallsBackToCmdMain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cmd", "server", "main.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\n\nimport _ \"github.com/jcsvwinston/nucleus/providers/ldap\"\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := names(LinkedIn(dir)); len(got) != 1 || got[0] != "ldap" {
		t.Fatalf("LinkedIn over cmd/server/main.go = %v, want [ldap]", got)
	}
}

// No Go files, an unreadable directory, a file that does not parse: nothing
// is linked and nothing fails — the registries alone decide, as before.
func TestLinkedInIsQuietWhenThereIsNothingToRead(t *testing.T) {
	if got := LinkedIn(t.TempDir()); len(got) != 0 {
		t.Fatalf("an empty directory links %v", got)
	}
	if got := LinkedIn(filepath.Join(t.TempDir(), "missing")); len(got) != 0 {
		t.Fatalf("a missing directory links %v", got)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package main\n\nimport _ \"github.com/jcsvwinston/nucleus/providers/ldap\"\n\nfunc main() {"), 0o644); err != nil {
		t.Fatal(err)
	}
	// ImportsOnly parses the import block before the body that is broken.
	if got := names(LinkedIn(dir)); len(got) != 1 || got[0] != "ldap" {
		t.Fatalf("a file whose body does not parse still has readable imports; got %v", got)
	}
}

// Only entries shipped as modules can be "not linked": a core entry
// registers through the framework module and never needs the import to be
// read, and an import outside the catalog is not a provider.
func TestLinkedByImportsIgnoresCoreEntriesAndStrangers(t *testing.T) {
	got := names(LinkedByImports(map[string]bool{
		RepoModule + "/pkg/auth/federated/oidc": true, // core: ships with the framework
		"github.com/someone/else/provider":      true,
		RepoModule + "/providers/auth-saml":     true,
	}))
	if len(got) != 1 || got[0] != "saml" {
		t.Fatalf("LinkedByImports = %v, want [saml]", got)
	}
}

func names(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}
