// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import "testing"

// A4/S6, NU-50: the framework's `db:` grammar and Quark's read a tag of the
// same name differently, and neither used to say so. Quark refuses a tag it
// cannot read as a column name; this side cannot refuse — an unrecognized
// token has always been a warning, and making it fatal would break apps
// whose stray tokens were ignored for versions — so it names the other
// grammar instead of leaving the reader to guess.

func TestLooksLikeQuarkTagGrammar(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tokens []string
		want   bool
	}{
		{"quark sizing option", []string{"size=255"}, true},
		{"quark precision", []string{"precision=18"}, true},
		{"quark scale", []string{"scale=4"}, true},
		{"quark comma-separated tag", []string{"email,size=255"}, true},
		// A bare identifier is ambiguous: it is equally a Quark column name
		// and a typo in the framework's grammar, so it does not trigger the
		// hint on its own.
		{"bare token", []string{"emial"}, false},
		{"nothing", nil, false},
		{"framework grammar typo", []string{"notnull"}, false},
	} {
		if got := looksLikeQuarkTagGrammar(tc.tokens); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
