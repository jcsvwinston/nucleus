// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The pre-check recognises this checkout as carrying the variable: a
// project that replaces nucleus with the repo root is not sent to the
// configuration-only fallback.
func TestResolveNucleusDependencyRecognisesThisCheckout(t *testing.T) {
	repoRoot := repoRootForTest(t)
	dir := t.TempDir()
	goMod := fmt.Sprintf("module example.com/current\n\ngo 1.22\n\nrequire github.com/jcsvwinston/nucleus v0.0.0\n\nreplace github.com/jcsvwinston/nucleus => %s\n", repoRoot)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	dep := resolveNucleusDependency(dir)
	if !dep.known || !dep.carriesRouteDump {
		t.Fatalf("this checkout carries internal/routedump, got %+v", dep)
	}
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, "go.mod"), []byte("module example.com/plain\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dep := resolveNucleusDependency(plain); dep.known {
		t.Fatalf("a project without the dependency is unknown to the pre-check, got %+v", dep)
	}
}
