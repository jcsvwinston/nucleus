// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package cli

import (
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
