// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package cli

import "os/exec"

// terminateProcessGroup has no SIGTERM to send here: the application
// process is killed outright, as the context cancel would.
func terminateProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
