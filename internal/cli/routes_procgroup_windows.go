// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package cli

import "os/exec"

// runInOwnProcessGroup has no process groups to lean on here: the
// command's context kills the application process itself on expiry.
func runInOwnProcessGroup(cmd *exec.Cmd) {
	cmd.Cancel = func() error { return cmd.Process.Kill() }
}
