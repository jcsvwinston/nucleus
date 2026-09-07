// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// completionOutput runs `nucleus completion <shell>` and returns stdout.
func completionOutput(t *testing.T, shell string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run([]string{"completion", shell}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("nucleus completion %s exited %d\nstderr: %s", shell, code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("nucleus completion %s wrote to stderr:\n%s", shell, errOut.String())
	}
	return out.String()
}

// TestCompletionGolden pins the generated script of each shell under
// testdata/completion. The scripts are derived from the command table,
// the aliases, each command's flags and its grammar, so a new command, a
// new flag or a new subcommand shows up here as a reviewed diff.
// Regenerate with `go test ./internal/cli -run TestCompletionGolden -update`.
func TestCompletionGolden(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		shell := shell
		t.Run(shell, func(t *testing.T) {
			got := completionOutput(t, shell)
			goldenPath := filepath.Join("testdata", "completion", shell+".golden")
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
				t.Fatalf("nucleus completion %s drifted from %s\n--- got ---\n%s\n--- want ---\n%s", shell, goldenPath, got, want)
			}
		})
	}
}

// TestCompletionCoversTheCommandTable pins what the scripts are made of:
// every primary command, every alias, help and version are offered as the
// first word in all three shells; migrate's actions (from its usageSpec)
// and doctor's checks (from its --check section) complete as well.
func TestCompletionCoversTheCommandTable(t *testing.T) {
	words := append(ContractPrimaryCommandNames(), ContractAliasCommandNames()...)
	words = append(words, "help", "version")
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script := completionOutput(t, shell)
		for _, w := range words {
			if !strings.Contains(script, w) {
				t.Errorf("%s completion does not mention the command %q", shell, w)
			}
		}
		for _, action := range commandUsages["migrate"].subcommandWords() {
			if !strings.Contains(script, action) {
				t.Errorf("%s completion does not offer the migrate action %q", shell, action)
			}
		}
		flagsWanted := []string{"--config", "--print-routes"}
		if shell == "fish" {
			flagsWanted = []string{"-l config", "-l print-routes"}
		}
		for _, want := range append(flagsWanted, "tasks outbox storage observability tenancy rbac security auth") {
			if !strings.Contains(script, want) {
				t.Errorf("%s completion lacks %q", shell, want)
			}
		}
	}
	bash := completionOutput(t, "bash")
	if !strings.Contains(bash, "complete -o default -F _nucleus_completion nucleus") {
		t.Errorf("bash completion must register its function:\n%s", bash)
	}
	zsh := completionOutput(t, "zsh")
	if !strings.HasPrefix(zsh, "#compdef nucleus\n") {
		t.Errorf("zsh completion must start with the compdef line")
	}
	fish := completionOutput(t, "fish")
	if !strings.Contains(fish, "complete -c nucleus -n '__fish_seen_subcommand_from migrate' -a up") {
		t.Errorf("fish completion must offer migrate's actions after the command word")
	}
	if !strings.Contains(fish, "complete -c nucleus -n '__fish_seen_subcommand_from completion' -a zsh") {
		t.Errorf("fish completion must offer the shells after `completion`")
	}
}

// TestCompletionFlagsComeFromTheHelp pins the flag source: a command's
// flags are what its own --help prints, value flags marked as such.
func TestCompletionFlagsComeFromTheHelp(t *testing.T) {
	flags := completionFlagsOf("routes")
	byName := map[string]completionFlag{}
	for _, f := range flags {
		byName[f.Name] = f
	}
	for name, takesValue := range map[string]bool{"config": true, "dir": true, "timeout": true, "json": false, "framework-only": false} {
		f, ok := byName[name]
		if !ok {
			t.Errorf("routes flag %q missing from the completion; help scrape broke", name)
			continue
		}
		if f.TakesValue != takesValue {
			t.Errorf("routes flag %q: TakesValue=%v, want %v", name, f.TakesValue, takesValue)
		}
		if f.Help == "" {
			t.Errorf("routes flag %q has no help text", name)
		}
	}
	model := buildCompletionModel()
	var doctor completionCommand
	for _, c := range model.Commands {
		if c.Name == "doctor" {
			doctor = c
		}
	}
	found := false
	for _, f := range doctor.Flags {
		if f.Name == "check" {
			found = true
			if len(f.Values) != len(commandUsages["doctor"].Sections[0].Rows) {
				t.Errorf("doctor --check values %v differ from the usage section", f.Values)
			}
		}
	}
	if !found {
		t.Errorf("doctor --check is missing from the completion model")
	}
}

// TestCompletionGlobalsMatchRootUsage keeps the hard-coded global options
// of the completion equal to what the root usage prints.
func TestCompletionGlobalsMatchRootUsage(t *testing.T) {
	var out bytes.Buffer
	printRootUsage(&out)
	for _, g := range globalCompletionFlags {
		if !strings.Contains(out.String(), "--"+g.Name) {
			t.Errorf("the root usage does not print --%s, which the completion offers", g.Name)
		}
		for _, v := range g.Values {
			if !strings.Contains(out.String(), v.Name) {
				t.Errorf("the root usage does not mention the --%s value %q", g.Name, v.Name)
			}
		}
	}
}

// TestCompletionRejectsUnknownShell pins the usage errors: no shell and an
// unknown shell are non-zero with a message naming the three shells.
func TestCompletionRejectsUnknownShell(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"completion"}, strings.NewReader(""), &out, &errOut); code == 0 {
		t.Fatalf("nucleus completion without a shell must fail")
	}
	if !strings.Contains(errOut.String(), "usage: nucleus completion <shell>") {
		t.Errorf("want the synopsis in the error, got: %s", errOut.String())
	}
	errOut.Reset()
	if code := Run([]string{"completion", "powershell"}, strings.NewReader(""), &out, &errOut); code == 0 {
		t.Fatalf("nucleus completion powershell must fail")
	}
	if !strings.Contains(errOut.String(), "bash, zsh or fish") {
		t.Errorf("want the shells named in the error, got: %s", errOut.String())
	}
	if out.Len() != 0 {
		t.Errorf("nothing on stdout when the shell is rejected, got:\n%s", out.String())
	}
}
