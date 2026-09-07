// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package cli

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// devHelperStopSignal is what the helper application waits on: the
// SIGTERM the loop sends to its process group.
func devHelperStopSignal() <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, os.Interrupt)
	return ch
}

// A SIGTERM to `nucleus dev` (a terminal's Ctrl-C reaches the loop, not
// the application, which runs in its own group) must stop the application
// and remove the build directory: nothing left listening, nothing left on
// disk, and the command exits 0 — ending a session is not a failure.
func TestDevStopsTheApplicationWhenTheCommandIsSignalled(t *testing.T) {
	dir := writeDevProject(t)
	tmpRoot := t.TempDir()
	port := freeLoopbackPort(t)
	addr := "127.0.0.1:" + strconv.Itoa(port)

	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(),
		"NUCLEUS_DEV_HELPER=loop",
		"NUCLEUS_DEV_HELPER_DIR="+dir,
		"NUCLEUS_DEV_HELPER_PORT="+strconv.Itoa(port),
		"TMPDIR="+tmpRoot,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	answering := false
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		select {
		case err := <-waited:
			t.Fatalf("the command exited before the application answered: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
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
		t.Fatalf("the application never answered on %s\nstdout: %s\nstderr: %s", addr, stdout.String(), stderr.String())
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("the signalled command must exit 0, got %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
		}
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("the command did not exit within 20s of SIGTERM\nstdout: %s\nstderr: %s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "[dev] stopped the application") {
		t.Errorf("the loop must report that it stopped the application, got:\n%s", stdout.String())
	}

	waitForPortToClose(t, addr)
	leftovers, err := filepath.Glob(filepath.Join(tmpRoot, "nucleus-dev-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Errorf("the build directory must be removed when the command is signalled, left: %v", leftovers)
	}
}
