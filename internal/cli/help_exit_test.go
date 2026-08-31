// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestEveryCommandHelpExitsZero pins NC-07: `nucleus <cmd> --help` is a
// success for EVERY primary command. An audit sweep found 35 of 38
// subcommands exiting 0 while `config`, `doctor` and `wizard` printed
// "error: flag: help requested" and exited 1 — asking for help is not an
// error anywhere, so it must not be one somewhere.
func TestEveryCommandHelpExitsZero(t *testing.T) {
	for _, spec := range commandSpecs {
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := Run([]string{spec.name, "--help"}, strings.NewReader(""), &out, &errOut)
			if code != 0 {
				t.Fatalf("nucleus %s --help exited %d\nstderr: %s", spec.name, code, errOut.String())
			}
			if strings.Contains(errOut.String(), "help requested") {
				t.Fatalf("nucleus %s --help leaked the flag package's error: %s", spec.name, errOut.String())
			}
		})
	}
}
