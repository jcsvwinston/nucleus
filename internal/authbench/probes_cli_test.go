// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/cli"
)

// hasCLICommand reports whether the CLI registers a command whose name
// contains the term — read off the command table the binary dispatches, not
// off the documentation that describes it.
func hasCLICommand(t *testing.T, term string) bool {
	t.Helper()
	names := append(cli.ContractPrimaryCommandNames(), cli.ContractAliasCommandNames()...)
	for _, n := range names {
		if strings.Contains(strings.ToLower(n), term) {
			t.Logf("CLI command %q", n)
			return true
		}
	}
	return false
}
