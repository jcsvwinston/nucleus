// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// starterDocumentBaseline is the OpenAPI document of the suite starter
// (`nucleus new --template suite`), frozen: the API every quickstart reader
// builds on, and the one the generated TypeScript client of the arc's gate
// is written against. A change to the starter that breaks a client written
// against it fails here, naming the change; an addition passes. Replace it
// deliberately with NUCLEUS_UPDATE_STARTER_OPENAPI=1.
const starterDocumentBaseline = "contracts/baseline/starter_openapi.json"

// TestSuiteStarterDocumentUnderContract scaffolds the suite starter against
// the sibling orbit and quark checkouts (skipped without them, like the
// starter's boot test) and checks the document it serves against the
// baseline with `nucleus openapi --check`.
func TestSuiteStarterDocumentUnderContract(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the suite scaffold; skipped with -short")
	}
	repoRoot := repoRootForTest(t)
	orbit, quark := siblingCheckouts(repoRoot)
	if orbit == "" {
		skipUnlessRequired(t, "no orbit and quark checkouts next to this repository (set NUCLEUS_SIBLING_CHECKOUTS); the suite starter lane runs this test")
	}
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	args := []string{"shop", "--out", outDir, "--template", "suite", "--module", "example.com/shop", "--port", fmt.Sprint(freeLoopbackPort(t)), "--offline"}
	if err := runNew(args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew(suite): %v\nstderr: %s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, "shop")
	pinGoModToSiblingCheckouts(t, projectDir, repoRoot, orbit, quark)
	runGoCommand(t, projectDir, "mod", "tidy")

	baseline := filepath.Join(repoRoot, filepath.FromSlash(starterDocumentBaseline))
	if os.Getenv("NUCLEUS_UPDATE_STARTER_OPENAPI") == "1" {
		stdout.Reset()
		stderr.Reset()
		if code := Run([]string{"openapi", "--project", projectDir, "--from", "app", "--out", baseline}, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("export the starter's document: exit %d: %s", code, stderr.String())
		}
		t.Logf("baseline replaced: %s", baseline)
		return
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"openapi", "--project", projectDir, "--from", "app", "--check", baseline}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("the suite starter's document breaks a client written against %s:\n%s", starterDocumentBaseline, stderr.String())
	}
}
