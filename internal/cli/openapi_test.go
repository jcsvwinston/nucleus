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

// TestOpenAPI_MissingContractsFailsWithRecipe pins the pre-check on a
// project without internal/contracts: the export used to compile a throwaway
// exporter that imports <module>/internal/contracts and hand the reader the
// raw stderr of `go run` — "no required module provides package
// example.com/myapp/internal/contracts; to add it: go get
// example.com/myapp/internal/contracts" — an instruction that cannot work
// for a package that lives in the project itself. Now the command fails
// before any build, naming the commands that create the aggregator.
//
// The test runs with the module proxy and the workspace off so a regression
// (reaching `go run`) fails fast and offline instead of trying the network
// for example.com.
func TestOpenAPI_MissingContractsFailsWithRecipe(t *testing.T) {
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module example.com/myapp\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Run([]string{"openapi", "--project", projectDir, "--out", "-"}, strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatalf("openapi succeeded on a project without internal/contracts; stdout:\n%s", out.String())
	}
	msg := errOut.String()
	for _, want := range []string{
		"internal/contracts/contracts.go",
		projectDir,
		"nucleus generate resource <Name>",
		"nucleus startapp <name>",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("openapi error lacks %q:\n%s", want, msg)
		}
	}
	for _, leaked := range []string{"go get example.com", "no required module provides package"} {
		if strings.Contains(msg, leaked) {
			t.Errorf("openapi error still leaks the raw go run output %q:\n%s", leaked, msg)
		}
	}
	if out.Len() != 0 {
		t.Errorf("openapi wrote to stdout on failure: %q", out.String())
	}

	// The pre-check must run before the exporter workspace is created, so
	// nothing is left behind in the project.
	leftovers, _ := filepath.Glob(filepath.Join(projectDir, ".nucleus-openapi-*"))
	if len(leftovers) != 0 {
		t.Errorf("openapi left exporter workspaces behind: %v", leftovers)
	}
}

// TestRequireContractsAggregator_PassesWhenPresent keeps the check a pure
// existence test: an aggregator that exists — whatever it registers — lets
// the export proceed to the real build.
func TestRequireContractsAggregator_PassesWhenPresent(t *testing.T) {
	projectDir := t.TempDir()
	if err := requireContractsAggregator(projectDir); err == nil {
		t.Fatal("requireContractsAggregator passed on an empty project")
	}
	if err := ensureContractsAggregator(projectDir, "Test API"); err != nil {
		t.Fatal(err)
	}
	if err := requireContractsAggregator(projectDir); err != nil {
		t.Fatalf("requireContractsAggregator failed with the aggregator present: %v", err)
	}
}
