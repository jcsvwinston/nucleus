// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// noisyGoOnPath puts a `go` wrapper first on PATH that writes the chatter
// a cold module cache produces to stderr before running the real go: the
// `go: downloading ...` lines of a fresh clone, a CI runner or a machine
// after `go clean -modcache`. It returns the PATH value for a process
// that must inherit it.
func noisyGoOnPath(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not on PATH")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\necho 'go: downloading golang.org/x/example v0.0.1' >&2\necho 'go: downloading github.com/example/dep v1.2.3' >&2\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	path := bin + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", path)
	return path
}

// `go list` chatter on stderr — a cold module cache downloading, -mod=mod
// resolving — is not part of the package name: a main package is
// recognised as one, for dev and for routes, and the chatter is not in
// the message when the directory is not a main package. Read combined,
// the chatter was glued to the name and a fresh clone was refused with
// "it is the library package go: downloading ... main".
func TestEnsureMainPackageReadsTheNameApartFromGoChatter(t *testing.T) {
	noisyGoOnPath(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":                  "module example.com/layout\n\ngo 1.22\n",
		"main.go":                 "package main\n\nfunc main() {}\n",
		"internal/notes/notes.go": "package notes\n\nvar X = 1\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, command := range []string{"dev", "routes"} {
		if err := ensureMainPackage(root, root, command); err != nil {
			t.Errorf("%s: a main package must be accepted whatever go list writes to stderr, got: %v", command, err)
		}
	}

	library := filepath.Join(root, "internal", "notes")
	for command, wantsWayOut := range map[string]bool{"dev": false, "routes": true} {
		err := ensureMainPackage(library, root, command)
		if err == nil {
			t.Fatalf("%s: a library package must be an error", command)
		}
		msg := err.Error()
		if !strings.Contains(msg, "it is the library package notes, not a main package") {
			t.Errorf("%s: the error must name the package alone, got: %v", command, err)
		}
		if strings.Contains(msg, "downloading") {
			t.Errorf("%s: go list chatter must not be in the message, got: %v", command, err)
		}
		if strings.Contains(msg, "--framework-only") != wantsWayOut {
			t.Errorf("%s: --framework-only in the message = %v, want %v: %v", command, !wantsWayOut, wantsWayOut, err)
		}
	}
}

// End to end: `nucleus dev` on a cold module cache — every `go` invocation
// writing download chatter to stderr — builds, runs the application and
// ends cleanly on a signal.
func TestDevRunsWhenGoListWritesToStderr(t *testing.T) {
	path := noisyGoOnPath(t)
	p := startDevHelperLoop(t, "PATH="+path)
	p.stopWith(t, syscall.SIGTERM)
}
