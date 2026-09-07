// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

// terminateProcessGroup asks the application started with
// runInOwnProcessGroup to shut down: SIGTERM to the whole group, which
// nucleus.Run turns into a graceful shutdown. The context cancel (SIGKILL
// to the group) is the fallback when it does not exit in time.
func terminateProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

// devStopSignals are the signals that end a development session: the
// signals routes stops on (Ctrl-C, SIGTERM) and SIGHUP, the terminal's
// hangup when its tab or window closes or an SSH connection drops — the
// everyday way a session ends. The application runs in its own process
// group, so the hangup never reaches it on its own; without this entry the
// loop would die on it while the application kept listening.
func devStopSignals() []os.Signal {
	return append(routesStopSignals(), syscall.SIGHUP)
}
