// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package knownproviders

import "testing"

// The table is data, and data rots quietly: a module renamed or a name typed
// differently here turns the helpful error back into the useless one it was
// meant to replace — and nothing else in the build would notice, because a
// wrong entry still compiles and still looks like an answer.
func TestEveryEntryCarriesAWorkingRecipe(t *testing.T) {
	for _, tc := range []struct {
		kind    string
		lookup  func(string) (Provider, bool)
		names   []string
		wantMod map[string]string
	}{
		{
			kind:   "storage provider",
			lookup: StorageProvider,
			names:  StorageProviderNames(),
			wantMod: map[string]string{
				"s3":    "github.com/jcsvwinston/nucleus/providers/storage-s3",
				"gcs":   "github.com/jcsvwinston/nucleus/providers/storage-gcs",
				"azure": "github.com/jcsvwinston/nucleus/providers/storage-azure",
			},
		},
		{
			kind:   "authentication backend",
			lookup: AuthBackend,
			names:  AuthBackendNames(),
			wantMod: map[string]string{
				"ldap": "github.com/jcsvwinston/nucleus/providers/ldap",
			},
		},
	} {
		if len(tc.names) != len(tc.wantMod) {
			t.Errorf("%s: table lists %v, test expects %d entries — one of the two is stale",
				tc.kind, tc.names, len(tc.wantMod))
		}
		for name, wantModule := range tc.wantMod {
			p, ok := tc.lookup(name)
			if !ok {
				t.Errorf("%s %q is published as its own module but is not in the table: its error would say \"unknown\" instead of naming the import", tc.kind, name)
				continue
			}
			if p.Module != wantModule {
				t.Errorf("%s %q points at %q, want %q — the recipe has to be a module that exists", tc.kind, name, p.Module, wantModule)
			}
			if p.Kind != tc.kind {
				t.Errorf("%s %q is described as %q: the kind is what the error sentence calls it", tc.kind, name, p.Kind)
			}
			hint := p.InstallHint()
			for _, want := range []string{"go get " + wantModule, "import _ \"" + wantModule + "\""} {
				if !contains(hint, want) {
					t.Errorf("%s %q: install hint is missing %q:\n%s", tc.kind, name, want, hint)
				}
			}
		}
	}
}

// The secrets table is keyed by SCHEME, not by a bare name, and the scheme has
// to end in ":" or it can never prefix-match a reference.
func TestSecretsResolverIsKeyedByScheme(t *testing.T) {
	p, ok := SecretsResolver("aws-sm:")
	if !ok {
		t.Fatal("the aws-sm: scheme ships as its own module and must be in the table")
	}
	if p.Module != "github.com/jcsvwinston/nucleus/providers/secrets-aws" {
		t.Errorf("aws-sm: points at %q", p.Module)
	}
	if _, ok := SecretsResolver("aws-sm"); ok {
		t.Error("lookup without the colon must miss: the key IS the scheme, and a reference is matched by prefix")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
