// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package cli

import (
	"os"
	"os/exec"
)

// runInOwnProcessGroup has no process groups to lean on here: the
// command's context kills the application process itself when it ends.
func runInOwnProcessGroup(cmd *exec.Cmd) {
	cmd.Cancel = func() error { return cmd.Process.Kill() }
}

// routesStopSignals is the interrupt alone: it is the only signal the
// runtime delivers here.
func routesStopSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
