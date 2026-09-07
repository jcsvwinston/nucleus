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

// devHelperLoop is the real `nucleus dev` (under the fake builder) run as
// a separate process: the test binary re-executed in its loop helper mode
// on a project of its own, with the build directory under a private
// TMPDIR so leftovers are visible. It returns once the application
// answers on its port; the command is killed at cleanup if it is still
// running.
type devHelperLoopProcess struct {
	cmd            *exec.Cmd
	addr           string
	tmpRoot        string
	stdout, stderr *bytes.Buffer
	waited         chan error
}

func startDevHelperLoop(t *testing.T, extraEnv ...string) *devHelperLoopProcess {
	t.Helper()
	dir := writeDevProject(t)
	tmpRoot := t.TempDir()
	port := freeLoopbackPort(t)
	p := &devHelperLoopProcess{
		cmd:     exec.Command(os.Args[0]),
		addr:    "127.0.0.1:" + strconv.Itoa(port),
		tmpRoot: tmpRoot,
		stdout:  &bytes.Buffer{},
		stderr:  &bytes.Buffer{},
		waited:  make(chan error, 1),
	}
	p.cmd.Env = append(append(os.Environ(),
		"NUCLEUS_DEV_HELPER=loop",
		"NUCLEUS_DEV_HELPER_DIR="+dir,
		"NUCLEUS_DEV_HELPER_PORT="+strconv.Itoa(port),
		"TMPDIR="+tmpRoot,
	), extraEnv...)
	p.cmd.Stdout = p.stdout
	p.cmd.Stderr = p.stderr
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.waited <- p.cmd.Wait() }()
	t.Cleanup(func() {
		select {
		case <-p.waited:
		default:
			_ = p.cmd.Process.Kill()
		}
	})

	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		select {
		case err := <-p.waited:
			t.Fatalf("the command exited before the application answered: %v\nstdout: %s\nstderr: %s", err, p.stdout.String(), p.stderr.String())
		default:
		}
		conn, dialErr := net.DialTimeout("tcp", p.addr, 200*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return p
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the application never answered on %s\nstdout: %s\nstderr: %s", p.addr, p.stdout.String(), p.stderr.String())
	return nil
}

// stopWith sends sig to the loop and asserts the session ends cleanly:
// exit 0, the application reported stopped, its port closed, no build
// directory left under TMPDIR.
func (p *devHelperLoopProcess) stopWith(t *testing.T, sig syscall.Signal) {
	t.Helper()
	if err := p.cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-p.waited:
		if err != nil {
			t.Fatalf("the command signalled with %s must exit 0, got %v\nstdout: %s\nstderr: %s", sig, err, p.stdout.String(), p.stderr.String())
		}
	case <-time.After(20 * time.Second):
		_ = p.cmd.Process.Kill()
		t.Fatalf("the command did not exit within 20s of %s\nstdout: %s\nstderr: %s", sig, p.stdout.String(), p.stderr.String())
	}
	if !strings.Contains(p.stdout.String(), "[dev] stopped the application") {
		t.Errorf("the loop must report that it stopped the application, got:\n%s", p.stdout.String())
	}

	waitForPortToClose(t, p.addr)
	leftovers, err := filepath.Glob(filepath.Join(p.tmpRoot, "nucleus-dev-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Errorf("the build directory must be removed when the command is signalled, left: %v", leftovers)
	}
}

// A signal to `nucleus dev` must stop the application and remove the
// build directory: nothing left listening, nothing left on disk, and the
// command exits 0 — ending a session is not a failure. The application
// runs in its own process group, so none of these reaches it on its own:
// a terminal's Ctrl-C and a `kill` are the SIGTERM case; closing the
// terminal's tab or window, or an SSH connection dropping, is the SIGHUP
// case — the everyday end of a session, which used to leave the
// application listening while the loop died with exit 129.
func TestDevStopsTheApplicationWhenTheCommandIsSignalled(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			p := startDevHelperLoop(t)
			p.stopWith(t, sig)
		})
	}
}
