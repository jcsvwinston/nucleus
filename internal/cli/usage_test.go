// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var updateHelpGolden = flag.Bool("update", false, "rewrite the per-command help golden files under testdata/help")

// helpOutput runs `nucleus <cmd> --help` and returns stdout+stderr joined:
// the flag package prints usage on the flag set's output (stderr), and the
// tests care about what the terminal shows, not which stream carried it.
func helpOutput(t *testing.T, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(args, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("nucleus %s exited %d\nstderr: %s", strings.Join(args, " "), code, errOut.String())
	}
	return out.String() + errOut.String()
}

// TestCommandHelpGolden pins the rendered help of every command that
// carries a usageSpec, one golden file per command under testdata/help.
// A help screen is a contract a reader learns by heart; a change to it is
// reviewed as a diff, not discovered on a terminal. Regenerate with
// `go test ./internal/cli -run TestCommandHelpGolden -update`.
func TestCommandHelpGolden(t *testing.T) {
	for _, name := range usageSpecNames() {
		name := name
		t.Run(name, func(t *testing.T) {
			got := helpOutput(t, name, "--help")
			goldenPath := filepath.Join("testdata", "help", name+".golden")
			if *updateHelpGolden {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %s (run with -update to create it): %v", goldenPath, err)
			}
			if got != string(want) {
				t.Fatalf("nucleus %s --help drifted from %s\n--- got ---\n%s\n--- want ---\n%s", name, goldenPath, got, want)
			}
		})
	}
}

// TestCommandHelpListsGrammar pins the point of the usageSpec: a command's
// --help names its subcommands and positionals in front of the flags, which
// flag.PrintDefaults alone never did (`migrate --help` listed five flags and
// none of the eight actions).
func TestCommandHelpListsGrammar(t *testing.T) {
	migrate := helpOutput(t, "migrate", "--help")
	for _, action := range []string{"up", "down", "steps", "status", "drift", "reset", "refresh", "create"} {
		if !strings.Contains(migrate, "\n  "+action+" ") && !strings.Contains(migrate, "\n  "+action+"\n") {
			t.Errorf("nucleus migrate --help does not list the %q action:\n%s", action, migrate)
		}
	}
	if !strings.Contains(migrate, "Usage:\n  nucleus migrate ") {
		t.Errorf("nucleus migrate --help has no synopsis line:\n%s", migrate)
	}
	if strings.Contains(migrate, "Usage of migrate:") {
		t.Errorf("nucleus migrate --help still prints the bare flag package header:\n%s", migrate)
	}

	for cmd, positional := range map[string]string{
		"new":        "<project_name>",
		"startapp":   "<name>",
		"loaddata":   "<fixture.json>",
		"testserver": "<fixture.json>",
		"findstatic": "<asset>...",
	} {
		out := helpOutput(t, cmd, "--help")
		if !strings.Contains(out, "Usage:\n  nucleus "+cmd+" ") || !strings.Contains(out, positional) {
			t.Errorf("nucleus %s --help does not document its %s positional:\n%s", cmd, positional, out)
		}
	}

	doctor := helpOutput(t, "doctor", "--help")
	for _, check := range []string{"tasks", "outbox", "storage", "observability", "tenancy", "rbac", "security", "auth"} {
		if !strings.Contains(doctor, "\n  "+check+" ") {
			t.Errorf("nucleus doctor --help does not list the %q check:\n%s", check, doctor)
		}
	}

	// The flags still follow the grammar: nothing the old help said is lost.
	if !strings.Contains(migrate, "-migrations string") || !strings.Contains(doctor, "-check string") {
		t.Errorf("the flag defaults are missing from the rendered help")
	}
}

// TestHelpCommandRoutesToGrammar pins `nucleus help <cmd>`: the root
// dispatcher routes it to the same usageSpec as `<cmd> --help`.
func TestHelpCommandRoutesToGrammar(t *testing.T) {
	viaHelp := helpOutput(t, "help", "migrate")
	viaFlag := helpOutput(t, "migrate", "--help")
	if viaHelp != viaFlag {
		t.Fatalf("nucleus help migrate and nucleus migrate --help differ:\n--- help ---\n%s\n--- flag ---\n%s", viaHelp, viaFlag)
	}
	if !strings.Contains(viaHelp, "Actions:") || !strings.Contains(viaHelp, "create <name>") {
		t.Fatalf("nucleus help migrate does not print the action grammar:\n%s", viaHelp)
	}
}

