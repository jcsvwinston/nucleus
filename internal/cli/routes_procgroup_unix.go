// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

// runInOwnProcessGroup starts the application in a process group of its
// own and, when the command's context ends, kills the whole group: an
// application may fork helpers of its own, and killing only the child
// would leave them (and any listener they hold) running.
func runInOwnProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// routesStopSignals are the signals that end the run context: a terminal's
// Ctrl-C (which, the child being in its own group, never reaches it on its
// own) and the SIGTERM a CI cancel, `timeout` or `kill` sends.
func routesStopSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
