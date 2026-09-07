// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package cli

import (
	"os"
	"os/exec"
)

// terminateProcessGroup has no SIGTERM to send here: the application
// process is killed outright, as the context cancel would.
func terminateProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// devStopSignals is the interrupt alone, as for routes: it is the only
// signal the runtime delivers here.
func devStopSignals() []os.Signal {
	return routesStopSignals()
}

// bindChildToParentDeath has nothing to lean on here: an application
// outliving a killed loop is stopped by the next loop taking the port.
func bindChildToParentDeath(*exec.Cmd) {}
