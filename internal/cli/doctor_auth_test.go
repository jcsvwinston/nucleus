// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func TestCheckAuth(t *testing.T) {
	for _, tc := range []struct {
		name       string
		backends   []string
		subtrees   map[string]map[string]any
		wantStatus doctorStatus
		wantIn     string
	}{
		{
			name:       "no chain is not a finding",
			wantStatus: doctorStatusPass,
			wantIn:     "no authentication chain is built",
		},
		{
			name:       "directory first, local second",
			backends:   []string{"ldap", "local"},
			subtrees:   map[string]map[string]any{"ldap": {"url": "ldaps://dc:636"}},
			wantStatus: doctorStatusPass,
			wantIn:     "ldap → local",
		},
		{
			name:       "a directory with no settings cannot reach one",
			backends:   []string{"ldap", "local"},
			wantStatus: doctorStatusError,
			wantIn:     "auth.ldap.* is empty",
		},
		{
			name:       "a chain of only remote backends has no way back in",
			backends:   []string{"ldap"},
			subtrees:   map[string]map[string]any{"ldap": {"url": "ldaps://dc:636"}},
			wantStatus: doctorStatusWarning,
			wantIn:     "nobody can log in",
		},
		{
			// The CLI binary does not import the application's providers,
			// so it must not judge registration: a name it has never
			// heard of is the application's own backend, not a mistake.
			name:       "an unknown name is not treated as a mistake",
			backends:   []string{"our-own-thing"},
			wantStatus: doctorStatusPass,
			wantIn:     "our-own-thing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := app.DefaultConfig()
			cfg.AuthBackends = tc.backends
			cfg.AuthBackendConfig = tc.subtrees

			got := checkAuth(&cfg, "")
			if got.status != tc.wantStatus {
				t.Fatalf("status = %s (%s), want %s", got.status, got.message, tc.wantStatus)
			}
			if !strings.Contains(got.message, tc.wantIn) {
				t.Errorf("message = %q, want it to contain %q", got.message, tc.wantIn)
			}
		})
	}
}
