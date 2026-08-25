// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-CLI-6: `nucleus version` reported "dev" for a binary installed from
// the module proxy at an exact version.
//
// `var Version = "dev"` is replaced by ldflags in a release build, but
// `go install pkg@vX.Y.Z` — the path the README documents, and the one the
// external demo and its CI use — passes no ldflags. The binary DOES know
// what it is (`go version -m` reads v1.12.0 from its build info); the
// command simply never asked.
//
// In a CLI-first product `version` is the primary way to prove which CLI
// produced an artifact, so reporting "dev" made a release and a
// hand-compiled binary indistinguishable. quark already does this right;
// the asymmetry between two CLIs of the same suite was the finding.
package cli

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestCLIVersion_FallsBackToBuildInfo(t *testing.T) {
	// With no ldflags value, the module version recorded in the binary is
	// the answer. Under `go test` that is "(devel)", which must degrade to
	// a truthful "devel" rather than the misleading "dev".
	got := cliVersion()
	if got == "dev" {
		t.Fatal("cliVersion must not report the ldflags placeholder; a binary installed from the proxy knows its own version")
	}

	bi, ok := debug.ReadBuildInfo()
	if ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		if got != bi.Main.Version {
			t.Errorf("cliVersion() = %q, want the build-info version %q", got, bi.Main.Version)
		}
	}
	if strings.Contains(got, "(devel)") {
		t.Errorf("the raw build-info placeholder must not leak to the user, got %q", got)
	}
}

// An explicit ldflags value still wins: release builds stamp it.
func TestCLIVersion_LdflagsWins(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })

	Version = "v9.9.9"
	if got := cliVersion(); got != "v9.9.9" {
		t.Errorf("an injected version must win, got %q", got)
	}
}
