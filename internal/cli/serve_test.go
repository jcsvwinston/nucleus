package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestRunServe_RejectsPositionalArgs covers the argument-validation guard for
// both the default and --without-defaults branches (R3 / ADR-013): the new bool
// flag parses in either position, and serve still rejects stray positional
// arguments before it ever attempts to construct or run the server. The
// happy-path branches both end in a blocking a.Run, so the behavioural
// difference of WithoutDefaults() is covered at the pkg/app and pkg/nucleus
// layers rather than here.
func TestRunServe_RejectsPositionalArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"default", []string{"stray"}},
		{"without-defaults", []string{"--without-defaults", "stray"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			err := runServe(tc.args, strings.NewReader(""), &out, &errOut)
			if err == nil {
				t.Fatal("expected an error for positional arguments")
			}
			if !strings.Contains(err.Error(), "positional") {
				t.Fatalf("expected a positional-arg error, got %v", err)
			}
		})
	}
}

// TestRunServe_RejectsUnknownFlag confirms flag parsing is strict. Paired with
// the without-defaults case above, it pins that --without-defaults is a *known*
// flag: an unknown flag errors, the known one parses and only trips the
// positional-arg guard.
func TestRunServe_RejectsUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	err := runServe([]string{"--definitely-not-a-flag"}, strings.NewReader(""), &out, &errOut)
	if err == nil {
		t.Fatal("expected a parse error for an unknown flag")
	}
}
