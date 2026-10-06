// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package testsqlite_test

import (
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

const self = "github.com/jcsvwinston/nucleus/internal/testsqlite"

// Only test files import this package. A non-test importer — a pkg/
// package, an internal package the runtime reaches — would link SQLite into
// every application, whatever engine it chose.
func TestOnlyTestFilesImportIt(t *testing.T) {
	cmd := exec.Command("go", "list", "-f", `{{.ImportPath}} {{join .Imports " "}}`, "github.com/jcsvwinston/nucleus/...")
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	var importers []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		for _, imp := range fields[min(1, len(fields)):] {
			if imp == self {
				importers = append(importers, fields[0])
			}
		}
	}
	sort.Strings(importers)
	if len(importers) > 0 {
		t.Fatalf("outside test files, %s is imported by %v.\n\n"+
			"It links SQLite. A test binary reaches it from a _test.go file (see\n"+
			"pkg/db/drivers_for_test.go); an application reaches its engine through that\n"+
			"engine's module under drivers/.", self, importers)
	}
}
