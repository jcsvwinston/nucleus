package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerateMultipleEntitiesBuilds is the multi-entity safety regression
// guard for the code generators. Scaffolding a SECOND app/resource into the
// same project used to redeclare the shared package-level NameOnlyRecord in
// internal/repositories, so any realistic (multi-entity) project stopped
// compiling after the second `startapp`/`generate resource`. The repository
// record type is now named per entity; this test scaffolds a project, runs
// both generators for three entities, and compile-checks the result.
func TestGenerateMultipleEntitiesBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go build of generated project in -short mode")
	}

	repoRoot := repoRootForTest(t)
	outDir := t.TempDir()
	projectName := "multigen"

	var stdout, stderr bytes.Buffer
	newArgs := []string{projectName, "--out", outDir, "--template", "mvc", "--module", "example.com/multigen"}
	if err := runNew(newArgs, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew failed: %v\nstderr: %s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, projectName)

	for _, app := range []string{"fleet", "device"} {
		stdout.Reset()
		stderr.Reset()
		if err := runStartApp([]string{app, "--out", projectDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runStartApp(%q) failed: %v\nstderr: %s", app, err, stderr.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	if err := runGenerate([]string{"resource", "alert", "--out", projectDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate(resource alert) failed: %v\nstderr: %s", err, stderr.String())
	}

	// Each repository must carry its own record type — no shared symbol.
	// The SECOND entity (device) is the one that used to redeclare it, so
	// assert both files independently of the final go build.
	for entity, record := range map[string]string{"fleet": "FleetRecord", "device": "DeviceRecord"} {
		repo, err := os.ReadFile(filepath.Join(projectDir, "internal", "repositories", entity+"_repository.go"))
		if err != nil {
			t.Fatalf("read %s repository: %v", entity, err)
		}
		if strings.Contains(string(repo), "NameOnlyRecord") {
			t.Fatalf("%s repository still uses the shared NameOnlyRecord type:\n%s", entity, repo)
		}
		if !strings.Contains(string(repo), "type "+record+" struct") {
			t.Fatalf("%s repository missing per-entity %s type:\n%s", entity, record, repo)
		}
	}

	pinGoModToLocalNucleus(t, projectDir, repoRoot)
	runGoCommand(t, projectDir, "mod", "tidy")
	runGoCommand(t, projectDir, "build", "./...")
}

// TestPluralizeResource pins the table-name pluralizer, including the
// already-plural inputs that used to double-pluralize (fleets -> fleetses).
// The heuristic knowingly treats rare s-ending singulars outside the
// ss/us/is families (canvas, atlas, gas) as already plural — acceptable for
// scaffold table names, where passing the plural is also supported.
func TestPluralizeResource(t *testing.T) {
	cases := []struct{ in, want string }{
		// singulars
		{"fleet", "fleets"},
		{"device", "devices"},
		{"policy", "policies"},
		{"box", "boxes"},
		{"address", "addresses"},
		{"status", "statuses"},
		{"bus", "buses"},
		// already plural — must come back unchanged
		{"fleets", "fleets"},
		{"devices", "devices"},
		{"sim_cards", "sim_cards"},
		{"usage_records", "usage_records"},
	}
	for _, tc := range cases {
		if got := pluralizeResource(tc.in); got != tc.want {
			t.Errorf("pluralizeResource(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
