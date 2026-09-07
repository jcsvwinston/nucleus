// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package cli

import (
	"os/exec"
	"syscall"
)

// runInOwnProcessGroup starts the application in a process group of its
// own and, when the command's context expires, kills the whole group: an
// application may fork helpers of its own, and killing only the child
// would leave them (and any listener they hold) running.
func runInOwnProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
