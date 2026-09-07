// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package cli

import (
	"os/exec"
	"syscall"
)

// bindChildToParentDeath asks the kernel to SIGKILL the application when
// the loop dies without running its shutdown path at all — a SIGKILL to
// `nucleus dev`, an OOM kill — so the port is not held by an orphan. It
// complements devStopSignals, which covers every signal the loop can
// catch. Belt and braces: the kernel delivers it when the thread that
// started the child exits, and the Go runtime keeps its threads alive for
// the life of the process, so in practice that is the loop's death.
func bindChildToParentDeath(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}
