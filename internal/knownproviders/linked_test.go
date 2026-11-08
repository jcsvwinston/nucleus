// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package knownproviders

import "testing"

// Only entries shipped as modules can be "not linked": a core entry
// registers through the framework module and never needs the import to be
// read, and an import outside the catalog is not a provider.
func TestLinkedByImportsIgnoresCoreEntriesAndStrangers(t *testing.T) {
	got := names(LinkedByImports(map[string]bool{
		RepoModule + "/pkg/auth/federated/oidc": true, // core: ships with the framework
		"github.com/someone/else/provider":      true,
		RepoModule + "/providers/auth-saml":     true,
	}))
	if len(got) != 1 || got[0] != "saml" {
		t.Fatalf("LinkedByImports = %v, want [saml]", got)
	}
}

func names(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}
