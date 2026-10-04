// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// The recipes (ADR-035): what `nucleus add` writes for an entry an import
// does not wire. These drive the real command on a project laid out the way
// the api starter lays it out, and read the files it leaves.

// recipeProject writes a module root with the scaffold's chain in main.go
// and, when config is non-empty, a nucleus.yml holding it.
func recipeProject(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.26\n")
	mustWrite(t, filepath.Join(dir, "main.go"), chainMainGo)
	if config != "" {
		mustWrite(t, filepath.Join(dir, "nucleus.yml"), config)
	}
	return dir
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func addOut(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runAdd(args, nil, &out, io.Discard)
	return out.String(), err
}

const starterConfig = "database_default: default\ndatabases:\n  default:\n    url: sqlite://app.db\nport: 8080\n"

// The recipe writes the chain call and the configuration block, and a second
// run changes nothing — byte for byte — and says so.
func TestAddRecipe_WritesAndIsIdempotent(t *testing.T) {
	dir := recipeProject(t, starterConfig)
	out, err := addOut(t, "oidc", "apikeys", "sql-queue", "--dir", dir)
	if err != nil {
		t.Fatalf("nucleus add: %v\n%s", err, out)
	}
	main := readFile(t, filepath.Join(dir, "main.go"))
	for _, want := range []string{
		`_ "github.com/jcsvwinston/nucleus/pkg/auth/federated/oidc"`,
		"Mount(nucleus.FederatedSignIn()).",
		"WithAPIKeys().",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("main.go lacks %s:\n%s", want, main)
		}
	}
	// Spliced before the terminal call, so the chain still ends in Start.
	if strings.Index(main, "WithAPIKeys()") > strings.Index(main, "Start()") {
		t.Errorf("the call went after Start():\n%s", main)
	}
	config := readFile(t, filepath.Join(dir, "nucleus.yml"))
	for _, want := range []string{"public_base_url:", "auth_federated:", "provider: oidc", "jobs_provider: sql"} {
		if !strings.Contains(config, want) {
			t.Errorf("nucleus.yml lacks %s:\n%s", want, config)
		}
	}
	if !strings.HasPrefix(config, starterConfig) {
		t.Errorf("the recipe rewrote what was there:\n%s", config)
	}
	for _, want := range []string{"serves: GET /auth/corp/start", "nucleus apikey create"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}

	again, err := addOut(t, "oidc", "apikeys", "sql-queue", "--dir", dir)
	if err != nil {
		t.Fatalf("second nucleus add: %v", err)
	}
	if readFile(t, filepath.Join(dir, "main.go")) != main || readFile(t, filepath.Join(dir, "nucleus.yml")) != config {
		t.Fatal("a second nucleus add changed the project")
	}
	for _, want := range []string{"already wired in main.go: .Mount(nucleus.FederatedSignIn())", "already wired in main.go: .WithAPIKeys()", "already configured in nucleus.yml"} {
		if !strings.Contains(again, want) {
			t.Errorf("second run does not say %q:\n%s", want, again)
		}
	}
}

// A dry run describes the recipe and touches nothing.
func TestAddRecipe_DryRunWritesNothing(t *testing.T) {
	dir := recipeProject(t, starterConfig)
	out, err := addOut(t, "oidc", "apikeys", "sql-queue", "--dry-run", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dir, "main.go")) != chainMainGo || readFile(t, filepath.Join(dir, "nucleus.yml")) != starterConfig {
		t.Fatal("--dry-run modified the project")
	}
	for _, want := range []string{
		"would wire: .Mount(nucleus.FederatedSignIn())",
		"would wire: .WithAPIKeys()",
		"would write to nucleus.yml:",
		"    jobs_provider: sql",
		"serves: GET /auth/corp/start",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run does not say %q:\n%s", want, out)
		}
	}
}

// A key the person has already set is theirs: the block is printed to merge
// by hand, and the file is left exactly as it was — including when only one
// of the block's keys is set (an ldap directory's `auth:` subtree).
func TestAddRecipe_NeverRewritesASetKey(t *testing.T) {
	for _, c := range []struct {
		name, entry, config, set string
	}{
		{"the selecting key", "sql-queue", starterConfig + "jobs_provider: memory\n", "jobs_provider"},
		{"one key of several", "oidc", starterConfig + "auth:\n  ldap:\n    url: ldap://dir:389\n", "auth"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := recipeProject(t, c.config)
			out, err := addOut(t, c.entry, "--dir", dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, filepath.Join(dir, "nucleus.yml")); got != c.config {
				t.Fatalf("nucleus.yml changed:\n%s", got)
			}
			if !strings.Contains(out, "already sets "+c.set) && !strings.Contains(out, "already configured in nucleus.yml: "+c.set) {
				t.Errorf("output does not name %s as already set:\n%s", c.set, out)
			}
		})
	}
	// The partial case prints the block to merge.
	dir := recipeProject(t, starterConfig+"auth:\n  ldap:\n    url: ldap://dir:389\n")
	out, _ := addOut(t, "oidc", "--dir", dir)
	if !strings.Contains(out, "merge this block by hand") || !strings.Contains(out, "    auth_federated:") {
		t.Errorf("the block to merge is not printed:\n%s", out)
	}
}

