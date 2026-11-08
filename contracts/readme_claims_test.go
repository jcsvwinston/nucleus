// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package contracts

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/cli"
)

// The README says how many lifecycle commands the CLI has. It said 40
// while `nucleus --help` listed 43 and the frozen baseline 42 — three
// numbers for one fact, each written by hand. The command table
// (commandSpecs, read through ContractPrimaryCommandNames) is the fact;
// this test makes the README and the baseline derive from it: the number
// the README prints is the table's length, and every command in the table
// is frozen in cli_primary_commands.txt (the freeze test only forbids
// removals, so a command added to the table and not to the baseline was
// invisible to it).
func TestREADME_CLICommandCountMatchesTheCommandTable(t *testing.T) {
	readme, err := os.ReadFile(baselinePath(t, "..", "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	claims := regexp.MustCompile(`\b(\d+) lifecycle commands\b`).FindAllStringSubmatch(string(readme), -1)
	if len(claims) == 0 {
		t.Fatal("README.md no longer says \"<n> lifecycle commands\" — update this test with the sentence that replaced it")
	}
	commands := cli.ContractPrimaryCommandNames()
	for _, m := range claims {
		n, _ := strconv.Atoi(m[1])
		if n != len(commands) {
			t.Errorf("README.md says %d lifecycle commands; the command table has %d (%s)", n, len(commands), strings.Join(commands, ", "))
		}
	}

	baseline := toSet(readBaselineLines(t, "baseline", "cli_primary_commands.txt"))
	unfrozen := make([]string, 0)
	for _, command := range commands {
		if _, ok := baseline[command]; !ok {
			unfrozen = append(unfrozen, command)
		}
	}
	sort.Strings(unfrozen)
	if len(unfrozen) > 0 {
		t.Errorf("primary command(s) in the command table and not in contracts/baseline/cli_primary_commands.txt: %s — add them to the baseline in the same change (the freeze forbids removing a command, not adding one)", strings.Join(unfrozen, ", "))
	}
}
