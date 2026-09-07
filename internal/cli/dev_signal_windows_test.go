// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package cli

import (
	"os"
	"os/signal"
)

// devHelperStopSignal is what the helper application waits on: the
// interrupt, the one signal the runtime delivers here.
func devHelperStopSignal() <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	return ch
}