// No configuration file: the block is printed, nothing is created.
func TestAddRecipe_NoConfigFilePrintsTheBlock(t *testing.T) {
	dir := recipeProject(t, "")
	out, err := addOut(t, "sql-queue", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "nucleus.yml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("nucleus add created a configuration file: %v", err)
	}
	if !strings.Contains(out, "no nucleus.yml") || !strings.Contains(out, "    jobs_provider: sql") {
		t.Errorf("the block is not printed:\n%s", out)
	}
	// --config points elsewhere.
	mustWrite(t, filepath.Join(dir, "app.yaml"), starterConfig)
	if _, err := addOut(t, "sql-queue", "--dir", dir, "--config", "app.yaml"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, "app.yaml")), "jobs_provider: sql") {
		t.Error("--config was not the file written")
	}
}

// A main.go the editor cannot read as the scaffold's is not guessed at: the
// call to add is printed and the command fails, as generate module --mount
// does. The import, which needs no chain, is still written.
func TestAddRecipe_NoChainIsRefusedWithTheLine(t *testing.T) {
	dir := recipeProject(t, starterConfig)
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	out, err := addOut(t, "oidc", "--dir", dir)
	if !errors.Is(err, errNoBuilderChain) {
		t.Fatalf("want errNoBuilderChain, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "nucleus.New().Mount(nucleus.FederatedSignIn())") {
		t.Errorf("the call to add is not printed:\n%s", out)
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, "main.go")), "pkg/auth/federated/oidc") {
		t.Error("the import was not written")
	}
}

// The chain call follows the file's own name for pkg/nucleus.
func TestAddRecipe_FollowsAnAliasedImport(t *testing.T) {
	dir := recipeProject(t, starterConfig)
	aliased := strings.Replace(chainMainGo, `"github.com/jcsvwinston/nucleus/pkg/nucleus"`, `nx "github.com/jcsvwinston/nucleus/pkg/nucleus"`, 1)
	aliased = strings.Replace(aliased, "nucleus.New()", "nx.New()", 1)
	mustWrite(t, filepath.Join(dir, "main.go"), aliased)
	if out, err := addOut(t, "oidc", "--dir", dir); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if main := readFile(t, filepath.Join(dir, "main.go")); !strings.Contains(main, "Mount(nx.FederatedSignIn())") {
		t.Errorf("the call does not use the file's name for pkg/nucleus:\n%s", main)
	}
	again, _ := addOut(t, "oidc", "--dir", dir)
	if !strings.Contains(again, "already wired") {
		t.Errorf("an aliased call is not recognised on the second run:\n%s", again)
	}
}

// Every recipe's block is configuration the framework accepts: appended to
// the api starter's nucleus.yml, the strict loader reads it without an
// unknown key. A block with a typo would otherwise be written into a
// project and refused at its first boot.
func TestAddRecipe_EveryBlockIsValidConfiguration(t *testing.T) {
	for _, e := range knownproviders.Entries() {
		if e.Recipe == nil || e.Recipe.Config == "" {
			continue
		}
		t.Run(e.Name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nucleus.yml")
			mustWrite(t, path, starterConfig+"\n"+e.Recipe.Config)
			if _, err := nucleus.New().FromConfigFile(path).Build(); err != nil {
				t.Fatalf("the %s block is refused by the configuration loader: %v", e.Name, err)
			}
		})
	}
}

// `nucleus new --with` writes what `nucleus add` would.
func TestNewWithAppliesTheRecipes(t *testing.T) {
	stubScaffoldNetwork(t)
	out := t.TempDir()
	var stdout bytes.Buffer
	if err := runNew([]string{"wired", "--out", out, "--template", "api", "--with", "oidc,apikeys,sql-queue", "--offline"}, strings.NewReader(""), &stdout, io.Discard); err != nil {
		t.Fatalf("nucleus new: %v\n%s", err, stdout.String())
	}
	dir := filepath.Join(out, "wired")
	main := readFile(t, filepath.Join(dir, "main.go"))
	for _, want := range []string{"pkg/auth/federated/oidc", "Mount(nucleus.FederatedSignIn())", "WithAPIKeys()"} {
		if !strings.Contains(main, want) {
			t.Errorf("main.go lacks %s:\n%s", want, main)
		}
	}
	config := readFile(t, filepath.Join(dir, "nucleus.yml"))
	for _, want := range []string{"auth_federated:", "jobs_provider: sql"} {
		if !strings.Contains(config, want) {
			t.Errorf("nucleus.yml lacks %s:\n%s", want, config)
		}
	}
	// And the project nucleus add would then find already wired.
	again, err := addOut(t, "oidc", "apikeys", "sql-queue", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(again, "wired  ") || strings.Contains(again, "wrote  ") {
		t.Errorf("nucleus add wrote into a project new --with had already wired:\n%s", again)
	}
}
