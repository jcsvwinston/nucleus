// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// skipUnlessRequired skips the test, unless the lane said its
// prerequisites are there (NUCLEUS_REQUIRE_SIBLING_CHECKOUTS): then a
// missing one fails it, so the gate can never go green by not running.
func skipUnlessRequired(t *testing.T, why string) {
	t.Helper()
	if os.Getenv("NUCLEUS_REQUIRE_SIBLING_CHECKOUTS") != "" {
		t.Fatalf("NUCLEUS_REQUIRE_SIBLING_CHECKOUTS is set: %s", why)
	}
	t.Skip(why)
}

// TestSuiteStarterTypeScriptClient is the gate of arc A10: the suite
// starter (`nucleus new --template suite`) serves its OpenAPI document, a
// TypeScript client is generated from it (`nucleus openapi --client
// typescript`), and a script that uses nothing but that client consumes
// the starter's API — list, filter, create, the 409 of a duplicate, the
// refusal of an invalid body — under node.
//
// It needs the sibling orbit and quark checkouts (like the starter's boot
// test) and node 22.18+, which runs a .ts file without a build step; it is
// skipped, not faked, without them. With NUCLEUS_TSC set to a TypeScript
// compiler the client and the script are also type-checked with --strict.
func TestSuiteStarterTypeScriptClient(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and boots the suite scaffold; skipped with -short")
	}
	node, ok := nodeStripsTypes(t)
	if !ok {
		skipUnlessRequired(t, "needs node 22.18+ on PATH (runs .ts without a build step)")
	}
	repoRoot := repoRootForTest(t)
	orbit, quark := siblingCheckouts(repoRoot)
	if orbit == "" {
		skipUnlessRequired(t, "no orbit and quark checkouts next to this repository (set NUCLEUS_SIBLING_CHECKOUTS); the suite starter lane runs this test")
	}

	outDir := t.TempDir()
	port := freeLoopbackPort(t)
	var stdout, stderr bytes.Buffer
	args := []string{"shop", "--out", outDir, "--template", "suite", "--module", "example.com/shop", "--port", fmt.Sprint(port), "--offline"}
	if err := runNew(args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew(suite): %v\nstderr: %s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, "shop")
	pinGoModToSiblingCheckouts(t, projectDir, repoRoot, orbit, quark)
	runGoCommand(t, projectDir, "mod", "tidy")
	runGoCommand(t, projectDir, "build", "-o", "app", ".")

	clientDir := filepath.Join(outDir, "client")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"openapi", "--project", projectDir, "--client", "typescript", "--out", filepath.Join(clientDir, "client.ts")}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("nucleus openapi --client typescript: exit %d: %s", code, stderr.String())
	}
	script, err := os.ReadFile(filepath.Join(repoRoot, "internal", "cli", "testdata", "starter_client", "e2e.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clientDir, "e2e.ts"), script, 0o644); err != nil {
		t.Fatal(err)
	}
	// ES modules, the way a frontend project uses the client: top-level
	// await in the script, import of the client by its .ts name.
	if err := os.WriteFile(filepath.Join(clientDir, "package.json"), []byte(`{"type": "module", "private": true}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if tsc := os.Getenv("NUCLEUS_TSC"); tsc != "" {
		// tsc sits in node_modules/.bin; the node types it checks the
		// script against are installed beside it.
		typeRoots := filepath.Join(filepath.Dir(filepath.Dir(tsc)), "@types")
		check := exec.Command(tsc, "--noEmit", "--strict", "--target", "es2022", "--module", "nodenext", "--moduleResolution", "nodenext",
			"--allowImportingTsExtensions", "--lib", "es2022,dom", "--typeRoots", typeRoots, "--types", "node", "client.ts", "e2e.ts")
		check.Dir = clientDir
		if out, err := check.CombinedOutput(); err != nil {
			client, _ := os.ReadFile(filepath.Join(clientDir, "client.ts"))
			t.Fatalf("tsc --strict on the generated client: %v\n%s\n--- client.ts ---\n%s", err, out, client)
		}
	}

	app := exec.Command(filepath.Join(projectDir, "app"))
	app.Dir = projectDir
	app.Env = append(os.Environ(), "ADMIN_BOOTSTRAP_PASSWORD=quickstart")
	var appLog bytes.Buffer
	app.Stdout = &appLog
	app.Stderr = &appLog
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = app.Process.Kill()
		_ = app.Wait()
	}
	t.Cleanup(stop)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForHealthz(t, &http.Client{Timeout: 2 * time.Second}, base, func() string { stop(); return appLog.String() })

	run := exec.Command(node, "e2e.ts", base)
	run.Dir = clientDir
	out, err := run.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "starter client e2e: ok") {
		client, _ := os.ReadFile(filepath.Join(clientDir, "client.ts"))
		stop()
		t.Fatalf("node e2e.ts: %v\n%s\n--- client.ts ---\n%s\n--- app log ---\n%s", err, out, client, appLog.String())
	}
}
