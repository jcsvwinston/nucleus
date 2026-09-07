// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRoutesHelperProcess is the child of
// TestRoutesStopsTheApplicationWhenTheCommandIsSignalled: the test binary
// re-executed with NUCLEUS_ROUTES_HELPER=1 runs the command itself, so the
// signal lands on a real `nucleus routes` process and not on the test.
func TestRoutesHelperProcess(t *testing.T) {
	if os.Getenv("NUCLEUS_ROUTES_HELPER") != "1" {
		return
	}
	dir := os.Getenv("NUCLEUS_ROUTES_HELPER_DIR")
	if err := runRoutes([]string{"--dir", dir, "--timeout", "60s"}, strings.NewReader(""), os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// The application runs in its own process group, so a terminal's Ctrl-C
// never reaches it on its own and a SIGTERM to the command (CI cancel,
// `timeout`, `kill`) used to end the command and orphan the application
// with its listener and the build directory. The command must take the
// signal as the end of the run: kill the group, remove the build directory
// and exit non-zero saying so.
func TestRoutesStopsTheApplicationWhenTheCommandIsSignalled(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs a Go program; skipped with -short")
	}
	dir := t.TempDir()
	addr := writeServingProject(t, dir)
	// The build directory of the child lands under this TMPDIR, so what is
	// left behind can be asserted without touching the shared temp dir.
	tmpRoot := t.TempDir()

	cmd := exec.Command(os.Args[0], "-test.run=^TestRoutesHelperProcess$")
	cmd.Env = append(os.Environ(),
		"NUCLEUS_ROUTES_HELPER=1",
		"NUCLEUS_ROUTES_HELPER_DIR="+dir,
		"TMPDIR="+tmpRoot,
		"GOFLAGS=-mod=mod",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	// Wait for the application to answer: the build is part of this window.
	answering := false
	for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); {
		select {
		case err := <-waited:
			t.Fatalf("the command exited before the application answered: %v\nstderr: %s", err, stderr.String())
		default:
		}
		conn, dialErr := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			answering = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !answering {
		_ = cmd.Process.Kill()
		t.Fatalf("the application never answered on %s\nstderr: %s", addr, stderr.String())
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-waited:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("the signalled command must exit non-zero, got %v\nstderr: %s", err, stderr.String())
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("the command did not exit within 15s of SIGTERM\nstderr: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "stopped on signal") {
		t.Errorf("the error must say the run was stopped on signal, got stderr:\n%s", stderr.String())
	}

	// Nothing left listening, nothing left on disk.
	waitForPortToClose(t, addr)
	leftovers, err := filepath.Glob(filepath.Join(tmpRoot, "nucleus-routes-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Errorf("the build directory must be removed when the command is signalled, left: %v", leftovers)
	}
}
