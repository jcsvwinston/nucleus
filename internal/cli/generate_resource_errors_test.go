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

// NU-136 for `generate resource`: the controller used to write its own
// `{"error": "…"}` bodies, outside the framework's envelope and without the
// problem+json negotiation — while the contract it ships in
// internal/contracts described the envelope. The controller now returns
// framework errors, and the unit test the generator writes next to it (a
// fake repository, handler methods called directly) renders them the way the
// router does and passes as written.
func TestGenerateResourceControllerReturnsFrameworkErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and tests a scaffolded app; skipped with -short")
	}
	projectDir := scaffoldOfflineProject(t, "resapp")
	var stdout, stderr bytes.Buffer
	for _, args := range [][]string{
		{"resource", "Widget", "--out", projectDir, "--offline"},
		{"resource", "Gadget", "--out", projectDir, "--data", "sql", "--offline"},
	} {
		if err := runGenerate(args, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runGenerate %v: %v\nstderr: %s", args, err, stderr.String())
		}
	}

	controller, err := os.ReadFile(filepath.Join(projectDir, "internal", "controllers", "widget_handler.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(controller), `map[string]string{"error"`) {
		t.Fatalf("the resource controller still writes its own error bodies:\n%s", controller)
	}
	for _, want := range []string{"nucleusErrors.NotFound(", "nucleusErrors.BadRequest(", "nucleusErrors.ValidationFailed("} {
		if !strings.Contains(string(controller), want) {
			t.Errorf("the resource controller must answer through the framework (%s):\n%s", want, controller)
		}
	}

	// The quark default pulls Quark into the build; -mod=mod resolves it
	// the way the tidy `--offline` skipped would have.
	runGoInProject(t, projectDir, "vet", "./...")
	out := runGoInProject(t, projectDir, "test", "-count=1", "./internal/controllers/")
	if !strings.Contains(out, "ok  \texample.com/resapp/internal/controllers") {
		t.Fatalf("the generated controller tests must pass, got:\n%s", out)
	}
}
