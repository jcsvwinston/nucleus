package auth

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
)

// TestErrUserNotFound_IsAliasAcrossTheBoundary pins the promise
// backend_registry.go makes in prose: "The names below are ALIASES, not
// copies … so errors.Is still matches across the boundary."
//
// It held for ErrInvalidCredentials and ErrBackendUnavailable, which are
// declared as aliases, and NOT for ErrUserNotFound, which was declared
// twice with errors.New and the same message text. A leaf backend
// returning backend.ErrUserNotFound was unrecognisable to code comparing
// against auth.ErrUserNotFound — and because both render as
// "auth: user not found", nothing in a log could show it.
func TestErrUserNotFound_IsAliasAcrossTheBoundary(t *testing.T) {
	cases := []struct {
		name string
		here error
		leaf error
	}{
		{"ErrUserNotFound", ErrUserNotFound, backend.ErrUserNotFound},
		{"ErrInvalidCredentials", ErrInvalidCredentials, backend.ErrInvalidCredentials},
		{"ErrBackendUnavailable", ErrBackendUnavailable, backend.ErrBackendUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.here != tc.leaf {
				t.Errorf("auth.%s and backend.%s are different values (%p vs %p); "+
					"a backend returning one is invisible to code comparing the other",
					tc.name, tc.name, tc.here, tc.leaf)
			}
			// The property that actually matters at a call site.
			if !errors.Is(fmt.Errorf("lookup %q: %w", "ana", tc.leaf), tc.here) {
				t.Errorf("errors.Is does not match auth.%s against a wrapped backend.%s", tc.name, tc.name)
			}
		})
	}
}
