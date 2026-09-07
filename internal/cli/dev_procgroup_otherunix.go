// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build unix && !linux

package cli

import "os/exec"

// bindChildToParentDeath has no parent-death signal on this platform:
// devStopSignals covers every signal the loop can catch, and an
// application outliving a SIGKILLed loop is stopped by the next loop
// taking the port.
func bindChildToParentDeath(*exec.Cmd) {}
