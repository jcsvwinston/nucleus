// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Regression tests for QCD-CLI-5 (quantum-coverage-demo): `loaddata` could
// not restore a fixture produced by its own `dumpdata` on schemas with
// foreign keys. Two ordering defects compounded:
//
//   - the default load order was the fixture file order, which dumpdata
//     writes alphabetically ("assets" sorts before "organizations"), so the
//     child table hit its FK constraint (23503) before the parent existed;
//   - an explicit `--tables organizations,...,assets` was silently
//     re-sorted alphabetically (normalizeTableList), so the documented
//     escape hatch did not work either.
//
// Contract fixed here: the load plan is ordered topologically by the FK
// graph introspected from the target database, using the caller's order
// (file order, or --tables order) as the stable tie-break — a valid explicit
// order passes through unchanged, an FK-invalid one is repaired.
package cli

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fkOrderFixture = `{"tables":[
  {"name":"assets","rows":[{"id":1,"org_id":1},{"id":2,"org_id":1}]},
  {"name":"organizations","rows":[{"id":1,"name":"acme"}]}
]}`

func writeFKOrderProject(t *testing.T) (cfgPath, fixturePath string) {
	t.Helper()
	cfgPath, dbPath := writeAdminCLIConfig(t)

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, ddl := range []string{
		`CREATE TABLE organizations (id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE assets (id INTEGER PRIMARY KEY, org_id INTEGER NOT NULL REFERENCES organizations(id))`,
	} {
		if _, err := conn.Exec(ddl); err != nil {
			t.Fatalf("ddl %q: %v", ddl, err)
		}
	}

	fixturePath = filepath.Join(filepath.Dir(cfgPath), "fixture.json")
	if err := os.WriteFile(fixturePath, []byte(fkOrderFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath, fixturePath
}

// tableOrder extracts the table column from LOADED/DRY-RUN plan lines.
func tableOrder(out, marker string) []string {
	var order []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, marker) {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) >= 2 {
			if marker == "DRY-RUN" && len(fields) >= 3 {
				order = append(order, fields[2])
			} else if marker == "LOADED" {
				order = append(order, fields[1])
			}
		}
	}
	return order
}

// With no --tables, the load order must be FK-topological even when the
// fixture file lists the child table first (as dumpdata's alphabetical
// output does).
func TestLoadDataOrdersTablesByForeignKeys(t *testing.T) {
	cfgPath, fixturePath := writeFKOrderProject(t)

	var stdout, stderr bytes.Buffer
	args := []string{"--config", cfgPath, "--file", fixturePath}
	if err := runLoadData(args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("loaddata: %v\nstderr: %s", err, stderr.String())
	}

	order := tableOrder(stdout.String(), "LOADED")
	want := fmt.Sprint([]string{"organizations", "assets"})
	if fmt.Sprint(order) != want {
		t.Errorf("load order %v is not FK-topological (want organizations before assets)\nstdout:\n%s", order, stdout.String())
	}
}

// An explicit, FK-valid --tables order must survive verbatim — today it is
// silently re-sorted alphabetically, which is exactly what broke the demo's
// escape hatch.
func TestLoadDataRespectsExplicitTablesOrder(t *testing.T) {
	cfgPath, fixturePath := writeFKOrderProject(t)

	var stdout, stderr bytes.Buffer
	args := []string{"--config", cfgPath, "--file", fixturePath, "--dry-run", "--tables", "organizations,assets"}
	if err := runLoadData(args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("loaddata --dry-run: %v\nstderr: %s", err, stderr.String())
	}

	order := tableOrder(stdout.String(), "DRY-RUN")
	want := fmt.Sprint([]string{"organizations", "assets"})
	if fmt.Sprint(order) != want {
		t.Errorf("--tables order not respected: got %v, want [organizations assets]\nstdout:\n%s", order, stdout.String())
	}
}
