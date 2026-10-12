// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package dbclassify_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The predicates are held to each engine's real error type in
// internal/testdeps/dbclassify: those tests import four engines, and the
// framework's go.mod — the one every application inherits — lists whatever
// its own tests import (NU-106).

// The point of the package: it reaches the standard library and nothing
// else. Importing an engine here is what made every driver module link all
// five (NU-8).
func TestImportsNoEngine(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".")
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	var foreign []string
	for _, p := range strings.Fields(string(out)) {
		if p != "github.com/jcsvwinston/nucleus/internal/dbclassify" {
			foreign = append(foreign, p)
		}
	}
	if len(foreign) > 0 {
		t.Fatalf("internal/dbclassify reaches packages outside the standard library:\n  %s\n\n"+
			"Driver modules up to v0.1.7 import this package, so whatever it imports every one of\n"+
			"their applications links. An engine's error type belongs in that engine's module.",
			strings.Join(foreign, "\n  "))
	}
}