// TestServeHelpRendersWithoutDefaultsFlagName pins the backtick trap: the
// flag package reads a backtick-quoted word in a usage string as the flag's
// value placeholder, so `serve --help` printed "-without-defaults go run ."
// as if the boolean flag took an argument.
func TestServeHelpRendersWithoutDefaultsFlagName(t *testing.T) {
	out := helpOutput(t, "serve", "--help")
	if !strings.Contains(out, "  -without-defaults\n") {
		t.Fatalf("serve --help does not render -without-defaults as a bare boolean flag:\n%s", out)
	}
	if strings.Contains(out, "-without-defaults go run .") {
		t.Fatalf("serve --help still renders the backtick placeholder:\n%s", out)
	}
}

// TestUsageErrorsMatchTheSynopsis pins that the error a command returns on
// wrong positionals is the first synopsis line of its usageSpec: the
// message and the help screen cannot disagree because they are one string.
func TestUsageErrorsMatchTheSynopsis(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"new"}, "usage: nucleus new <project_name> [flags]"},
		{[]string{"startapp"}, "usage: nucleus startapp <name> [flags]"},
		{[]string{"loaddata"}, "usage: nucleus loaddata [flags] <fixture.json>"},
		{[]string{"testserver"}, "usage: nucleus testserver [flags] <fixture.json>"},
		{[]string{"openapi", "extra"}, "usage: nucleus openapi [flags]"},
	}
	for _, tc := range cases {
		var out, errOut bytes.Buffer
		code := Run(tc.args, strings.NewReader(""), &out, &errOut)
		if code == 0 {
			t.Errorf("nucleus %s succeeded without its positional", strings.Join(tc.args, " "))
			continue
		}
		if !strings.Contains(errOut.String(), tc.want) {
			t.Errorf("nucleus %s: want %q in stderr, got:\n%s", strings.Join(tc.args, " "), tc.want, errOut.String())
		}
		spec := commandUsages[tc.args[0]]
		if got := strings.TrimPrefix(tc.want, "usage: "); spec.Synopsis[0] != got {
			t.Errorf("%s: the usage error %q is not the first synopsis line %q", tc.args[0], got, spec.Synopsis[0])
		}
	}
}

// TestCommandUsagesAreConsistent keeps the grammar table honest as data —
// the property a completion generator will rely on: every key is a real
// primary command, every example invokes that command, and the migrate
// action list is exactly what the dispatcher accepts.
func TestCommandUsagesAreConsistent(t *testing.T) {
	for _, name := range usageSpecNames() {
		if _, ok := commandByName[name]; !ok {
			t.Errorf("commandUsages[%q] is not a primary command", name)
		}
		spec := commandUsages[name]
		if len(spec.Synopsis) == 0 {
			t.Errorf("commandUsages[%q] has no synopsis", name)
		}
		for _, line := range spec.Synopsis {
			if !strings.HasPrefix(line, "nucleus "+name) {
				t.Errorf("commandUsages[%q] synopsis %q does not start with the command", name, line)
			}
		}
		for _, ex := range spec.Examples {
			if !strings.HasPrefix(ex, "nucleus "+name+" ") {
				t.Errorf("commandUsages[%q] example %q does not invoke the command", name, ex)
			}
		}
		for _, row := range append(append([]usageRow{}, spec.Positionals...), spec.Subcommands...) {
			if strings.TrimSpace(row.Name) == "" || strings.TrimSpace(row.Help) == "" {
				t.Errorf("commandUsages[%q] has a row without name or help: %+v", name, row)
			}
		}
	}

	// Every command the audit named as positional-or-subcommand-bearing
	// carries a grammar; a future command with positionals should join.
	for _, name := range []string{"migrate", "routes", "serve", "new", "startapp", "shell", "seed", "loaddata", "testserver", "findstatic", "collectstatic", "doctor", "openapi"} {
		if _, ok := commandUsages[name]; !ok {
			t.Errorf("commandUsages lacks %q", name)
		}
	}

	got := commandUsages["migrate"].subcommandWords()
	sort.Strings(got)
	want := []string{"create"}
	for _, action := range []string{"up", "down", "steps", "status", "drift", "reset", "refresh"} {
		if !isMigrateActionSupported(action) {
			t.Fatalf("isMigrateActionSupported(%q) is false; the dispatcher and this test disagree", action)
		}
		want = append(want, action)
	}
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("migrate usage actions %v differ from the dispatcher's %v", got, want)
	}
	for _, word := range got {
		if word != "create" && !isMigrateActionSupported(word) {
			t.Errorf("migrate usage lists %q, which the dispatcher rejects", word)
		}
	}
}
