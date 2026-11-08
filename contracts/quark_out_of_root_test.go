// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package contracts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The framework's module does not depend on Quark (arc D3 thinned the
// graph on purpose; owner decision D12, 2026-11-08). The one place the
// framework needs Quark's grammar — nucleustest.Make persisting a Quark
// model — is served by a module of its own, pkg/nucleustest/quarkmake,
// behind the nucleustest.Factory seam. This test holds the root to that
// split from both sides: the root go.mod lists no Quark module, and
// nothing pkg/nucleustest links reaches one.
func TestRootModuleKeepsQuarkOutOfItsGraph(t *testing.T) {
	repoRoot := filepath.Dir(baselinePath(t))
	raw, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "github.com/jcsvwinston/quark") {
			t.Fatalf("the root go.mod requires Quark: %q\n\nThe framework's module keeps Quark out of its graph (D3, D12). What needs Quark lives in a module of its own: pkg/nucleustest/quarkmake for the test factory, the driver modules for an engine. Move the import there.", strings.TrimSpace(line))
		}
	}

	out, err := exec.Command("go", "list", "-deps", "../pkg/nucleustest").Output()
	if err != nil {
		t.Fatalf("go list -deps ../pkg/nucleustest: %v", err)
	}
	for _, dep := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(dep, "github.com/jcsvwinston/quark") {
			t.Fatalf("pkg/nucleustest links %s: the kit reads Quark models through the Factory installed with UseFactory (pkg/nucleustest/quarkmake), never by importing Quark (D12)", dep)
		}
	}

	// The seam's other side: the quarkmake module is where Quark is
	// required, and its package does not import the kit — Go interfaces
	// are structural, so it satisfies nucleustest.Factory without the
	// import, and builds against the framework release it pins.
	mod, err := os.ReadFile(filepath.Join(repoRoot, "pkg/nucleustest/quarkmake/go.mod"))
	if err != nil {
		t.Fatalf("read pkg/nucleustest/quarkmake/go.mod: %v", err)
	}
	if !strings.Contains(string(mod), "github.com/jcsvwinston/quark v") {
		t.Fatalf("pkg/nucleustest/quarkmake/go.mod does not require Quark — that module is where the Quark requirement lives")
	}
	src, err := os.ReadFile(filepath.Join(repoRoot, "pkg/nucleustest/quarkmake/quarkmake.go"))
	if err != nil {
		t.Fatalf("read quarkmake.go: %v", err)
	}
	if strings.Contains(string(src), `"github.com/jcsvwinston/nucleus/pkg/nucleustest"`) {
		t.Fatalf("pkg/nucleustest/quarkmake imports the kit: it must satisfy nucleustest.Factory structurally, or it stops building against the framework release its go.mod pins (the tests may import the kit; the package may not)")
	}
}
