// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest_test

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// engineImports are the five engines' module paths, as scripts/ci/check_hello_size.sh
// counts them.
var engineImports = regexp.MustCompile(`^(github\.com/(jackc/pgx|go-sql-driver/mysql|microsoft/go-mssqldb|sijms/go-ora)|modernc\.org/sqlite)`)

// DEP-2026-019 (NU-121): importing nucleustest links no database engine.
// Up to v1 the kit imported modernc.org/sqlite so TempSQLite and CheckModule
// worked in a test binary with no driver of its own, and every test binary
// that imported the kit carried SQLite whether the application used it or
// not. Since v2.0.0 the test binary imports the driver module it wants
// (drivers/sqlite, or another engine's), like an application.
//
// Two halves. The build list of the package, which is what a consumer's
// test binary inherits; and testdata/nodriver, a test package that imports
// the kit and nothing that registers a driver, run in a process of its own
// because this binary links SQLite (sqlite_driver_for_test.go) and a driver
// cannot be unregistered.
func TestNucleustestLinksNoDriver(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	for _, dep := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if engineImports.MatchString(dep) {
			t.Errorf("pkg/nucleustest links %s: the kit imports no database engine (DEP-2026-019)", dep)
		}
	}

	cmd := exec.Command("go", "test", "-run", "^TestTempSQLiteWithoutDriver$", "./testdata/nodriver")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the no-driver probe (pkg/nucleustest/testdata/nodriver) failed: %v\n%s", err, out)
	}
}
